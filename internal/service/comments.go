package service

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/search"
	"github.com/edward-murrell/kowhai/internal/store"
)

func (s *Service) ListComments(ctx context.Context, cardID string, cursor *string, limit *int) (CommentPage, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return CommentPage{}, err
	}
	if _, _, err := s.loadCard(ctx, a, cardID, ActBoardView); err != nil {
		return CommentPage{}, err
	}
	l := clampLimit(limit, 50, 200)
	rows, err := s.DB.R.ListCardComments(ctx, store.ListCardCommentsParams{CardID: cardID, Before: deref(cursor, ""), Limit: int64(l + 1)})
	if err != nil {
		return CommentPage{}, err
	}
	page := CommentPage{Items: []Comment{}}
	if len(rows) > l {
		rows = rows[:l]
		page.NextCursor = ptr(rows[l-1].ID)
	}
	var ids []string
	for _, c := range rows {
		if !slices.Contains(ids, c.AuthorID) {
			ids = append(ids, c.AuthorID)
		}
	}
	users, err := s.usersByID(ctx, ids)
	if err != nil {
		return page, err
	}
	for _, c := range rows {
		page.Items = append(page.Items, commentOut(c, users[c.AuthorID]))
	}
	return page, nil
}

func (s *Service) CreateComment(ctx context.Context, cardID, body string) (Comment, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Comment{}, err
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return Comment{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, badRequest("body is required")
	}
	var out Comment
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		c, err := tx.GetCard(ctx, cardID)
		if err != nil {
			return notFound(err)
		}
		now := s.now()
		cm, err := tx.CreateComment(ctx, store.CreateCommentParams{
			ID: s.NewID(), CardID: cardID, AuthorID: a.User.ID, TokenID: a.tokenID(), ActorLabel: a.Label(),
			Body: body, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := search.IndexComment(ctx, tx.Conn, cm, b.ID); err != nil {
			return err
		}
		out = commentOut(cm, a.User)
		mentioned, err := s.mentionedUsers(ctx, b, body)
		if err != nil {
			return err
		}
		audience, err := cardAudience(ctx, tx.Queries, cardID)
		if err != nil {
			return err
		}
		co, err := cardOut(ctx, tx.Queries, c)
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{
			WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cardID, Type: TCommentCreated,
			Payload: map[string]any{"card_title": c.Title, "comment_id": cm.ID, "excerpt": excerpt(body, 200), "entity": out, "card": co},
			// Mentions come first so a mentioned watcher gets the mention.
			Notify: []notifyGroup{{Pref: PrefMention, Users: mentioned}, {Pref: PrefComment, Users: audience}},
		})
		return err
	})
	return out, err
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// UpdateComment edits a comment. Only its author can edit it.
func (s *Service) UpdateComment(ctx context.Context, id, ifMatch, body string) (Comment, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Comment{}, err
	}
	cm, err := s.DB.R.GetComment(ctx, id)
	if err != nil || cm.DeletedAt.Valid {
		return Comment{}, ErrNotFound
	}
	_, b, err := s.loadCard(ctx, a, cm.CardID, ActBoardEdit)
	if err != nil {
		return Comment{}, err
	}
	if cm.AuthorID != a.User.ID {
		return Comment{}, forbidden("you can only edit your own comments")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, badRequest("body is required")
	}
	var out Comment
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		cm, err := tx.GetComment(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, cm.Version); err != nil {
			return err
		}
		ncm, err := tx.UpdateComment(ctx, store.UpdateCommentParams{ID: id, Body: body, EditedAt: nt(s.now()), UpdatedAt: s.now()})
		if err != nil {
			return err
		}
		if err := search.IndexComment(ctx, tx.Conn, ncm, b.ID); err != nil {
			return err
		}
		out = commentOut(ncm, a.User)
		before, err := s.mentionedUsers(ctx, b, cm.Body)
		if err != nil {
			return err
		}
		after, err := s.mentionedUsers(ctx, b, body)
		if err != nil {
			return err
		}
		c, _ := tx.GetCard(ctx, cm.CardID)
		_, err = s.record(ctx, tx, a, activityInput{
			WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cm.CardID, Type: TCommentUpdated,
			Payload: map[string]any{"card_title": c.Title, "comment_id": id, "entity": out},
			Notify:  []notifyGroup{{Pref: PrefMention, Users: slices.DeleteFunc(after, func(u string) bool { return slices.Contains(before, u) })}},
		})
		return err
	})
	return out, err
}

// DeleteComment soft-deletes a comment. The author or a board admin can.
func (s *Service) DeleteComment(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	cm, err := s.DB.R.GetComment(ctx, id)
	if err != nil || cm.DeletedAt.Valid {
		return ErrNotFound
	}
	action := ActBoardAdmin
	if cm.AuthorID == a.User.ID {
		action = ActBoardEdit
	}
	_, b, err := s.loadCard(ctx, a, cm.CardID, action)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if _, err := tx.UpdateComment(ctx, store.UpdateCommentParams{ID: id, Body: cm.Body, EditedAt: cm.EditedAt, DeletedAt: sql.NullTime{Time: s.now(), Valid: true}, UpdatedAt: s.now()}); err != nil {
			return err
		}
		if err := search.Remove(ctx, tx.Conn, search.TypeComment, id); err != nil {
			return err
		}
		c, err := tx.GetCard(ctx, cm.CardID)
		if err != nil {
			return err
		}
		co, err := cardOut(ctx, tx.Queries, c)
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{
			WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cm.CardID, Type: TCommentDeleted,
			Payload: map[string]any{"card_title": c.Title, "comment_id": id, "card": co},
		})
		return err
	})
}
