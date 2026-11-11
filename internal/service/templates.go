package service

import (
	"context"
	"strings"
	"time"

	. "kowhai/internal/apitypes"
	"kowhai/internal/position"
	"kowhai/internal/search"
	"kowhai/internal/store"
	tmpl "kowhai/internal/template"
)

// ---- Board copies and templates ----

func (s *Service) ListBuiltinTemplates(ctx context.Context, wsID string) ([]BuiltinTemplate, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceView); err != nil {
		return nil, err
	}
	existing, err := s.DB.R.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: wsID, IsTemplate: true})
	if err != nil {
		return nil, err
	}
	var out []BuiltinTemplate
	for _, t := range tmpl.Builtins {
		bt := BuiltinTemplate{Key: t.Key, Name: t.Name, Description: t.Description}
		for _, b := range existing {
			if b.Name == t.Name {
				bt.BoardId = ptr(b.ID)
			}
		}
		out = append(out, bt)
	}
	return out, nil
}

// SeedBuiltinTemplate adds a built-in template board to a workspace, or
// returns the one already there.
func (s *Service) SeedBuiltinTemplate(ctx context.Context, wsID, key string) (Board, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Board{}, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActBoardCreate); err != nil {
		return Board{}, err
	}
	def, ok := tmpl.Find(key)
	if !ok {
		return Board{}, badRequest("unknown template %q", key)
	}
	var out store.Board
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		existing, err := tx.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: wsID, IsTemplate: true})
		if err != nil {
			return err
		}
		for _, b := range existing {
			if b.Name == def.Name {
				out = b
				return nil
			}
		}
		out, err = s.seedTx(ctx, tx, a, wsID, def)
		return err
	})
	if err != nil {
		return Board{}, err
	}
	role, err := s.boardRole(ctx, a, out)
	return boardOut(out, role), err
}

func (s *Service) seedTx(ctx context.Context, tx *store.Tx, a *Actor, wsID string, def tmpl.Board) (store.Board, error) {
	now := s.now()
	b, err := tx.CreateBoard(ctx, store.CreateBoardParams{
		ID: s.NewID(), WorkspaceID: wsID, Name: def.Name, Description: def.Description, Visibility: "workspace",
		IsTemplate: true, Settings: "{}", CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return b, err
	}
	if err := tx.UpsertBoardMember(ctx, store.UpsertBoardMemberParams{BoardID: b.ID, UserID: a.User.ID, Role: "admin", CreatedAt: now}); err != nil {
		return b, err
	}
	labels := map[string]string{}
	for _, l := range def.Labels {
		row, err := tx.CreateLabel(ctx, store.CreateLabelParams{ID: s.NewID(), BoardID: b.ID, Name: l.Name, Colour: l.Colour, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return b, err
		}
		labels[l.Name] = row.ID
	}
	listKeys := position.Spread(len(def.Lists))
	listIDs := map[string]string{}
	for i, l := range def.Lists {
		row, err := tx.CreateList(ctx, store.CreateListParams{ID: s.NewID(), BoardID: b.ID, Name: l.Name, Position: listKeys[i], CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return b, err
		}
		listIDs[l.Name] = row.ID
		cardKeys := position.Spread(len(l.Cards))
		for j, c := range l.Cards {
			card, err := tx.CreateCard(ctx, store.CreateCardParams{
				ID: s.NewID(), ListID: row.ID, BoardID: b.ID, Title: c.Title, Description: c.Description,
				Position: cardKeys[j], IsTemplate: c.IsTemplate, CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return b, err
			}
			for _, ln := range c.Labels {
				if id := labels[ln]; id != "" {
					if _, err := tx.AddCardLabel(ctx, store.AddCardLabelParams{CardID: card.ID, LabelID: id}); err != nil {
						return b, err
					}
				}
			}
			clKeys := position.Spread(len(c.Checklists))
			for k, cl := range c.Checklists {
				clRow, err := tx.CreateChecklist(ctx, store.CreateChecklistParams{ID: s.NewID(), CardID: card.ID, Title: cl.Title, Position: clKeys[k], CreatedAt: now, UpdatedAt: now})
				if err != nil {
					return b, err
				}
				itemKeys := position.Spread(len(cl.Items))
				for m, text := range cl.Items {
					if _, err := tx.CreateChecklistItem(ctx, store.CreateChecklistItemParams{ID: s.NewID(), ChecklistID: clRow.ID, CardID: card.ID, Text: text, Position: itemKeys[m], CreatedAt: now, UpdatedAt: now}); err != nil {
						return b, err
					}
				}
			}
			if err := search.IndexCard(ctx, tx.Conn, card); err != nil {
				return b, err
			}
		}
	}
	if len(def.StatusLists) > 0 {
		m := map[string]string{}
		for status, name := range def.StatusLists {
			m[status] = listIDs[name]
		}
		st := BoardSettings{StatusListsEnabled: ptr(true), StatusLists: &m}
		if b, err = tx.UpdateBoard(ctx, store.UpdateBoardParams{ID: b.ID, Name: b.Name, Description: b.Description, Visibility: b.Visibility,
			Background: b.Background, IsTemplate: true, Settings: mustJSON(st), UpdatedAt: now}); err != nil {
			return b, err
		}
	}
	_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: wsID, BoardID: b.ID, Type: TBoardCreated,
		Payload: map[string]any{"name": b.Name, "builtin": def.Key, "entity": boardOut(b, "")}})
	return b, err
}

// CopyBoard copies a board, or makes a board from a template. Lists,
// labels, settings and template cards are always copied. Other cards and
// their checklists are copied when IncludeCards is set. Members,
// comments, attachments and activity are not copied.
func (s *Service) CopyBoard(ctx context.Context, srcID string, in CopyBoard) (Board, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Board{}, err
	}
	src, err := s.loadBoard(ctx, a, srcID, ActBoardView)
	if err != nil {
		return Board{}, err
	}
	wsID := deref(in.WorkspaceId, src.WorkspaceID)
	if _, err := s.loadWorkspace(ctx, a, wsID, ActBoardCreate); err != nil {
		return Board{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Board{}, badRequest("name is required")
	}
	vis := src.Visibility
	if in.Visibility != nil {
		vis = string(*in.Visibility)
	}
	if !validVisibility(vis) {
		return Board{}, badRequest("visibility must be private or workspace")
	}
	includeCards := deref(in.IncludeCards, false)
	var out store.Board
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		now := s.now()
		b, err := tx.CreateBoard(ctx, store.CreateBoardParams{
			ID: s.NewID(), WorkspaceID: wsID, Name: name, Description: src.Description, Visibility: vis,
			Background: src.Background, IsTemplate: deref(in.AsTemplate, false), Settings: "{}",
			CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := tx.UpsertBoardMember(ctx, store.UpsertBoardMemberParams{BoardID: b.ID, UserID: a.User.ID, Role: "admin", CreatedAt: now}); err != nil {
			return err
		}
		labelMap := map[string]string{}
		labels, err := tx.ListBoardLabels(ctx, src.ID)
		if err != nil {
			return err
		}
		for _, l := range labels {
			row, err := tx.CreateLabel(ctx, store.CreateLabelParams{ID: s.NewID(), BoardID: b.ID, Name: l.Name, Colour: l.Colour, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			labelMap[l.ID] = row.ID
		}
		listMap := map[string]string{}
		lists, err := tx.ListBoardLists(ctx, store.ListBoardListsParams{BoardID: src.ID})
		if err != nil {
			return err
		}
		ncards := 0
		for _, l := range lists {
			row, err := tx.CreateList(ctx, store.CreateListParams{ID: s.NewID(), BoardID: b.ID, Name: l.Name, Position: l.Position, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			listMap[l.ID] = row.ID
			cards, err := tx.ListAllListCards(ctx, l.ID)
			if err != nil {
				return err
			}
			for _, c := range cards {
				if c.ArchivedAt.Valid || (!c.IsTemplate && !includeCards) {
					continue
				}
				if _, err := s.copyCardRows(ctx, tx, a, c, b.ID, row.ID, c.Position, copyOpts{Title: c.Title, LabelMap: labelMap, AsTemplate: ptr(c.IsTemplate)}); err != nil {
					return err
				}
				ncards++
			}
		}
		st := parseSettings(src.Settings)
		if st.StatusLists != nil {
			m := map[string]string{}
			for k, v := range *st.StatusLists {
				if nv := listMap[v]; nv != "" {
					m[k] = nv
				}
			}
			st.StatusLists = &m
		}
		if out, err = tx.UpdateBoard(ctx, store.UpdateBoardParams{ID: b.ID, Name: b.Name, Description: b.Description, Visibility: b.Visibility,
			Background: b.Background, IsTemplate: b.IsTemplate, Settings: mustJSON(st), UpdatedAt: now}); err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: wsID, BoardID: b.ID, Type: TBoardCreated,
			Payload: map[string]any{"name": b.Name, "from_board_id": src.ID, "from_board_name": src.Name, "from_template": src.IsTemplate, "cards": ncards, "entity": boardOut(out, "")}})
		return err
	})
	if err != nil {
		return Board{}, err
	}
	return boardOut(out, "admin"), nil
}

// ---- Card copies ----

type copyOpts struct {
	Title          string
	IncludeMembers bool
	AsTemplate     *bool
	RecurrenceID   string
	DueAt          *time.Time
	Placement      Placement
	// LabelMap maps source label IDs to target label IDs. Nil means
	// match labels by name and colour on the target board.
	LabelMap map[string]string
}

// copyCardRows inserts a copy of src with its labels, checklists and
// optionally members. It writes no activity.
func (s *Service) copyCardRows(ctx context.Context, tx *store.Tx, a *Actor, src store.Card, boardID, listID, pos string, o copyOpts) (store.Card, error) {
	now := s.now()
	title := strings.TrimSpace(o.Title)
	if title == "" {
		title = src.Title
	}
	due := src.DueAt
	if o.DueAt != nil {
		due = nt(*o.DueAt)
	}
	isTemplate := false
	if o.AsTemplate != nil {
		isTemplate = *o.AsTemplate
	}
	c, err := tx.CreateCard(ctx, store.CreateCardParams{
		ID: s.NewID(), ListID: listID, BoardID: boardID, Title: title, Description: src.Description, Position: pos,
		DueAt: due, StartAt: src.StartAt, IsTemplate: isTemplate, RecurrenceID: ns(o.RecurrenceID),
		CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return c, err
	}
	labelIDs, err := tx.ListCardLabelIDs(ctx, src.ID)
	if err != nil {
		return c, err
	}
	labelMap := o.LabelMap
	if labelMap == nil {
		labelMap = map[string]string{}
		if boardID == src.BoardID {
			for _, id := range labelIDs {
				labelMap[id] = id
			}
		} else {
			dst, err := tx.ListBoardLabels(ctx, boardID)
			if err != nil {
				return c, err
			}
			for _, id := range labelIDs {
				l, err := tx.GetLabel(ctx, id)
				if err != nil {
					continue
				}
				for _, d := range dst {
					if d.Name == l.Name && d.Colour == l.Colour {
						labelMap[id] = d.ID
					}
				}
			}
		}
	}
	for _, id := range labelIDs {
		if nid := labelMap[id]; nid != "" {
			if _, err := tx.AddCardLabel(ctx, store.AddCardLabelParams{CardID: c.ID, LabelID: nid}); err != nil {
				return c, err
			}
		}
	}
	if o.IncludeMembers {
		members, err := tx.ListCardMembers(ctx, src.ID)
		if err != nil {
			return c, err
		}
		b, err := tx.GetBoard(ctx, boardID)
		if err != nil {
			return c, err
		}
		for _, uid := range members {
			if s.canSeeBoard(ctx, uid, b) {
				if _, err := tx.AddCardMember(ctx, store.AddCardMemberParams{CardID: c.ID, UserID: uid, CreatedAt: now}); err != nil {
					return c, err
				}
			}
		}
	}
	lists, err := tx.ListCardChecklists(ctx, src.ID)
	if err != nil {
		return c, err
	}
	items, err := tx.ListCardChecklistItems(ctx, src.ID)
	if err != nil {
		return c, err
	}
	for _, cl := range lists {
		ncl, err := tx.CreateChecklist(ctx, store.CreateChecklistParams{ID: s.NewID(), CardID: c.ID, Title: cl.Title, Position: cl.Position, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return c, err
		}
		for _, it := range items {
			if it.ChecklistID != cl.ID {
				continue
			}
			nit, err := tx.CreateChecklistItem(ctx, store.CreateChecklistItemParams{
				ID: s.NewID(), ChecklistID: ncl.ID, CardID: c.ID, Text: it.Text, Position: it.Position,
				AssigneeID: it.AssigneeID, CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return c, err
			}
			if !isTemplate {
				if err := search.IndexItem(ctx, tx.Conn, nit, boardID); err != nil {
					return c, err
				}
			}
		}
	}
	return c, search.IndexCard(ctx, tx.Conn, c)
}

// copyCardTx copies src into list and records one card.created activity.
func (s *Service) copyCardTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, src store.Card, list store.List, o copyOpts) (Card, error) {
	pos, err := s.placeCard(ctx, tx, b.ID, list.ID, "", o.Placement)
	if err != nil {
		return Card{}, err
	}
	c, err := s.copyCardRows(ctx, tx, a, src, b.ID, list.ID, pos, o)
	if err != nil {
		return Card{}, err
	}
	payload := map[string]any{"list_id": list.ID, "list_name": list.Name, "from_card_id": src.ID, "from_template": src.IsTemplate}
	if o.RecurrenceID != "" {
		payload["recurrence_id"] = o.RecurrenceID
	}
	members, err := tx.ListCardMembers(ctx, c.ID)
	if err != nil {
		return Card{}, err
	}
	return s.cardEvent(ctx, tx, a, b, c, TCardCreated, payload, notifyGroup{Pref: PrefCardMember, Users: members})
}

// CopyCard copies a card, or makes a card from a card template.
func (s *Service) CopyCard(ctx context.Context, srcID string, in CopyCard) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	src, _, err := s.loadCard(ctx, a, srcID, ActBoardView)
	if err != nil {
		return Card{}, err
	}
	list, b, err := s.getListForEdit(ctx, a, in.ListId)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, err = s.copyCardTx(ctx, tx, a, b, src, list, copyOpts{
			Title: deref(in.Title, src.Title), IncludeMembers: deref(in.IncludeMembers, false), AsTemplate: in.AsTemplate,
			Placement: Placement{AfterId: in.AfterId, BeforeId: in.BeforeId},
		})
		return err
	})
	return out, err
}
