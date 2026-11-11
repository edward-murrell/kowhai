package service

import (
	"context"
	"database/sql"
	"time"

	. "kowhai/internal/apitypes"
	"kowhai/internal/recur"
	"kowhai/internal/store"
)

func parseRule(rrule, dtstart, tz string) (*recur.Rule, error) {
	r, err := recur.Parse(rrule, dtstart, tz)
	if err != nil {
		return nil, badRequest("%s", err.Error())
	}
	return r, nil
}

func (s *Service) nextRun(r *recur.Rule) sql.NullTime {
	return nt(r.After(s.now()))
}

func (s *Service) ListRecurrences(ctx context.Context, boardID string) ([]Recurrence, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadBoard(ctx, a, boardID, ActBoardView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListBoardRecurrences(ctx, boardID)
	if err != nil {
		return nil, err
	}
	out := []Recurrence{}
	for _, r := range rows {
		out = append(out, recurrenceOut(r))
	}
	return out, nil
}

func (s *Service) loadRecurrence(ctx context.Context, a *Actor, id string, action Action) (store.CardRecurrence, store.Board, error) {
	r, err := s.DB.R.GetRecurrence(ctx, id)
	if err != nil {
		return r, store.Board{}, notFound(err)
	}
	b, err := s.loadBoard(ctx, a, r.BoardID, action)
	return r, b, err
}

func (s *Service) GetRecurrence(ctx context.Context, id string) (Recurrence, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Recurrence{}, err
	}
	r, _, err := s.loadRecurrence(ctx, a, id, ActBoardView)
	return recurrenceOut(r), err
}

func checkRecurrenceTargets(ctx context.Context, q *store.Queries, boardID, templateID, listID string) error {
	c, err := q.GetCard(ctx, templateID)
	if err != nil || c.BoardID != boardID {
		return badRequest("template_card_id is not a card on this board")
	}
	l, err := q.GetList(ctx, listID)
	if err != nil || l.BoardID != boardID {
		return badRequest("target_list_id is not a list on this board")
	}
	return nil
}

func (s *Service) CreateRecurrence(ctx context.Context, boardID string, in CreateRecurrence) (Recurrence, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Recurrence{}, err
	}
	b, err := s.loadBoard(ctx, a, boardID, ActBoardEdit)
	if err != nil {
		return Recurrence{}, err
	}
	rule, err := parseRule(in.Rrule, in.Dtstart, in.Timezone)
	if err != nil {
		return Recurrence{}, err
	}
	var out store.CardRecurrence
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if err := checkRecurrenceTargets(ctx, tx.Queries, boardID, in.TemplateCardId, in.TargetListId); err != nil {
			return err
		}
		var off sql.NullInt64
		if in.DueOffsetMinutes != nil {
			off = sql.NullInt64{Int64: int64(*in.DueOffsetMinutes), Valid: true}
		}
		now := s.now()
		out, err = tx.CreateRecurrence(ctx, store.CreateRecurrenceParams{
			ID: s.NewID(), BoardID: boardID, TemplateCardID: ns(in.TemplateCardId), TargetListID: in.TargetListId,
			Rrule: in.Rrule, Dtstart: in.Dtstart, Timezone: in.Timezone, DueOffsetMinutes: off, NextRunAt: s.nextRun(rule),
			CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		c, _ := tx.GetCard(ctx, in.TemplateCardId)
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: boardID, CardID: in.TemplateCardId, Type: TRecurrenceCreated,
			Payload: map[string]any{"card_title": c.Title, "rrule": out.Rrule, "entity": recurrenceOut(out)}})
		return err
	})
	return recurrenceOut(out), err
}

func (s *Service) UpdateRecurrence(ctx context.Context, id, ifMatch string, in UpdateRecurrence) (Recurrence, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Recurrence{}, err
	}
	_, b, err := s.loadRecurrence(ctx, a, id, ActBoardEdit)
	if err != nil {
		return Recurrence{}, err
	}
	var out store.CardRecurrence
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		r, err := tx.GetRecurrence(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, r.Version); err != nil {
			return err
		}
		p := store.UpdateRecurrenceParams{
			ID: id, TemplateCardID: r.TemplateCardID, TargetListID: deref(in.TargetListId, r.TargetListID),
			Rrule: deref(in.Rrule, r.Rrule), Dtstart: deref(in.Dtstart, r.Dtstart), Timezone: deref(in.Timezone, r.Timezone),
			DueOffsetMinutes: r.DueOffsetMinutes, NextRunAt: r.NextRunAt, LastRunAt: r.LastRunAt, PausedAt: r.PausedAt, UpdatedAt: s.now(),
		}
		if in.TemplateCardId != nil {
			p.TemplateCardID = ns(*in.TemplateCardId)
		}
		if !p.TemplateCardID.Valid {
			return badRequest("the template card was deleted. Pick a new one")
		}
		if err := checkRecurrenceTargets(ctx, tx.Queries, r.BoardID, p.TemplateCardID.String, p.TargetListID); err != nil {
			return err
		}
		if in.DueOffsetMinutes.IsSpecified() {
			if in.DueOffsetMinutes.IsNull() {
				p.DueOffsetMinutes = sql.NullInt64{}
			} else {
				v, _ := in.DueOffsetMinutes.Get()
				p.DueOffsetMinutes = sql.NullInt64{Int64: int64(v), Valid: true}
			}
		}
		if in.Paused != nil {
			if *in.Paused && !r.PausedAt.Valid {
				p.PausedAt = nt(s.now())
			} else if !*in.Paused {
				p.PausedAt = sql.NullTime{}
			}
		}
		rule, err := parseRule(p.Rrule, p.Dtstart, p.Timezone)
		if err != nil {
			return err
		}
		// Recompute from now, so resuming does not create a backlog.
		p.NextRunAt = s.nextRun(rule)
		if out, err = tx.UpdateRecurrence(ctx, p); err != nil {
			return err
		}
		c, _ := tx.GetCard(ctx, p.TemplateCardID.String)
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: c.ID, Type: TRecurrenceUpdated,
			Payload: map[string]any{"card_title": c.Title, "rrule": out.Rrule, "paused": out.PausedAt.Valid, "entity": recurrenceOut(out)}})
		return err
	})
	return recurrenceOut(out), err
}

func (s *Service) DeleteRecurrence(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	r, b, err := s.loadRecurrence(ctx, a, id, ActBoardEdit)
	if err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if err := tx.DeleteRecurrence(ctx, id); err != nil {
			return err
		}
		_, err := s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: r.TemplateCardID.String, Type: TRecurrenceDeleted,
			Payload: map[string]any{"recurrence_id": id}})
		return err
	})
}

// pauseRecurrenceTx pauses a recurrence and tells its creator why.
func (s *Service) pauseRecurrenceTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, r store.CardRecurrence, reason string) error {
	nr, err := tx.UpdateRecurrence(ctx, store.UpdateRecurrenceParams{
		ID: r.ID, TemplateCardID: r.TemplateCardID, TargetListID: r.TargetListID, Rrule: r.Rrule, Dtstart: r.Dtstart,
		Timezone: r.Timezone, DueOffsetMinutes: r.DueOffsetMinutes, NextRunAt: r.NextRunAt, LastRunAt: r.LastRunAt,
		PausedAt: nt(s.now()), UpdatedAt: s.now(),
	})
	if err != nil {
		return err
	}
	title := ""
	if c, err := tx.GetCard(ctx, r.TemplateCardID.String); err == nil {
		title = c.Title
	}
	_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: b.WorkspaceID, BoardID: b.ID, CardID: r.TemplateCardID.String, Type: TRecurrencePaused,
		Payload: map[string]any{"card_title": title, "reason": reason, "recurrence_id": r.ID, "entity": recurrenceOut(nr)},
		Notify:  []notifyGroup{{Pref: PrefRecurrencePaused, Users: []string{r.CreatedBy}}}})
	return err
}

// runRecurrenceTx creates one card from the recurrence's template.
func (s *Service) runRecurrenceTx(ctx context.Context, tx *store.Tx, a *Actor, b store.Board, r store.CardRecurrence, advance bool) (Card, error) {
	if !r.TemplateCardID.Valid {
		return Card{}, badRequest("the template card was deleted")
	}
	tmpl, err := tx.GetCard(ctx, r.TemplateCardID.String)
	if err != nil {
		return Card{}, badRequest("the template card was deleted")
	}
	list, err := tx.GetList(ctx, r.TargetListID)
	if err != nil {
		return Card{}, badRequest("the target list was deleted")
	}
	now := s.now()
	var due *time.Time
	if r.DueOffsetMinutes.Valid {
		due = ptr(now.Add(time.Duration(r.DueOffsetMinutes.Int64) * time.Minute))
	}
	out, err := s.copyCardTx(ctx, tx, a, b, tmpl, list, copyOpts{
		Title: tmpl.Title, IncludeMembers: true, RecurrenceID: r.ID, DueAt: due, Placement: Placement{},
	})
	if err != nil {
		return out, err
	}
	next := r.NextRunAt
	if advance {
		rule, err := recur.Parse(r.Rrule, r.Dtstart, r.Timezone)
		if err != nil {
			return out, err
		}
		next = nt(rule.After(now))
	}
	_, err = tx.UpdateRecurrence(ctx, store.UpdateRecurrenceParams{
		ID: r.ID, TemplateCardID: r.TemplateCardID, TargetListID: r.TargetListID, Rrule: r.Rrule, Dtstart: r.Dtstart,
		Timezone: r.Timezone, DueOffsetMinutes: r.DueOffsetMinutes, NextRunAt: next, LastRunAt: nt(now),
		PausedAt: r.PausedAt, UpdatedAt: now,
	})
	return out, err
}

// RunRecurrence creates the next card now without moving the schedule.
func (s *Service) RunRecurrence(ctx context.Context, id string) (Card, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Card{}, err
	}
	_, b, err := s.loadRecurrence(ctx, a, id, ActBoardEdit)
	if err != nil {
		return Card{}, err
	}
	var out Card
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		r, err := tx.GetRecurrence(ctx, id)
		if err != nil {
			return notFound(err)
		}
		out, err = s.runRecurrenceTx(ctx, tx, a, b, r, false)
		return err
	})
	return out, err
}

func (s *Service) PreviewRecurrence(ctx context.Context, id string, count int) (RecurrencePreview, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return RecurrencePreview{}, err
	}
	r, _, err := s.loadRecurrence(ctx, a, id, ActBoardView)
	if err != nil {
		return RecurrencePreview{}, err
	}
	return s.PreviewRule(ctx, PreviewRule{Rrule: r.Rrule, Dtstart: r.Dtstart, Timezone: r.Timezone, Count: &count})
}

func (s *Service) PreviewRule(ctx context.Context, in PreviewRule) (RecurrencePreview, error) {
	if _, err := mustActor(ctx); err != nil {
		return RecurrencePreview{}, err
	}
	rule, err := parseRule(in.Rrule, in.Dtstart, in.Timezone)
	if err != nil {
		return RecurrencePreview{}, err
	}
	n := deref(in.Count, 5)
	n = max(1, min(n, 50))
	times := rule.Next(s.now(), n)
	if times == nil {
		times = []time.Time{}
	}
	return RecurrencePreview{Times: times}, nil
}

// RunDueRecurrences creates cards for every recurrence that is due. If
// the server was down it makes one card for the latest missed run, then
// schedules the next run after now.
func (s *Service) RunDueRecurrences(ctx context.Context) (int, error) {
	due, err := s.DB.R.ListDueRecurrences(ctx, store.ListDueRecurrencesParams{NextRunAt: nt(s.now()), Limit: 100})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range due {
		err := s.DB.WriteTx(ctx, func(tx *store.Tx) error {
			r, err := tx.GetRecurrence(ctx, r.ID)
			if err != nil || r.PausedAt.Valid || !r.NextRunAt.Valid || r.NextRunAt.Time.After(s.now()) {
				return nil
			}
			b, err := tx.GetBoard(ctx, r.BoardID)
			if err != nil {
				return err
			}
			creator, err := tx.GetUser(ctx, r.CreatedBy)
			if err != nil {
				return err
			}
			a := &Actor{User: creator}
			if _, err := s.runRecurrenceTx(ctx, tx, a, b, r, true); err != nil {
				if e, ok := err.(*Error); ok && e.Status == 400 {
					return s.pauseRecurrenceTx(ctx, tx, nil, b, r, e.Detail)
				}
				return err
			}
			n++
			return nil
		})
		if err != nil {
			s.Log.Error("run recurrence", "id", r.ID, "err", err)
		}
	}
	return n, nil
}
