package service

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/search"
	"github.com/edward-murrell/kowhai/internal/store"
)

func cardSiblings(ctx context.Context, q *store.Queries, listID string) ([]sibling, error) {
	rows, err := q.ListAllListCards(ctx, listID)
	if err != nil {
		return nil, err
	}
	out := make([]sibling, len(rows))
	for i, r := range rows {
		out[i] = sibling{r.ID, r.Position}
	}
	return out, nil
}

func (s *Service) placeCard(ctx context.Context, tx *store.Tx, boardID, listID, self string, p Placement) (string, error) {
	sibs, err := cardSiblings(ctx, tx.Queries, listID)
	if err != nil {
		return "", err
	}
	key, respaced, err := placeAmong(sibs, self, deref(p.AfterId, ""), deref(p.BeforeId, ""))
	if err != nil {
		return "", err
	}
	for _, r := range respaced {
		if err := tx.SetCardPosition(ctx, store.SetCardPositionParams{ID: r.ID, Position: r.Pos}); err != nil {
			return "", err
		}
	}
	if len(respaced) > 0 {
		tx.AfterCommit(func() { s.PublishResync(boardID) })
	}
	return key, nil
}

// cardEvent records a card activity with the card's current state as the
// entity, so clients can patch their copy.
func (s *Service) cardEvent(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, c store.Card, typ string, payload map[string]any, notify ...notifyGroup) (Card, error) {
	out, err := cardOut(ctx, tx.Queries, c)
	if err != nil {
		return out, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["card_title"] = c.Title
	payload["entity"] = out
	_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: c.ID, Type: typ, Payload: payload, Notify: notify})
	return out, err
}

// cardAudience returns the card's members and watchers.
func cardAudience(ctx context.Context, q *store.Queries, cardID string) ([]string, error) {
	m, err := q.ListCardMembers(ctx, cardID)
	if err != nil {
		return nil, err
	}
	w, err := q.ListCardWatchers(ctx, cardID)
	if err != nil {
		return nil, err
	}
	for _, id := range w {
		if !slices.Contains(m, id) {
			m = append(m, id)
		}
	}
	return m, nil
}

type newCard struct {
	Title, Description string
	DueAt, StartAt     *time.Time
	IsTemplate         bool
	RecurrenceID       string
	LabelIDs           []string
	MemberIDs          []string
	Placement          Placement
}

// createCardTx inserts a card with its labels and members, indexes it and
// records the activity.
func (s *Service) createCardTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, l store.List, in newCard, extra map[string]any) (Card, store.Card, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Card{}, store.Card{}, badRequest("title is required")
	}
	pos, err := s.placeCard(ctx, tx, b.ID, l.ID, "", in.Placement)
	if err != nil {
		return Card{}, store.Card{}, err
	}
	now := s.now()
	createdBy := a.User.ID
	c, err := tx.CreateCard(ctx, store.CreateCardParams{
		ID: s.NewID(), ListID: l.ID, BoardID: b.ID, Title: title, Description: in.Description, Position: pos,
		DueAt: ntp(in.DueAt), StartAt: ntp(in.StartAt), IsTemplate: in.IsTemplate, RecurrenceID: ns(in.RecurrenceID),
		CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Card{}, c, err
	}
	for _, id := range in.LabelIDs {
		lbl, err := tx.GetLabel(ctx, id)
		if err != nil || lbl.BoardID != b.ID {
			return Card{}, c, badRequest("label %s is not on this board", id)
		}
		if _, err := tx.AddCardLabel(ctx, store.AddCardLabelParams{CardID: c.ID, LabelID: id}); err != nil {
			return Card{}, c, err
		}
	}
	var added []string
	for _, id := range in.MemberIDs {
		if !s.canSeeBoard(ctx, id, b) {
			return Card{}, c, badRequest("user %s cannot see this board", id)
		}
		if _, err := tx.AddCardMember(ctx, store.AddCardMemberParams{CardID: c.ID, UserID: id, CreatedAt: now}); err != nil {
			return Card{}, c, err
		}
		added = append(added, id)
	}
	if err := search.IndexCard(ctx, tx.Conn, c); err != nil {
		return Card{}, c, err
	}
	mentioned, err := s.mentionedUsers(ctx, b, c.Description)
	if err != nil {
		return Card{}, c, err
	}
	payload := map[string]any{"list_id": l.ID, "list_name": l.Name}
	for k, v := range extra {
		payload[k] = v
	}
	out, err := s.cardEvent(ctx, tx, a, b, c, TCardCreated, payload,
		notifyGroup{Pref: PrefCardMember, Users: added}, notifyGroup{Pref: PrefMention, Users: mentioned})
	return out, c, err
}

func (s *Service) CreateCard(ctx context.Context, listID string, in CreateCard) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	l, b, err := s.getListForEdit(ctx, a, listID)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, _, err = s.createCardTx(ctx, tx, a, b, l, newCard{
			Title: in.Title, Description: deref(in.Description, ""), DueAt: in.DueAt, StartAt: in.StartAt,
			IsTemplate: deref(in.IsTemplate, false), LabelIDs: deref(in.LabelIds, nil), MemberIDs: deref(in.MemberIds, nil),
			Placement: Placement{AfterId: in.AfterId, BeforeId: in.BeforeId},
		}, nil)
		return err
	})
	return out, err
}

func (s *Service) GetCard(ctx context.Context, id string) (CardDetail, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return CardDetail{}, err
	}
	c, _, err := s.loadCard(ctx, a, id, ActBoardView)
	if err != nil {
		return CardDetail{}, err
	}
	return s.cardDetail(ctx, a, c)
}

func (s *Service) cardDetail(ctx context.Context, a *Actor, c store.Card) (CardDetail, error) {
	q := s.DB.R
	base, err := cardOut(ctx, q, c)
	if err != nil {
		return CardDetail{}, err
	}
	d := CardDetail{
		Id: base.Id, ListId: base.ListId, BoardId: base.BoardId, Title: base.Title, Description: base.Description,
		Position: base.Position, DueAt: base.DueAt, DueDone: base.DueDone, StartAt: base.StartAt,
		CoverAttachmentId: base.CoverAttachmentId, IsTemplate: base.IsTemplate, RecurrenceId: base.RecurrenceId,
		Archived: base.Archived, LabelIds: base.LabelIds, MemberIds: base.MemberIds, Counts: base.Counts,
		CreatedBy: base.CreatedBy, CreatedAt: base.CreatedAt, UpdatedAt: base.UpdatedAt, Version: base.Version,
		Checklists: []Checklist{}, Attachments: []Attachment{}, Links: []CardLink{},
	}
	lists, err := q.ListCardChecklists(ctx, c.ID)
	if err != nil {
		return d, err
	}
	items, err := q.ListCardChecklistItems(ctx, c.ID)
	if err != nil {
		return d, err
	}
	for _, cl := range lists {
		d.Checklists = append(d.Checklists, checklistOut(cl, items))
	}
	atts, err := q.ListCardAttachments(ctx, c.ID)
	if err != nil {
		return d, err
	}
	for _, at := range atts {
		d.Attachments = append(d.Attachments, attachmentOut(at))
	}
	links, err := q.ListCardLinks(ctx, c.ID)
	if err != nil {
		return d, err
	}
	for _, l := range links {
		d.Links = append(d.Links, linkOut(l))
	}
	n, err := q.IsCardWatcher(ctx, store.IsCardWatcherParams{CardID: c.ID, UserID: a.User.ID})
	d.Watching = n > 0
	return d, err
}

func (s *Service) UpdateCard(ctx context.Context, id, ifMatch string, in UpdateCard) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	_, b, err := s.loadCard(ctx, a, id, ActBoardEdit)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		c, err := tx.GetCard(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, c.Version); err != nil {
			return err
		}
		p := store.UpdateCardParams{
			ID: id, Title: c.Title, Description: c.Description, DueAt: c.DueAt, DueDone: c.DueDone, StartAt: c.StartAt,
			CoverAttachmentID: c.CoverAttachmentID, IsTemplate: c.IsTemplate, ArchivedAt: c.ArchivedAt,
			DueSoonNotifiedAt: c.DueSoonNotifiedAt, OverdueNotifiedAt: c.OverdueNotifiedAt, UpdatedAt: s.now(),
		}
		changes := map[string]any{}
		if in.Title != nil {
			if p.Title = strings.TrimSpace(*in.Title); p.Title == "" {
				return badRequest("title is required")
			}
			if p.Title != c.Title {
				changes["title"] = p.Title
				changes["previous_title"] = c.Title
			}
		}
		if in.Description != nil && *in.Description != c.Description {
			p.Description = *in.Description
			changes["description"] = true
		}
		if in.DueAt.IsSpecified() {
			if in.DueAt.IsNull() {
				p.DueAt = sql.NullTime{}
			} else {
				v, _ := in.DueAt.Get()
				p.DueAt = nt(v)
			}
			if p.DueAt != c.DueAt {
				changes["due_at"] = timep(p.DueAt)
				// New due date, so reminders can fire again.
				p.DueSoonNotifiedAt, p.OverdueNotifiedAt = sql.NullTime{}, sql.NullTime{}
			}
		}
		if in.DueDone != nil && *in.DueDone != c.DueDone {
			p.DueDone = *in.DueDone
			changes["due_done"] = p.DueDone
		}
		if in.StartAt.IsSpecified() {
			if in.StartAt.IsNull() {
				p.StartAt = sql.NullTime{}
			} else {
				v, _ := in.StartAt.Get()
				p.StartAt = nt(v)
			}
			changes["start_at"] = timep(p.StartAt)
		}
		if in.CoverAttachmentId.IsSpecified() {
			if in.CoverAttachmentId.IsNull() {
				p.CoverAttachmentID = sql.NullString{}
			} else {
				v, _ := in.CoverAttachmentId.Get()
				att, err := tx.GetAttachment(ctx, v)
				if err != nil || att.CardID != id {
					return badRequest("cover_attachment_id is not an attachment on this card")
				}
				p.CoverAttachmentID = ns(v)
			}
			changes["cover"] = true
		}
		if in.IsTemplate != nil && *in.IsTemplate != c.IsTemplate {
			p.IsTemplate = *in.IsTemplate
			changes["is_template"] = p.IsTemplate
		}
		if in.Archived != nil && *in.Archived != c.ArchivedAt.Valid {
			if *in.Archived {
				p.ArchivedAt = nt(s.now())
			} else {
				p.ArchivedAt = sql.NullTime{}
			}
			changes["archived"] = *in.Archived
		}
		nc, err := tx.UpdateCard(ctx, p)
		if err != nil {
			return err
		}
		if err := search.IndexCard(ctx, tx.Conn, nc); err != nil {
			return err
		}
		var groups []notifyGroup
		if changes["description"] != nil {
			// Notify only people newly mentioned.
			before, err := s.mentionedUsers(ctx, b, c.Description)
			if err != nil {
				return err
			}
			after, err := s.mentionedUsers(ctx, b, nc.Description)
			if err != nil {
				return err
			}
			groups = append(groups, notifyGroup{Pref: PrefMention, Users: slices.DeleteFunc(after, func(id string) bool { return slices.Contains(before, id) })})
		}
		out, err = s.cardEvent(ctx, tx, a, b, nc, TCardUpdated, map[string]any{"changes": changes}, groups...)
		return err
	})
	return out, err
}

func (s *Service) MoveCard(ctx context.Context, id string, in MoveCard) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	_, b, err := s.loadCard(ctx, a, id, ActBoardEdit)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, err = s.moveCardTx(ctx, tx, a, b, id, in.ListId, Placement{AfterId: in.AfterId, BeforeId: in.BeforeId}, nil)
		return err
	})
	return out, err
}

func (s *Service) moveCardTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, id, listID string, p Placement, extra map[string]any) (Card, error) {
	c, err := tx.GetCard(ctx, id)
	if err != nil {
		return Card{}, notFound(err)
	}
	to, err := tx.GetList(ctx, listID)
	if err != nil || to.BoardID != c.BoardID {
		return Card{}, badRequest("list_id is not a list on this card's board")
	}
	from, err := tx.GetList(ctx, c.ListID)
	if err != nil {
		return Card{}, err
	}
	pos, err := s.placeCard(ctx, tx, b.ID, to.ID, id, p)
	if err != nil {
		return Card{}, err
	}
	nc, err := tx.MoveCard(ctx, store.MoveCardParams{ID: id, ListID: to.ID, BoardID: c.BoardID, Position: pos, UpdatedAt: s.now()})
	if err != nil {
		return Card{}, err
	}
	payload := map[string]any{"from_list_id": from.ID, "from_list_name": from.Name, "to_list_id": to.ID, "to_list_name": to.Name}
	for k, v := range extra {
		payload[k] = v
	}
	return s.cardEvent(ctx, tx, a, b, nc, TCardMoved, payload)
}

// DeleteCard removes a card for good. Recurrences that use it as their
// template are paused and their creators told.
func (s *Service) DeleteCard(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	c, b, err := s.loadCard(ctx, a, id, ActBoardEdit)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		keys, err := tx.ListCardAttachmentKeys(ctx, id)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := tx.QueueBlobDeletion(ctx, store.QueueBlobDeletionParams{ObjectKey: k, CreatedAt: s.now()}); err != nil {
				return err
			}
		}
		recs, err := tx.ListTemplateRecurrences(ctx, ns(id))
		if err != nil {
			return err
		}
		for _, r := range recs {
			if err := s.pauseRecurrenceTx(ctx, tx, a, b, r, "template card deleted"); err != nil {
				return err
			}
		}
		if err := search.RemoveCard(ctx, tx.Conn, id); err != nil {
			return err
		}
		if err := tx.DeleteCard(ctx, id); err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: id, Type: TCardDeleted,
			Payload: map[string]any{"card_title": c.Title, "card_id": id, "list_id": c.ListID}})
		return err
	})
}

// ---- Members, labels, watchers ----

func (s *Service) AddCardMember(ctx context.Context, cardID, userID string) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return Card{}, err
	}
	if !s.canSeeBoard(ctx, userID, b) {
		return Card{}, badRequest("that user cannot see this board")
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		c, err := tx.GetCard(ctx, cardID)
		if err != nil {
			return notFound(err)
		}
		n, err := tx.AddCardMember(ctx, store.AddCardMemberParams{CardID: cardID, UserID: userID, CreatedAt: s.now()})
		if err != nil {
			return err
		}
		if n == 0 {
			out, err = cardOut(ctx, tx.Queries, c)
			return err
		}
		u, _ := tx.GetUser(ctx, userID)
		out, err = s.cardEvent(ctx, tx, a, b, c, TCardMemberAdded, map[string]any{"user_id": userID, "user_name": u.DisplayName},
			notifyGroup{Pref: PrefCardMember, Users: []string{userID}})
		return err
	})
	return out, err
}

func (s *Service) RemoveCardMember(ctx context.Context, cardID, userID string) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		c, err := tx.GetCard(ctx, cardID)
		if err != nil {
			return notFound(err)
		}
		n, err := tx.RemoveCardMember(ctx, store.RemoveCardMemberParams{CardID: cardID, UserID: userID})
		if err != nil {
			return err
		}
		if n == 0 {
			out, err = cardOut(ctx, tx.Queries, c)
			return err
		}
		u, _ := tx.GetUser(ctx, userID)
		out, err = s.cardEvent(ctx, tx, a, b, c, TCardMemberRemoved, map[string]any{"user_id": userID, "user_name": u.DisplayName})
		return err
	})
	return out, err
}

func (s *Service) AddCardLabel(ctx context.Context, cardID, labelID string) (Card, error) {
	return s.setCardLabel(ctx, cardID, labelID, true)
}

func (s *Service) RemoveCardLabel(ctx context.Context, cardID, labelID string) (Card, error) {
	return s.setCardLabel(ctx, cardID, labelID, false)
}

func (s *Service) setCardLabel(ctx context.Context, cardID, labelID string, on bool) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, err = s.setCardLabelTx(ctx, tx, a, b, cardID, labelID, on)
		return err
	})
	return out, err
}

func (s *Service) setCardLabelTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, cardID, labelID string, on bool) (Card, error) {
	c, err := tx.GetCard(ctx, cardID)
	if err != nil {
		return Card{}, notFound(err)
	}
	lbl, err := tx.GetLabel(ctx, labelID)
	if err != nil || lbl.BoardID != b.ID {
		return Card{}, badRequest("label is not on this board")
	}
	var n int64
	typ := TCardLabelAdded
	if on {
		n, err = tx.AddCardLabel(ctx, store.AddCardLabelParams{CardID: cardID, LabelID: labelID})
	} else {
		typ = TCardLabelRemoved
		n, err = tx.RemoveCardLabel(ctx, store.RemoveCardLabelParams{CardID: cardID, LabelID: labelID})
	}
	if err != nil {
		return Card{}, err
	}
	if n == 0 {
		return cardOut(ctx, tx.Queries, c)
	}
	return s.cardEvent(ctx, tx, a, b, c, typ, map[string]any{"label_id": labelID, "label_name": lbl.Name, "label_colour": lbl.Colour})
}

func (s *Service) SetWatching(ctx context.Context, cardID string, watch bool) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	if _, _, err := s.loadCard(ctx, a, cardID, ActBoardView); err != nil {
		return err
	}
	if !a.CanWrite() {
		return forbidden("this token is read-only")
	}
	w := store.New(s.DB.Write)
	if watch {
		_, err = w.AddCardWatcher(ctx, store.AddCardWatcherParams{CardID: cardID, UserID: a.User.ID, CreatedAt: s.now()})
	} else {
		_, err = w.RemoveCardWatcher(ctx, store.RemoveCardWatcherParams{CardID: cardID, UserID: a.User.ID})
	}
	return err
}

// ListCardTemplates returns the template cards on a board.
func (s *Service) ListCardTemplates(ctx context.Context, boardID string) ([]Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadBoard(ctx, a, boardID, ActBoardView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListBoardTemplateCards(ctx, boardID)
	if err != nil {
		return nil, err
	}
	out := []Card{}
	for _, c := range rows {
		co, err := cardOut(ctx, s.DB.R, c)
		if err != nil {
			return nil, err
		}
		out = append(out, co)
	}
	return out, nil
}
