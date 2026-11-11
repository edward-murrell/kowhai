package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/events"
	"github.com/edward-murrell/kowhai/internal/store"
)

// StaleAfter is how long a working or waiting session can go quiet before
// the worker marks it stale.
const StaleAfter = 30 * time.Minute

var statusRank = map[string]int{"waiting": 4, "working": 3, "idle": 2, "stale": 1, "ended": 0, "": 0}

// AggregateStatus picks the most urgent status of a card's sessions:
// waiting, then working, then idle, then stale.
func AggregateStatus(links []store.CardLink) string {
	best := ""
	for _, l := range links {
		if l.Kind == "claude_code" && statusRank[l.Status] > statusRank[best] {
			best = l.Status
		}
	}
	return best
}

func validStatus(s string) bool {
	_, ok := statusRank[s]
	return ok
}

func (s *Service) ListCardLinks(ctx context.Context, cardID string) ([]CardLink, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.loadCard(ctx, a, cardID, ActBoardView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListCardLinks(ctx, cardID)
	if err != nil {
		return nil, err
	}
	out := []CardLink{}
	for _, l := range rows {
		out = append(out, linkOut(l))
	}
	return out, nil
}

func (s *Service) CreateCardLink(ctx context.Context, cardID string, in CreateCardLink) (CardLink, error) {
	return s.PutCardLink(ctx, cardID, string(in.Kind), in.ExternalKey, PutCardLink{Title: in.Title}, true)
}

// PutCardLink creates or updates a link by kind and external key. The
// hook calls this on every Claude Code event. A session that was linked
// to another card moves to this one. Status changes write an activity
// row; other updates only refresh last_seen_at and are pushed to open
// boards without one.
func (s *Service) PutCardLink(ctx context.Context, cardID, kind, key string, in PutCardLink, createOnly bool) (CardLink, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return CardLink{}, err
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return CardLink{}, err
	}
	if kind != "claude_code" && kind != "url" {
		return CardLink{}, badRequest("kind must be claude_code or url")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return CardLink{}, badRequest("external key is required")
	}
	if in.Status != nil && !validStatus(string(*in.Status)) {
		return CardLink{}, badRequest("unknown status")
	}
	var out CardLink
	var quiet bool
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		existing, err := tx.GetCardLinkByKey(ctx, store.GetCardLinkByKeyParams{Kind: kind, ExternalKey: key})
		now := s.now()
		if errors.Is(err, sql.ErrNoRows) {
			status := ""
			if in.Status != nil {
				status = string(*in.Status)
			}
			meta := "{}"
			if in.Meta != nil {
				meta = mustJSON(in.Meta)
			}
			var seen sql.NullTime
			if kind == "claude_code" {
				seen = nt(now)
			}
			l, err := tx.CreateCardLink(ctx, store.CreateCardLinkParams{
				ID: s.NewID(), CardID: cardID, BoardID: b.ID, Kind: kind, ExternalKey: key, Title: deref(in.Title, ""),
				Repo: deref(in.Repo, ""), Branch: deref(in.Branch, ""), Host: deref(in.Host, ""), Status: status,
				StatusDetail: deref(in.StatusDetail, ""), LastSeenAt: seen, Meta: meta, CreatedBy: a.User.ID,
				CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return err
			}
			out = linkOut(l)
			c, _ := tx.GetCard(ctx, cardID)
			if _, err := s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cardID, Type: TLinkCreated,
				Payload: map[string]any{"card_title": c.Title, "kind": kind, "status": status, "repo": l.Repo, "branch": l.Branch, "host": l.Host, "entity": out}}); err != nil {
				return err
			}
			return s.afterStatusChange(ctx, tx, a, b, cardID, "", l)
		}
		if err != nil {
			return err
		}
		if createOnly {
			return conflict("a link with that key already exists")
		}
		if existing.CardID != cardID {
			// The link must be editable on its old board too.
			if _, err := s.loadBoard(ctx, a, existing.BoardID, ActBoardEdit); err != nil {
				return err
			}
		}
		return s.updateLinkTx(ctx, tx, a, b, existing, cardID, in, &out, &quiet)
	})
	if err == nil && quiet {
		s.publishLink(out)
	}
	return out, err
}

func (s *Service) updateLinkTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, l store.CardLink, cardID string, in PutCardLink, out *CardLink, quiet *bool) error {
	now := s.now()
	p := store.UpdateCardLinkParams{
		ID: l.ID, CardID: cardID, BoardID: b.ID, Title: deref(in.Title, l.Title), Repo: deref(in.Repo, l.Repo),
		Branch: deref(in.Branch, l.Branch), Host: deref(in.Host, l.Host), Status: l.Status,
		StatusDetail: deref(in.StatusDetail, l.StatusDetail), LastSeenAt: l.LastSeenAt, Meta: l.Meta, UpdatedAt: now,
	}
	if in.Status != nil {
		p.Status = string(*in.Status)
	}
	if in.Meta != nil {
		p.Meta = mustJSON(in.Meta)
	}
	if l.Kind == "claude_code" {
		p.LastSeenAt = nt(now)
	}
	nl, err := tx.UpdateCardLink(ctx, p)
	if err != nil {
		return err
	}
	*out = linkOut(nl)
	moved := l.CardID != cardID
	if p.Status == l.Status && !moved && p.Branch == l.Branch && p.StatusDetail == l.StatusDetail {
		*quiet = true
		return nil
	}
	c, _ := tx.GetCard(ctx, cardID)
	payload := map[string]any{
		"card_title": c.Title, "status": p.Status, "previous_status": l.Status, "detail": excerpt(p.StatusDetail, 300),
		"repo": nl.Repo, "branch": nl.Branch, "host": nl.Host, "session": shortKey(nl.ExternalKey), "entity": *out,
	}
	if moved {
		payload["from_card_id"] = l.CardID
	}
	var notify []notifyGroup
	if p.Status == "waiting" && l.Status != "waiting" {
		members, err := tx.ListCardMembers(ctx, cardID)
		if err != nil {
			return err
		}
		notify = append(notify, notifyGroup{Pref: PrefSessionWaiting, Users: members, IncludeActor: true})
	}
	if _, err := s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cardID, Type: TLinkStatus, Payload: payload, Notify: notify}); err != nil {
		return err
	}
	if moved {
		old, err := tx.GetBoard(ctx, l.BoardID)
		if err == nil && old.ID != b.ID {
			tx.AfterCommit(func() { s.PublishResync(old.ID) })
		}
	}
	return s.afterStatusChange(ctx, tx, a, b, cardID, l.Status, nl)
}

func shortKey(k string) string {
	if len(k) > 8 {
		return k[:8]
	}
	return k
}

// afterStatusChange moves the card when the board maps the card's new
// overall status to a list.
func (s *Service) afterStatusChange(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, cardID, _ string, _ store.CardLink) error {
	st := parseSettings(b.Settings)
	if st.StatusListsEnabled == nil || !*st.StatusListsEnabled || st.StatusLists == nil {
		return nil
	}
	links, err := tx.ListCardLinks(ctx, cardID)
	if err != nil {
		return err
	}
	status := AggregateStatus(links)
	listID := (*st.StatusLists)[status]
	if listID == "" {
		return nil
	}
	c, err := tx.GetCard(ctx, cardID)
	if err != nil {
		return err
	}
	if c.ListID == listID || c.ArchivedAt.Valid {
		return nil
	}
	if l, err := tx.GetList(ctx, listID); err != nil || l.BoardID != b.ID || l.ArchivedAt.Valid {
		return nil
	}
	_, err = s.moveCardTx(ctx, tx, a, b, cardID, listID, Placement{}, map[string]any{"reason": "status", "status": status})
	return err
}

func (s *Service) publishLink(l CardLink) {
	if s.Hub == nil {
		return
	}
	b, _ := json.Marshal(l)
	s.Hub.Publish(events.BoardTopic(l.BoardId), events.Event{Name: "link", Data: b})
}

func (s *Service) loadLink(ctx context.Context, a *Actor, id string) (store.CardLink, store.Board, error) {
	l, err := s.DB.R.GetCardLink(ctx, id)
	if err != nil {
		return l, store.Board{}, notFound(err)
	}
	b, err := s.loadBoard(ctx, a, l.BoardID, ActBoardEdit)
	return l, b, err
}

func (s *Service) UpdateCardLink(ctx context.Context, id, ifMatch string, in PutCardLink) (CardLink, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return CardLink{}, err
	}
	_, b, err := s.loadLink(ctx, a, id)
	if err != nil {
		return CardLink{}, err
	}
	if in.Status != nil && !validStatus(string(*in.Status)) {
		return CardLink{}, badRequest("unknown status")
	}
	var out CardLink
	var quiet bool
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		l, err := tx.GetCardLink(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, l.Version); err != nil {
			return err
		}
		return s.updateLinkTx(ctx, tx, a, b, l, l.CardID, in, &out, &quiet)
	})
	if err == nil && quiet {
		s.publishLink(out)
	}
	return out, err
}

func (s *Service) DeleteCardLink(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	l, b, err := s.loadLink(ctx, a, id)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if err := tx.DeleteCardLink(ctx, id); err != nil {
			return err
		}
		c, _ := tx.GetCard(ctx, l.CardID)
		_, err := s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: l.CardID, Type: TLinkDeleted,
			Payload: map[string]any{"card_title": c.Title, "link_id": id, "kind": l.Kind}})
		return err
	})
}

// MarkStaleLinks marks sessions stale when they have said working or
// waiting with no update for StaleAfter. It catches crashed sessions.
func (s *Service) MarkStaleLinks(ctx context.Context) (int, error) {
	rows, err := s.DB.R.ListStaleLinks(ctx, nt(s.now().Add(-StaleAfter)))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range rows {
		err := s.DB.WriteTx(ctx, func(tx *store.Tx) error {
			l, err := tx.GetCardLink(ctx, l.ID)
			if err != nil || (l.Status != "working" && l.Status != "waiting") || l.LastSeenAt.Time.After(s.now().Add(-StaleAfter)) {
				return nil
			}
			b, err := tx.GetBoard(ctx, l.BoardID)
			if err != nil {
				return err
			}
			nl, err := tx.UpdateCardLink(ctx, store.UpdateCardLinkParams{
				ID: l.ID, CardID: l.CardID, BoardID: l.BoardID, Title: l.Title, Repo: l.Repo, Branch: l.Branch, Host: l.Host,
				Status: "stale", StatusDetail: l.StatusDetail, LastSeenAt: l.LastSeenAt, Meta: l.Meta, UpdatedAt: s.now(),
			})
			if err != nil {
				return err
			}
			c, _ := tx.GetCard(ctx, l.CardID)
			if _, err := s.record(ctx, tx, nil, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: l.CardID, Type: TLinkStatus,
				Payload: map[string]any{"card_title": c.Title, "status": "stale", "previous_status": l.Status, "session": shortKey(l.ExternalKey),
					"repo": l.Repo, "branch": l.Branch, "host": l.Host, "entity": linkOut(nl)}}); err != nil {
				return err
			}
			n++
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
