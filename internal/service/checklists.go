package service

import (
	"context"
	"database/sql"
	"strings"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/search"
	"github.com/edward-murrell/kowhai/internal/store"
)

func (s *Service) checklistEvent(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, cardID, typ string, payload map[string]any) error {
	c, err := tx.GetCard(ctx, cardID)
	if err != nil {
		return err
	}
	// Send the card too, since its checklist counts changed.
	co, err := cardOut(ctx, tx.Queries, c)
	if err != nil {
		return err
	}
	payload["card_title"] = c.Title
	payload["card"] = co
	_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: cardID, Type: typ, Payload: payload})
	return err
}

func (s *Service) loadChecklist(ctx context.Context, a *Actor, id string) (store.Checklist, store.Board, error) {
	cl, err := s.DB.R.GetChecklist(ctx, id)
	if err != nil {
		return cl, store.Board{}, notFound(err)
	}
	_, b, err := s.loadCard(ctx, a, cl.CardID, ActBoardEdit)
	return cl, b, err
}

func (s *Service) CreateChecklist(ctx context.Context, cardID string, in CreateChecklist) (Checklist, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Checklist{}, err
	}
	_, b, err := s.loadCard(ctx, a, cardID, ActBoardEdit)
	if err != nil {
		return Checklist{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Checklist{}, badRequest("title is required")
	}
	var out Checklist
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		cl, err := s.createChecklistTx(ctx, tx, cardID, title)
		if err != nil {
			return err
		}
		out = checklistOut(cl, nil)
		return s.checklistEvent(ctx, tx, a, b, cardID, TChecklistCreated, map[string]any{"title": title, "entity": out})
	})
	return out, err
}

func (s *Service) createChecklistTx(ctx context.Context, tx *store.Tx, cardID, title string) (store.Checklist, error) {
	sibs, err := tx.ListCardChecklists(ctx, cardID)
	if err != nil {
		return store.Checklist{}, err
	}
	ss := make([]sibling, len(sibs))
	for i, x := range sibs {
		ss[i] = sibling{x.ID, x.Position}
	}
	pos, respaced, err := placeAmong(ss, "", "", "")
	if err != nil {
		return store.Checklist{}, err
	}
	for _, r := range respaced {
		if _, err := tx.Conn.ExecContext(ctx, `UPDATE checklists SET position = ? WHERE id = ?`, r.Pos, r.ID); err != nil {
			return store.Checklist{}, err
		}
	}
	now := s.now()
	return tx.CreateChecklist(ctx, store.CreateChecklistParams{ID: s.NewID(), CardID: cardID, Title: title, Position: pos, CreatedAt: now, UpdatedAt: now})
}

func (s *Service) UpdateChecklist(ctx context.Context, id, ifMatch string, in UpdateChecklist) (Checklist, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Checklist{}, err
	}
	_, b, err := s.loadChecklist(ctx, a, id)
	if err != nil {
		return Checklist{}, err
	}
	var out Checklist
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		cl, err := tx.GetChecklist(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, cl.Version); err != nil {
			return err
		}
		p := store.UpdateChecklistParams{ID: id, Title: cl.Title, Position: cl.Position, UpdatedAt: s.now()}
		if in.Title != nil {
			if p.Title = strings.TrimSpace(*in.Title); p.Title == "" {
				return badRequest("title is required")
			}
		}
		if in.AfterId != nil || in.BeforeId != nil {
			sibs, err := tx.ListCardChecklists(ctx, cl.CardID)
			if err != nil {
				return err
			}
			ss := make([]sibling, len(sibs))
			for i, x := range sibs {
				ss[i] = sibling{x.ID, x.Position}
			}
			pos, respaced, err := placeAmong(ss, id, deref(in.AfterId, ""), deref(in.BeforeId, ""))
			if err != nil {
				return err
			}
			for _, r := range respaced {
				if _, err := tx.Conn.ExecContext(ctx, `UPDATE checklists SET position = ? WHERE id = ?`, r.Pos, r.ID); err != nil {
					return err
				}
			}
			p.Position = pos
		}
		ncl, err := tx.UpdateChecklist(ctx, p)
		if err != nil {
			return err
		}
		items, err := tx.ListChecklistItems(ctx, id)
		if err != nil {
			return err
		}
		out = checklistOut(ncl, items)
		return s.checklistEvent(ctx, tx, a, b, cl.CardID, TChecklistUpdated, map[string]any{"title": ncl.Title, "entity": out})
	})
	return out, err
}

func (s *Service) DeleteChecklist(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	cl, b, err := s.loadChecklist(ctx, a, id)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		items, err := tx.ListChecklistItems(ctx, id)
		if err != nil {
			return err
		}
		ids := make([]string, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		if err := search.RemoveItems(ctx, tx.Conn, ids); err != nil {
			return err
		}
		if err := tx.DeleteChecklist(ctx, id); err != nil {
			return err
		}
		return s.checklistEvent(ctx, tx, a, b, cl.CardID, TChecklistDeleted, map[string]any{"title": cl.Title, "checklist_id": id})
	})
}

// ---- Items ----

func (s *Service) CreateChecklistItem(ctx context.Context, checklistID string, in CreateChecklistItem) (ChecklistItem, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return ChecklistItem{}, err
	}
	cl, b, err := s.loadChecklist(ctx, a, checklistID)
	if err != nil {
		return ChecklistItem{}, err
	}
	var out ChecklistItem
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		it, err := s.createItemTx(ctx, tx, b, cl, in)
		if err != nil {
			return err
		}
		out = checklistItemOut(it)
		return s.checklistEvent(ctx, tx, a, b, cl.CardID, TItemCreated, map[string]any{"text": it.Text, "entity": out})
	})
	return out, err
}

func (s *Service) createItemTx(ctx context.Context, tx *store.Tx, b store.Board, cl store.Checklist, in CreateChecklistItem) (store.ChecklistItem, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return store.ChecklistItem{}, badRequest("text is required")
	}
	if in.AssigneeId != nil && *in.AssigneeId != "" && !s.canSeeBoard(ctx, *in.AssigneeId, b) {
		return store.ChecklistItem{}, badRequest("assignee cannot see this board")
	}
	sibs, err := tx.ListChecklistItems(ctx, cl.ID)
	if err != nil {
		return store.ChecklistItem{}, err
	}
	ss := make([]sibling, len(sibs))
	for i, x := range sibs {
		ss[i] = sibling{x.ID, x.Position}
	}
	pos, respaced, err := placeAmong(ss, "", "", "")
	if err != nil {
		return store.ChecklistItem{}, err
	}
	for _, r := range respaced {
		if _, err := tx.Conn.ExecContext(ctx, `UPDATE checklist_items SET position = ? WHERE id = ?`, r.Pos, r.ID); err != nil {
			return store.ChecklistItem{}, err
		}
	}
	now := s.now()
	it, err := tx.CreateChecklistItem(ctx, store.CreateChecklistItemParams{
		ID: s.NewID(), ChecklistID: cl.ID, CardID: cl.CardID, Text: text, Done: deref(in.Done, false), Position: pos,
		AssigneeID: nsp(in.AssigneeId), DueAt: ntp(in.DueAt), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return it, err
	}
	return it, search.IndexItem(ctx, tx.Conn, it, b.ID)
}

func (s *Service) loadItem(ctx context.Context, a *Actor, id string) (store.ChecklistItem, store.Board, error) {
	it, err := s.DB.R.GetChecklistItem(ctx, id)
	if err != nil {
		return it, store.Board{}, notFound(err)
	}
	_, b, err := s.loadCard(ctx, a, it.CardID, ActBoardEdit)
	return it, b, err
}

func (s *Service) UpdateChecklistItem(ctx context.Context, id, ifMatch string, in UpdateChecklistItem) (ChecklistItem, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return ChecklistItem{}, err
	}
	_, b, err := s.loadItem(ctx, a, id)
	if err != nil {
		return ChecklistItem{}, err
	}
	var out ChecklistItem
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, err = s.updateItemTx(ctx, tx, a, b, id, ifMatch, in)
		return err
	})
	return out, err
}

func (s *Service) updateItemTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, id, ifMatch string, in UpdateChecklistItem) (ChecklistItem, error) {
	it, err := tx.GetChecklistItem(ctx, id)
	if err != nil {
		return ChecklistItem{}, notFound(err)
	}
	if err := CheckVersion(ifMatch, it.Version); err != nil {
		return ChecklistItem{}, err
	}
	p := store.UpdateChecklistItemParams{ID: id, Text: it.Text, Done: it.Done, Position: it.Position, AssigneeID: it.AssigneeID, DueAt: it.DueAt, UpdatedAt: s.now()}
	checklistID := it.ChecklistID
	changes := map[string]any{}
	if in.Text != nil {
		if p.Text = strings.TrimSpace(*in.Text); p.Text == "" {
			return ChecklistItem{}, badRequest("text is required")
		}
		changes["text"] = p.Text
	}
	if in.Done != nil && *in.Done != it.Done {
		p.Done = *in.Done
		changes["done"] = p.Done
	}
	if in.AssigneeId.IsSpecified() {
		if in.AssigneeId.IsNull() {
			p.AssigneeID = sql.NullString{}
		} else {
			v, _ := in.AssigneeId.Get()
			if v != "" && !s.canSeeBoard(ctx, v, b) {
				return ChecklistItem{}, badRequest("assignee cannot see this board")
			}
			p.AssigneeID = ns(v)
		}
		changes["assignee_id"] = strp(p.AssigneeID)
	}
	if in.DueAt.IsSpecified() {
		if in.DueAt.IsNull() {
			p.DueAt = sql.NullTime{}
		} else {
			v, _ := in.DueAt.Get()
			p.DueAt = nt(v)
		}
		changes["due_at"] = timep(p.DueAt)
	}
	moving := in.AfterId != nil || in.BeforeId != nil || (in.ChecklistId != nil && *in.ChecklistId != it.ChecklistID)
	if in.ChecklistId != nil && *in.ChecklistId != it.ChecklistID {
		dest, err := tx.GetChecklist(ctx, *in.ChecklistId)
		if err != nil || dest.CardID != it.CardID {
			return ChecklistItem{}, badRequest("checklist_id is not a checklist on this card")
		}
		checklistID = dest.ID
	}
	if moving {
		sibs, err := tx.ListChecklistItems(ctx, checklistID)
		if err != nil {
			return ChecklistItem{}, err
		}
		ss := make([]sibling, len(sibs))
		for i, x := range sibs {
			ss[i] = sibling{x.ID, x.Position}
		}
		pos, respaced, err := placeAmong(ss, id, deref(in.AfterId, ""), deref(in.BeforeId, ""))
		if err != nil {
			return ChecklistItem{}, err
		}
		for _, r := range respaced {
			if _, err := tx.Conn.ExecContext(ctx, `UPDATE checklist_items SET position = ? WHERE id = ?`, r.Pos, r.ID); err != nil {
				return ChecklistItem{}, err
			}
		}
		p.Position = pos
		if checklistID != it.ChecklistID {
			if _, err := tx.Conn.ExecContext(ctx, `UPDATE checklist_items SET checklist_id = ? WHERE id = ?`, checklistID, id); err != nil {
				return ChecklistItem{}, err
			}
		}
		changes["moved"] = true
	}
	nit, err := tx.UpdateChecklistItem(ctx, p)
	if err != nil {
		return ChecklistItem{}, err
	}
	if err := search.IndexItem(ctx, tx.Conn, nit, b.ID); err != nil {
		return ChecklistItem{}, err
	}
	out := checklistItemOut(nit)
	return out, s.checklistEvent(ctx, tx, a, b, it.CardID, TItemUpdated, map[string]any{"text": nit.Text, "changes": changes, "entity": out})
}

func (s *Service) DeleteChecklistItem(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	it, b, err := s.loadItem(ctx, a, id)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if err := search.Remove(ctx, tx.Conn, search.TypeItem, id); err != nil {
			return err
		}
		if err := tx.DeleteChecklistItem(ctx, id); err != nil {
			return err
		}
		return s.checklistEvent(ctx, tx, a, b, it.CardID, TItemDeleted,
			map[string]any{"text": it.Text, "item_id": id, "checklist_id": it.ChecklistID})
	})
}
