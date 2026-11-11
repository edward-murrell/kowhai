package service

import (
	"context"
	"time"

	"kowhai/internal/position"
	"kowhai/internal/store"
)

// Jobs the worker runs on a schedule.

// SendDueReminders notifies card members and watchers once when a card
// is due within 24 hours and once when it becomes overdue.
func (s *Service) SendDueReminders(ctx context.Context) (int, error) {
	now := s.now()
	soon, err := s.DB.R.ListCardsDueSoon(ctx, store.ListCardsDueSoonParams{Now: nt(now), Soon: nt(now.Add(24 * time.Hour))})
	if err != nil {
		return 0, err
	}
	overdue, err := s.DB.R.ListCardsOverdue(ctx, nt(now))
	if err != nil {
		return 0, err
	}
	n := 0
	send := func(c store.Card, typ string) error {
		return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
			b, err := tx.GetBoard(ctx, c.BoardID)
			if err != nil {
				return err
			}
			if typ == TCardDueSoon {
				err = tx.MarkCardDueSoonNotified(ctx, store.MarkCardDueSoonNotifiedParams{ID: c.ID, DueSoonNotifiedAt: nt(now)})
			} else {
				err = tx.MarkCardOverdueNotified(ctx, store.MarkCardOverdueNotifiedParams{ID: c.ID, OverdueNotifiedAt: nt(now)})
			}
			if err != nil {
				return err
			}
			audience, err := cardAudience(ctx, tx.Queries, c.ID)
			if err != nil {
				return err
			}
			_, err = s.record(ctx, tx, nil, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: c.ID, Type: typ,
				Payload: map[string]any{"card_title": c.Title, "due_at": timep(c.DueAt)},
				Notify:  []notifyGroup{{Pref: PrefDue, Users: audience}}})
			n++
			return err
		})
	}
	for _, c := range soon {
		if err := send(c, TCardDueSoon); err != nil {
			return n, err
		}
	}
	for _, c := range overdue {
		// A card that was never "due soon" (made overdue directly) only
		// gets the overdue notice.
		if err := send(c, TCardOverdue); err != nil {
			return n, err
		}
	}
	return n, nil
}

// CleanupSessions removes expired sessions and login states.
func (s *Service) CleanupSessions(ctx context.Context) error {
	w := store.New(s.DB.Write)
	if _, err := w.DeleteExpiredSessions(ctx, s.now()); err != nil {
		return err
	}
	_, err := w.DeleteExpiredOIDCStates(ctx, s.now())
	return err
}

// CleanupBlobs deletes queued objects from S3.
func (s *Service) CleanupBlobs(ctx context.Context) (int, error) {
	if s.Blob == nil {
		return 0, nil
	}
	rows, err := s.DB.R.ListBlobDeletions(ctx, 200)
	if err != nil {
		return 0, err
	}
	w := store.New(s.DB.Write)
	n := 0
	for _, r := range rows {
		if err := s.Blob.Delete(ctx, r.ObjectKey); err != nil {
			s.Log.Warn("delete object", "key", r.ObjectKey, "err", err)
			_ = w.BumpBlobDeletion(ctx, r.ObjectKey)
			continue
		}
		if err := w.DeleteBlobDeletion(ctx, r.ObjectKey); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Rebalance rewrites position keys that have grown long, then tells open
// boards to re-fetch.
func (s *Service) Rebalance(ctx context.Context) error {
	boards := map[string]bool{}
	ids, err := s.DB.R.ListsWithLongPositions(ctx, position.MaxLen)
	if err != nil {
		return err
	}
	for _, boardID := range ids {
		err := s.DB.WriteTx(ctx, func(tx *store.Tx) error {
			lists, err := tx.ListBoardLists(ctx, store.ListBoardListsParams{BoardID: boardID, IncludeArchived: true})
			if err != nil {
				return err
			}
			keys := position.Spread(len(lists))
			for i, l := range lists {
				if err := tx.SetListPosition(ctx, store.SetListPositionParams{ID: l.ID, Position: keys[i]}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		boards[boardID] = true
	}
	listIDs, err := s.DB.R.ListsWithLongCardPositions(ctx, position.MaxLen)
	if err != nil {
		return err
	}
	for _, listID := range listIDs {
		err := s.DB.WriteTx(ctx, func(tx *store.Tx) error {
			l, err := tx.GetList(ctx, listID)
			if err != nil {
				return err
			}
			cards, err := tx.ListAllListCards(ctx, listID)
			if err != nil {
				return err
			}
			keys := position.Spread(len(cards))
			for i, c := range cards {
				if err := tx.SetCardPosition(ctx, store.SetCardPositionParams{ID: c.ID, Position: keys[i]}); err != nil {
					return err
				}
			}
			boards[l.BoardID] = true
			return nil
		})
		if err != nil {
			return err
		}
	}
	for id := range boards {
		s.PublishResync(id)
	}
	return nil
}
