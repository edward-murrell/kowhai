package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/position"
	"github.com/edward-murrell/kowhai/internal/store"
)

var defaultLabelColours = []string{"green", "yellow", "orange", "red", "purple", "blue"}

func validVisibility(v string) bool { return v == "private" || v == "workspace" }

func (s *Service) ListWorkspaceBoards(ctx context.Context, wsID string, archived bool) ([]Board, error) {
	return s.listBoards(ctx, wsID, false, archived)
}

func (s *Service) ListBoardTemplates(ctx context.Context, wsID string) ([]Board, error) {
	return s.listBoards(ctx, wsID, true, false)
}

func (s *Service) listBoards(ctx context.Context, wsID string, templates, archived bool) ([]Board, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: wsID, IsTemplate: templates, IncludeArchived: archived})
	if err != nil {
		return nil, err
	}
	out := []Board{}
	for _, b := range rows {
		role, err := s.boardRole(ctx, a, b)
		if err != nil {
			return nil, err
		}
		if role != "" {
			out = append(out, boardOut(b, role))
		}
	}
	return out, nil
}

// VisibleBoards returns every board the actor can see, for search and MCP.
func (s *Service) VisibleBoards(ctx context.Context, a *Actor) ([]store.Board, error) {
	var out []store.Board
	cursor := ""
	for {
		ws, err := s.DB.R.ListUserWorkspaces(ctx, store.ListUserWorkspacesParams{UserID: a.User.ID, After: cursor, Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, w := range ws {
			boards, err := s.DB.R.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: w.ID, IncludeArchived: true})
			if err != nil {
				return nil, err
			}
			for _, b := range boards {
				if role, err := s.boardRole(ctx, a, b); err != nil {
					return nil, err
				} else if role != "" {
					out = append(out, b)
				}
			}
		}
		if len(ws) < 200 {
			return out, nil
		}
		cursor = ws[len(ws)-1].ID
	}
}

func (s *Service) CreateBoard(ctx context.Context, wsID string, in CreateBoard) (Board, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Board{}, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActBoardCreate); err != nil {
		return Board{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Board{}, badRequest("name is required")
	}
	vis := "workspace"
	if in.Visibility != nil {
		vis = string(*in.Visibility)
	}
	if !validVisibility(vis) {
		return Board{}, badRequest("visibility must be private or workspace")
	}
	var out store.Board
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		b, err := s.createBoardTx(ctx, tx, a, store.CreateBoardParams{
			WorkspaceID: wsID, Name: name, Description: deref(in.Description, ""), Visibility: vis,
			Background: deref(in.Background, ""), IsTemplate: deref(in.IsTemplate, false), Settings: "{}",
		})
		if err != nil {
			return err
		}
		out = b
		now := s.now()
		for _, c := range defaultLabelColours {
			if _, err := tx.CreateLabel(ctx, store.CreateLabelParams{ID: s.NewID(), BoardID: b.ID, Colour: c, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Board{}, err
	}
	return boardOut(out, "admin"), nil
}

// createBoardTx inserts a board, makes the actor its admin and records it.
func (s *Service) createBoardTx(ctx context.Context, tx *store.Tx, a *Actor, p store.CreateBoardParams) (store.Board, error) {
	now := s.now()
	p.ID = s.NewID()
	p.CreatedBy = a.User.ID
	p.CreatedAt, p.UpdatedAt = now, now
	b, err := tx.CreateBoard(ctx, p)
	if err != nil {
		return b, err
	}
	if err := tx.UpsertBoardMember(ctx, store.UpsertBoardMemberParams{BoardID: b.ID, UserID: a.User.ID, Role: "admin", CreatedAt: now}); err != nil {
		return b, err
	}
	_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Type: TBoardCreated,
		Payload: map[string]any{"name": b.Name, "entity": boardOut(b, "")}})
	return b, err
}

func (s *Service) GetBoard(ctx context.Context, id string) (Board, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Board{}, err
	}
	b, err := s.loadBoard(ctx, a, id, ActBoardView)
	if err != nil {
		return Board{}, err
	}
	role, err := s.boardRole(ctx, a, b)
	return boardOut(b, role), err
}

func (s *Service) validateSettings(ctx context.Context, q *store.Queries, boardID string, st BoardSettings) error {
	if st.StatusLists == nil {
		return nil
	}
	for status, listID := range *st.StatusLists {
		switch status {
		case "working", "waiting", "idle", "ended", "stale":
		default:
			return badRequest("unknown status %q in status_lists", status)
		}
		if listID == "" {
			continue
		}
		l, err := q.GetList(ctx, listID)
		if err != nil || l.BoardID != boardID {
			return badRequest("status_lists.%s is not a list on this board", status)
		}
	}
	return nil
}

func (s *Service) UpdateBoard(ctx context.Context, id, ifMatch string, in UpdateBoard) (Board, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Board{}, err
	}
	if _, err := s.loadBoard(ctx, a, id, ActBoardAdmin); err != nil {
		return Board{}, err
	}
	var out store.Board
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		b, err := tx.GetBoard(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, b.Version); err != nil {
			return err
		}
		p := store.UpdateBoardParams{
			ID: id, Name: b.Name, Description: b.Description, Visibility: b.Visibility, Background: b.Background,
			IsTemplate: b.IsTemplate, Settings: b.Settings, ArchivedAt: b.ArchivedAt, UpdatedAt: s.now(),
		}
		changed := map[string]any{}
		if in.Name != nil {
			if p.Name = strings.TrimSpace(*in.Name); p.Name == "" {
				return badRequest("name is required")
			}
			changed["name"] = p.Name
		}
		if in.Description != nil {
			p.Description = *in.Description
			changed["description"] = true
		}
		if in.Visibility != nil {
			if p.Visibility = string(*in.Visibility); !validVisibility(p.Visibility) {
				return badRequest("visibility must be private or workspace")
			}
			changed["visibility"] = p.Visibility
		}
		if in.Background != nil {
			p.Background = *in.Background
			changed["background"] = p.Background
		}
		if in.IsTemplate != nil {
			p.IsTemplate = *in.IsTemplate
			changed["is_template"] = p.IsTemplate
		}
		if in.Archived != nil {
			if *in.Archived && !b.ArchivedAt.Valid {
				p.ArchivedAt = nt(s.now())
			} else if !*in.Archived {
				p.ArchivedAt = sql.NullTime{}
			}
			changed["archived"] = *in.Archived
		}
		if in.Settings != nil {
			if err := s.validateSettings(ctx, tx.Queries, id, *in.Settings); err != nil {
				return err
			}
			p.Settings = mustJSON(in.Settings)
			changed["settings"] = true
		}
		if out, err = tx.UpdateBoard(ctx, p); err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: id, Type: TBoardUpdated,
			Payload: map[string]any{"name": out.Name, "changes": changed, "entity": boardOut(out, "")}})
		return err
	})
	if err != nil {
		return Board{}, err
	}
	role, err := s.boardRole(ctx, a, out)
	return boardOut(out, role), err
}

func (s *Service) DeleteBoard(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	b, err := s.loadBoard(ctx, a, id, ActBoardAdmin)
	if err != nil {
		return err
	}
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		keys, err := tx.ListAllAttachmentKeysForBoard(ctx, id)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := tx.QueueBlobDeletion(ctx, store.QueueBlobDeletionParams{ObjectKey: k, CreatedAt: s.now()}); err != nil {
				return err
			}
		}
		if err := s.unindexBoard(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.DeleteBoard(ctx, id); err != nil {
			return err
		}
		// The board's own activity rows are gone, so log it at workspace level.
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, Type: TBoardDeleted, Payload: map[string]any{"name": b.Name, "board_id": id}})
		return err
	})
	if err == nil {
		s.PublishResync(id)
	}
	return err
}

// BoardFull returns everything the board view needs in one call.
func (s *Service) BoardFull(ctx context.Context, id string, archived bool) (BoardFull, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return BoardFull{}, err
	}
	b, err := s.loadBoard(ctx, a, id, ActBoardView)
	if err != nil {
		return BoardFull{}, err
	}
	// Read the last event ID first so no event can slip between the
	// snapshot and the stream.
	last := s.lastBoardActivityID(ctx, id)
	role, err := s.boardRole(ctx, a, b)
	if err != nil {
		return BoardFull{}, err
	}
	out := BoardFull{Board: boardOut(b, role), Lists: []List{}, Cards: []Card{}, Labels: []Label{}, Users: []UserRef{}, Links: []CardLink{}}
	if last != "" {
		out.LastEventId = &last
	}
	q := s.DB.R
	lists, err := q.ListBoardLists(ctx, store.ListBoardListsParams{BoardID: id, IncludeArchived: archived})
	if err != nil {
		return out, err
	}
	for _, l := range lists {
		out.Lists = append(out.Lists, listOut(l))
	}
	if out.Cards, err = s.boardCards(ctx, id, archived); err != nil {
		return out, err
	}
	labels, err := q.ListBoardLabels(ctx, id)
	if err != nil {
		return out, err
	}
	for _, l := range labels {
		out.Labels = append(out.Labels, labelOut(l))
	}
	users, err := s.boardUsers(ctx, b)
	if err != nil {
		return out, err
	}
	for _, u := range users {
		out.Users = append(out.Users, *userRef(u))
	}
	links, err := q.ListBoardLinks(ctx, store.ListBoardLinksParams{BoardID: id, UpdatedAt: s.now().Add(-24 * time.Hour)})
	if err != nil {
		return out, err
	}
	for _, l := range links {
		out.Links = append(out.Links, linkOut(l))
	}
	return out, nil
}

// boardCards loads all cards on a board with labels, members and counts
// using one query per kind.
func (s *Service) boardCards(ctx context.Context, boardID string, archived bool) ([]Card, error) {
	q := s.DB.R
	cards, err := q.ListBoardCards(ctx, store.ListBoardCardsParams{BoardID: boardID, IncludeArchived: archived})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Card, len(cards))
	out := make([]Card, len(cards))
	for i, c := range cards {
		out[i] = cardBase(c)
		byID[c.ID] = &out[i]
	}
	cl, err := q.ListBoardCardLabels(ctx, boardID)
	if err != nil {
		return nil, err
	}
	for _, r := range cl {
		if c := byID[r.CardID]; c != nil {
			c.LabelIds = append(c.LabelIds, r.LabelID)
		}
	}
	cm, err := q.ListBoardCardMembers(ctx, boardID)
	if err != nil {
		return nil, err
	}
	for _, r := range cm {
		if c := byID[r.CardID]; c != nil {
			c.MemberIds = append(c.MemberIds, r.UserID)
		}
	}
	items, err := q.CountBoardCardChecklistItems(ctx, boardID)
	if err != nil {
		return nil, err
	}
	for _, r := range items {
		if c := byID[r.CardID]; c != nil {
			c.Counts.ChecklistItems, c.Counts.ChecklistDone = int(r.Total), int(r.Done)
		}
	}
	comments, err := q.CountBoardCardComments(ctx, boardID)
	if err != nil {
		return nil, err
	}
	for _, r := range comments {
		if c := byID[r.CardID]; c != nil {
			c.Counts.Comments = int(r.Total)
		}
	}
	atts, err := q.CountBoardCardAttachments(ctx, boardID)
	if err != nil {
		return nil, err
	}
	for _, r := range atts {
		if c := byID[r.CardID]; c != nil {
			c.Counts.Attachments = int(r.Total)
		}
	}
	return out, nil
}

// boardUsers returns everyone who can see the board.
func (s *Service) boardUsers(ctx context.Context, b store.Board) ([]store.User, error) {
	seen := map[string]bool{}
	var out []store.User
	wm, err := s.DB.R.ListWorkspaceMembers(ctx, b.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, m := range wm {
		if b.Visibility == "workspace" || m.Role == "owner" || m.Role == "admin" {
			seen[m.User.ID] = true
			out = append(out, m.User)
		}
	}
	bm, err := s.DB.R.ListBoardMembers(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	for _, m := range bm {
		if !seen[m.User.ID] {
			seen[m.User.ID] = true
			out = append(out, m.User)
		}
	}
	return slices.DeleteFunc(out, func(u store.User) bool { return u.DisabledAt.Valid }), nil
}

// canSeeBoard reports whether userID can see board b.
func (s *Service) canSeeBoard(ctx context.Context, userID string, b store.Board) bool {
	u, err := s.DB.R.GetUser(ctx, userID)
	if err != nil || u.DisabledAt.Valid {
		return false
	}
	role, err := s.boardRole(ctx, &Actor{User: u}, b)
	return err == nil && role != ""
}

// ---- Board members ----

func (s *Service) ListBoardMembers(ctx context.Context, boardID string) ([]BoardMember, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadBoard(ctx, a, boardID, ActBoardView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListBoardMembers(ctx, boardID)
	if err != nil {
		return nil, err
	}
	out := []BoardMember{}
	for _, r := range rows {
		out = append(out, BoardMember{User: userOut(r.User), Role: BoardRole(r.Role), JoinedAt: r.JoinedAt})
	}
	return out, nil
}

func validBoardRole(r string) bool { return r == "admin" || r == "editor" || r == "viewer" }

// SetBoardMember adds a member or changes their role. The user must be a
// member of the board's workspace.
func (s *Service) SetBoardMember(ctx context.Context, boardID, userID, role string, mustExist bool) (BoardMember, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return BoardMember{}, err
	}
	b, err := s.loadBoard(ctx, a, boardID, ActBoardAdmin)
	if err != nil {
		return BoardMember{}, err
	}
	if !validBoardRole(role) {
		return BoardMember{}, badRequest("unknown role %q", role)
	}
	var out BoardMember
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		current, err := tx.GetBoardRole(ctx, store.GetBoardRoleParams{BoardID: boardID, UserID: userID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if mustExist && current == "" {
			return ErrNotFound
		}
		if _, err := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: b.WorkspaceID, UserID: userID}); err != nil {
			return badRequest("add the user to the workspace first")
		}
		if current == "admin" && role != "admin" {
			if n, err := tx.CountBoardAdmins(ctx, boardID); err != nil {
				return err
			} else if n <= 1 {
				return conflict("a board needs at least one admin")
			}
		}
		u, err := tx.GetUser(ctx, userID)
		if err != nil {
			return notFound(err)
		}
		now := s.now()
		if err := tx.UpsertBoardMember(ctx, store.UpsertBoardMemberParams{BoardID: boardID, UserID: userID, Role: role, CreatedAt: now}); err != nil {
			return err
		}
		out = BoardMember{User: userOut(u), Role: BoardRole(role), JoinedAt: now}
		in := activityInput{WorkspaceID: b.WorkspaceID, BoardID: boardID, Type: TBoardMemberAdded,
			Payload: map[string]any{"user_id": userID, "user_name": u.DisplayName, "role": role, "board_name": b.Name}}
		if current == "" {
			in.Notify = []notifyGroup{{Pref: PrefInvite, Users: []string{userID}}}
		}
		_, err = s.record(ctx, tx, a, in)
		return err
	})
	return out, err
}

func (s *Service) RemoveBoardMember(ctx context.Context, boardID, userID string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	action := ActBoardAdmin
	if userID == a.User.ID {
		action = ActBoardView
	}
	b, err := s.loadBoard(ctx, a, boardID, action)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		current, err := tx.GetBoardRole(ctx, store.GetBoardRoleParams{BoardID: boardID, UserID: userID})
		if err != nil {
			return notFound(err)
		}
		if current == "admin" {
			if n, err := tx.CountBoardAdmins(ctx, boardID); err != nil {
				return err
			} else if n <= 1 {
				return conflict("a board needs at least one admin")
			}
		}
		if err := tx.DeleteBoardMember(ctx, store.DeleteBoardMemberParams{BoardID: boardID, UserID: userID}); err != nil {
			return err
		}
		u, _ := tx.GetUser(ctx, userID)
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: boardID, Type: TBoardMemberRemoved,
			Payload: map[string]any{"user_id": userID, "user_name": u.DisplayName}})
		return err
	})
}

// ---- Labels ----

func (s *Service) ListLabels(ctx context.Context, boardID string) ([]Label, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadBoard(ctx, a, boardID, ActBoardView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListBoardLabels(ctx, boardID)
	if err != nil {
		return nil, err
	}
	out := []Label{}
	for _, l := range rows {
		out = append(out, labelOut(l))
	}
	return out, nil
}

func (s *Service) CreateLabel(ctx context.Context, boardID string, in CreateLabel) (Label, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Label{}, err
	}
	b, err := s.loadBoard(ctx, a, boardID, ActBoardEdit)
	if err != nil {
		return Label{}, err
	}
	if strings.TrimSpace(in.Colour) == "" {
		return Label{}, badRequest("colour is required")
	}
	var out store.Label
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		now := s.now()
		out, err = tx.CreateLabel(ctx, store.CreateLabelParams{ID: s.NewID(), BoardID: boardID, Name: strings.TrimSpace(deref(in.Name, "")), Colour: in.Colour, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: boardID, Type: TLabelCreated,
			Payload: map[string]any{"name": out.Name, "entity": labelOut(out)}})
		return err
	})
	return labelOut(out), err
}

func (s *Service) UpdateLabel(ctx context.Context, id, ifMatch string, in UpdateLabel) (Label, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Label{}, err
	}
	l, err := s.DB.R.GetLabel(ctx, id)
	if err != nil {
		return Label{}, notFound(err)
	}
	b, err := s.loadBoard(ctx, a, l.BoardID, ActBoardEdit)
	if err != nil {
		return Label{}, err
	}
	var out store.Label
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		l, err := tx.GetLabel(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, l.Version); err != nil {
			return err
		}
		out, err = tx.UpdateLabel(ctx, store.UpdateLabelParams{ID: id, Name: strings.TrimSpace(deref(in.Name, l.Name)), Colour: deref(in.Colour, l.Colour), UpdatedAt: s.now()})
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Type: TLabelUpdated,
			Payload: map[string]any{"name": out.Name, "entity": labelOut(out)}})
		return err
	})
	return labelOut(out), err
}

func (s *Service) DeleteLabel(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	l, err := s.DB.R.GetLabel(ctx, id)
	if err != nil {
		return notFound(err)
	}
	b, err := s.loadBoard(ctx, a, l.BoardID, ActBoardEdit)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if err := tx.DeleteLabel(ctx, id); err != nil {
			return err
		}
		_, err := s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Type: TLabelDeleted,
			Payload: map[string]any{"name": l.Name, "label_id": id}})
		return err
	})
}

// ---- Placement ----

type sibling struct{ ID, Pos string }

// placeAmong works out a position key for an item being placed among
// siblings. self is the item's own ID when it is moving, so it is
// skipped. If keys collide the siblings are respaced first; respaced
// lists the new keys to write.
func placeAmong(sibs []sibling, self, afterID, beforeID string) (key string, respaced []sibling, err error) {
	sibs = slices.DeleteFunc(slices.Clone(sibs), func(x sibling) bool { return x.ID == self })
	idx := func(id string) int { return slices.IndexFunc(sibs, func(x sibling) bool { return x.ID == id }) }
	slot := len(sibs) // insert before sibs[slot]
	switch {
	case afterID != "":
		i := idx(afterID)
		if i < 0 {
			return "", nil, badRequest("after_id is not in the same container")
		}
		slot = i + 1
	case beforeID != "":
		i := idx(beforeID)
		if i < 0 {
			return "", nil, badRequest("before_id is not in the same container")
		}
		slot = i
	}
	a, b := "", ""
	if slot > 0 {
		a = sibs[slot-1].Pos
	}
	if slot < len(sibs) {
		b = sibs[slot].Pos
	}
	if key, err := position.Between(a, b); err == nil {
		return key, nil, nil
	}
	// Two siblings share a key. Respace everything, leaving a gap.
	keys := position.Spread(len(sibs) + 1)
	for i := range sibs {
		k := keys[i]
		if i >= slot {
			k = keys[i+1]
		}
		respaced = append(respaced, sibling{ID: sibs[i].ID, Pos: k})
	}
	return keys[slot], respaced, nil
}
