package service

import (
	"context"
	"database/sql"
	"strings"

	. "kowhai/internal/apitypes"
	"kowhai/internal/store"
)

func listSiblings(ctx context.Context, q *store.Queries, boardID string) ([]sibling, error) {
	rows, err := q.ListBoardLists(ctx, store.ListBoardListsParams{BoardID: boardID, IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make([]sibling, len(rows))
	for i, r := range rows {
		out[i] = sibling{r.ID, r.Position}
	}
	return out, nil
}

func (s *Service) placeList(ctx context.Context, tx *store.Tx, boardID, self string, p Placement) (string, error) {
	sibs, err := listSiblings(ctx, tx.Queries, boardID)
	if err != nil {
		return "", err
	}
	key, respaced, err := placeAmong(sibs, self, deref(p.AfterId, ""), deref(p.BeforeId, ""))
	if err != nil {
		return "", err
	}
	for _, r := range respaced {
		if err := tx.SetListPosition(ctx, store.SetListPositionParams{ID: r.ID, Position: r.Pos}); err != nil {
			return "", err
		}
	}
	if len(respaced) > 0 {
		tx.AfterCommit(func() { s.PublishResync(boardID) })
	}
	return key, nil
}

func (s *Service) CreateList(ctx context.Context, boardID string, in CreateList) (List, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return List{}, err
	}
	b, err := s.loadBoard(ctx, a, boardID, ActBoardEdit)
	if err != nil {
		return List{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return List{}, badRequest("name is required")
	}
	var out store.List
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, err = s.createListTx(ctx, tx, a, b, name, Placement{AfterId: in.AfterId, BeforeId: in.BeforeId})
		return err
	})
	return listOut(out), err
}

func (s *Service) createListTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, name string, p Placement) (store.List, error) {
	pos, err := s.placeList(ctx, tx, b.ID, "", p)
	if err != nil {
		return store.List{}, err
	}
	now := s.now()
	l, err := tx.CreateList(ctx, store.CreateListParams{ID: s.NewID(), BoardID: b.ID, Name: name, Position: pos, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return l, err
	}
	_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Type: TListCreated,
		Payload: map[string]any{"name": l.Name, "entity": listOut(l)}})
	return l, err
}

func (s *Service) getListForEdit(ctx context.Context, a *Actor, id string) (store.List, store.Board, error) {
	l, err := s.DB.R.GetList(ctx, id)
	if err != nil {
		return l, store.Board{}, notFound(err)
	}
	b, err := s.loadBoard(ctx, a, l.BoardID, ActBoardEdit)
	return l, b, err
}

func (s *Service) UpdateList(ctx context.Context, id, ifMatch string, in UpdateList) (List, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return List{}, err
	}
	_, b, err := s.getListForEdit(ctx, a, id)
	if err != nil {
		return List{}, err
	}
	var out store.List
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		l, err := tx.GetList(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, l.Version); err != nil {
			return err
		}
		p := store.UpdateListParams{ID: id, Name: l.Name, ArchivedAt: l.ArchivedAt, UpdatedAt: s.now()}
		changes := map[string]any{}
		if in.Name != nil {
			if p.Name = strings.TrimSpace(*in.Name); p.Name == "" {
				return badRequest("name is required")
			}
			changes["name"] = p.Name
			changes["previous_name"] = l.Name
		}
		if in.Archived != nil {
			if *in.Archived && !l.ArchivedAt.Valid {
				p.ArchivedAt = nt(s.now())
			} else if !*in.Archived {
				p.ArchivedAt = sql.NullTime{}
			}
			changes["archived"] = *in.Archived
		}
		if out, err = tx.UpdateList(ctx, p); err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Type: TListUpdated,
			Payload: map[string]any{"name": out.Name, "changes": changes, "entity": listOut(out)}})
		return err
	})
	return listOut(out), err
}

func (s *Service) MoveList(ctx context.Context, id string, in Placement) (List, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return List{}, err
	}
	_, b, err := s.getListForEdit(ctx, a, id)
	if err != nil {
		return List{}, err
	}
	var out store.List
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		pos, err := s.placeList(ctx, tx, b.ID, id, in)
		if err != nil {
			return err
		}
		if out, err = tx.MoveList(ctx, store.MoveListParams{ID: id, Position: pos, UpdatedAt: s.now()}); err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Type: TListMoved,
			Payload: map[string]any{"name": out.Name, "entity": listOut(out)}})
		return err
	})
	return listOut(out), err
}

// BoardOfList returns a list's board ID after checking the actor can see it.
func (s *Service) BoardOfList(ctx context.Context, listID string) (string, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return "", err
	}
	l, err := s.DB.R.GetList(ctx, listID)
	if err != nil {
		return "", notFound(err)
	}
	if _, err := s.loadBoard(ctx, a, l.BoardID, ActBoardView); err != nil {
		return "", err
	}
	return l.BoardID, nil
}

// FirstCardInList returns the ID of the top card in a list, or "".
func (s *Service) FirstCardInList(ctx context.Context, listID string) (string, error) {
	if _, err := s.BoardOfList(ctx, listID); err != nil {
		return "", err
	}
	cards, err := s.DB.R.ListListCards(ctx, listID)
	if err != nil || len(cards) == 0 {
		return "", err
	}
	return cards[0].ID, nil
}
