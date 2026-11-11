package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kowhai/internal/events"
	"kowhai/internal/service"
)

// Streams serves the endpoints that the generated code does not: SSE,
// uploads and downloads.
type Streams struct {
	S   *service.Service
	Hub *events.Hub
}

const heartbeat = 25 * time.Second

func sseStart(w http.ResponseWriter) (http.Flusher, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	// SSE connections outlive the server write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 3000\n\n")
	f.Flush()
	return f, true
}

func writeEvent(w io.Writer, id, name string, data []byte) error {
	var b strings.Builder
	if id != "" {
		fmt.Fprintf(&b, "id: %s\n", id)
	}
	if name != "" {
		fmt.Fprintf(&b, "event: %s\n", name)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fmt.Fprintf(&b, "data: %s\n", line)
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// BoardEvents streams a board's activity. A reconnect with Last-Event-ID
// (or ?last_event_id=) replays what was missed.
func (x *Streams) BoardEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, err := x.S.GetBoard(ctx, id); err != nil {
		WriteError(w, r, err)
		return
	}
	// Subscribe before replaying so nothing falls in between.
	sub := x.Hub.Subscribe(events.BoardTopic(id))
	defer x.Hub.Unsubscribe(sub)
	f, ok := sseStart(w)
	if !ok {
		WriteProblem(w, 500, "Streaming not supported", "")
		return
	}
	last := r.Header.Get("Last-Event-ID")
	if last == "" {
		last = r.URL.Query().Get("last_event_id")
	}
	if last != "" {
		acts, ok, err := x.S.ReplayBoard(ctx, id, last)
		if err != nil || !ok {
			_ = writeEvent(w, "", "resync", []byte("{}"))
		} else {
			for _, a := range acts {
				b, _ := json.Marshal(a)
				if err := writeEvent(w, a.Id, "activity", b); err != nil {
					return
				}
				last = a.Id
			}
		}
		f.Flush()
	}
	x.pump(w, r, f, sub, last)
}

// UserEvents streams the current user's notifications.
func (x *Streams) UserEvents(w http.ResponseWriter, r *http.Request) {
	me, err := x.S.Me(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	sub := x.Hub.Subscribe(events.UserTopic(me.User.Id))
	defer x.Hub.Unsubscribe(sub)
	f, ok := sseStart(w)
	if !ok {
		return
	}
	b, _ := json.Marshal(map[string]int{"unread": me.UnreadNotifications})
	_ = writeEvent(w, "", "unread", b)
	f.Flush()
	x.pump(w, r, f, sub, "")
}

func (x *Streams) pump(w http.ResponseWriter, r *http.Request, f http.Flusher, sub *events.Sub, last string) {
	t := time.NewTicker(heartbeat)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			f.Flush()
		case ev, ok := <-sub.C:
			if !ok {
				// Dropped for being slow. The client reconnects and replays.
				return
			}
			// Skip anything the replay already sent.
			if ev.ID != "" && last != "" && ev.ID <= last && ev.Name == "activity" {
				continue
			}
			if err := writeEvent(w, ev.ID, ev.Name, ev.Data); err != nil {
				return
			}
			f.Flush()
		}
	}
}

// Upload handles a multipart upload with one "file" part. The part is
// streamed to the service, which enforces the size limit.
func (x *Streams) Upload(w http.ResponseWriter, r *http.Request) {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "multipart/form-data" {
		WriteProblem(w, 400, "Bad request", "send multipart/form-data with a file part")
		return
	}
	limit := x.S.Cfg.MaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		WriteProblem(w, 400, "Bad request", err.Error())
		return
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			WriteProblem(w, 400, "Bad request", "no file part")
			return
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				WriteProblem(w, 413, "Too large", "the file is over the upload limit")
				return
			}
			WriteProblem(w, 400, "Bad request", err.Error())
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		att, err := x.S.UploadAttachment(r.Context(), r.PathValue("id"), part.FileName(), part)
		part.Close()
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				WriteProblem(w, 413, "Too large", "the file is over the upload limit")
				return
			}
			WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(att)
		return
	}
}

// inlineTypes are shown in the browser. Everything else downloads. SVG is
// left out because it can carry script.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true,
}

// Download streams an attachment after a permission check.
func (x *Streams) Download(w http.ResponseWriter, r *http.Request) {
	at, rc, err := x.S.OpenAttachment(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	defer rc.Close()
	h := w.Header()
	h.Set("Content-Type", at.ContentType)
	h.Set("Content-Length", strconv.FormatInt(at.SizeBytes, 10))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("ETag", `"`+at.Sha256+`"`)
	disp := "attachment"
	if inlineTypes[at.ContentType] && r.URL.Query().Get("download") == "" {
		disp = "inline"
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": at.Filename}))
	_, _ = io.Copy(w, rc)
}
