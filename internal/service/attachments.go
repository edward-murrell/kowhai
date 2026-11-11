package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	. "kowhai/internal/apitypes"
	"kowhai/internal/store"
)

// ErrNoBlobStore is returned when attachments are not configured.
var ErrNoBlobStore = &Error{Status: 503, Title: "Attachments are not configured"}

func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == "" {
		name = "file"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name
}

// UploadAttachment stores a file on a card. The body is spooled to a
// temporary file first so the size limit, checksum and content type are
// known before anything reaches S3. The content type comes from the
// bytes, not the client.
func (s *Service) UploadAttachment(ctx context.Context, cardID, filename string, body io.Reader) (Attachment, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Attachment{}, err
	}
	if s.Blob == nil {
		return Attachment{}, ErrNoBlobStore
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return Attachment{}, err
	}
	tmp, err := os.CreateTemp("", "kowhai-upload-*")
	if err != nil {
		return Attachment{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	limit := s.Cfg.MaxUploadBytes
	if limit <= 0 {
		limit = 25 << 20
	}
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, limit+1))
	if err != nil {
		return Attachment{}, err
	}
	if n > limit {
		return Attachment{}, &Error{Status: 413, Title: "Too large", Detail: "the file is over the upload limit"}
	}
	head := make([]byte, 512)
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Attachment{}, err
	}
	hn, _ := io.ReadFull(tmp, head)
	ctype := http.DetectContentType(head[:hn])
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Attachment{}, err
	}

	id := s.NewID()
	key := "attachments/" + b.ID + "/" + id
	if err := s.Blob.Put(ctx, key, tmp, n, ctype); err != nil {
		s.Log.Error("upload to object store", "err", err)
		return Attachment{}, &Error{Status: 502, Title: "Upload failed", Detail: "the object store did not accept the file"}
	}
	var out Attachment
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		at, err := tx.CreateAttachment(ctx, store.CreateAttachmentParams{
			ID: id, CardID: cardID, BoardID: b.ID, UploaderID: a.User.ID, ObjectKey: key, Filename: cleanFilename(filename),
			ContentType: ctype, SizeBytes: n, Sha256: hex.EncodeToString(h.Sum(nil)), CreatedAt: s.now(),
		})
		if err != nil {
			return err
		}
		out = attachmentOut(at)
		c, err := tx.GetCard(ctx, cardID)
		if err != nil {
			return err
		}
		co, err := cardOut(ctx, tx.Queries, c)
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cardID, Type: TAttachmentAdded,
			Payload: map[string]any{"card_title": c.Title, "filename": at.Filename, "entity": out, "card": co}})
		return err
	})
	if err != nil {
		// Do not leave an orphan object behind.
		_ = store.New(s.DB.Write).QueueBlobDeletion(context.WithoutCancel(ctx), store.QueueBlobDeletionParams{ObjectKey: key, CreatedAt: s.now()})
		return Attachment{}, err
	}
	return out, nil
}

// OpenAttachment checks access and opens the object for streaming.
func (s *Service) OpenAttachment(ctx context.Context, id string) (store.Attachment, io.ReadCloser, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return store.Attachment{}, nil, err
	}
	if s.Blob == nil {
		return store.Attachment{}, nil, ErrNoBlobStore
	}
	at, err := s.DB.R.GetAttachment(ctx, id)
	if err != nil {
		return at, nil, notFound(err)
	}
	if _, err := s.loadBoard(ctx, a, at.BoardID, ActBoardView); err != nil {
		return at, nil, err
	}
	rc, err := s.Blob.Get(ctx, at.ObjectKey)
	if err != nil {
		s.Log.Error("read from object store", "key", at.ObjectKey, "err", err)
		return at, nil, &Error{Status: 502, Title: "Download failed"}
	}
	return at, rc, nil
}

func (s *Service) DeleteAttachment(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	at, err := s.DB.R.GetAttachment(ctx, id)
	if err != nil {
		return notFound(err)
	}
	b, err := s.loadBoard(ctx, a, at.BoardID, ActBoardEdit)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if err := tx.QueueBlobDeletion(ctx, store.QueueBlobDeletionParams{ObjectKey: at.ObjectKey, CreatedAt: s.now()}); err != nil {
			return err
		}
		if err := tx.ClearCardCover(ctx, store.ClearCardCoverParams{CoverAttachmentID: ns(id), UpdatedAt: s.now()}); err != nil {
			return err
		}
		if err := tx.DeleteAttachment(ctx, id); err != nil {
			return err
		}
		c, err := tx.GetCard(ctx, at.CardID)
		if err != nil {
			return err
		}
		co, err := cardOut(ctx, tx.Queries, c)
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: at.CardID, Type: TAttachmentDeleted,
			Payload: map[string]any{"card_title": c.Title, "filename": at.Filename, "attachment_id": id, "card": co}})
		return err
	})
}
