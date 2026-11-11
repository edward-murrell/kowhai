package service

import (
	"context"
	"database/sql"
	"errors"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/notify"
	"github.com/edward-murrell/kowhai/internal/store"
)

func (s *Service) prefOn(ctx context.Context, tx *store.Tx, userID, key string) (bool, error) {
	if key == "" {
		return true, nil
	}
	p, err := tx.GetNotificationPrefs(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return notify.ParsePrefs(p.Settings).On(key), nil
}

// mentionedUsers returns users who can see b and are mentioned in text.
func (s *Service) mentionedUsers(ctx context.Context, b store.Board, text string) ([]string, error) {
	handles := notify.Mentions(text)
	if len(handles) == 0 {
		return nil, nil
	}
	users, err := s.boardUsers(ctx, b)
	if err != nil {
		return nil, err
	}
	people := make([]notify.Person, len(users))
	for i, u := range users {
		people[i] = notify.Person{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
	}
	return notify.Match(handles, people), nil
}

func (s *Service) ListNotifications(ctx context.Context, cursor *string, limit *int, unread bool) (NotificationPage, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return NotificationPage{}, err
	}
	l := clampLimit(limit, 30, 100)
	rows, err := s.DB.R.ListUserNotifications(ctx, store.ListUserNotificationsParams{
		UserID: a.User.ID, Before: deref(cursor, ""), UnreadOnly: unread, Limit: int64(l + 1),
	})
	if err != nil {
		return NotificationPage{}, err
	}
	page := NotificationPage{Items: []Notification{}}
	if len(rows) > l {
		rows = rows[:l]
		page.NextCursor = ptr(rows[l-1].Notification.ID)
	}
	acts := make([]store.Activity, len(rows))
	for i, r := range rows {
		acts[i] = r.Activity
	}
	out, err := s.activitiesOut(ctx, acts)
	if err != nil {
		return page, err
	}
	for i, r := range rows {
		page.Items = append(page.Items, Notification{Id: r.Notification.ID, Read: r.Notification.ReadAt.Valid, CreatedAt: r.Notification.CreatedAt, Activity: out[i]})
	}
	n, err := s.DB.R.CountUnreadNotifications(ctx, a.User.ID)
	page.Unread = int(n)
	return page, err
}

func (s *Service) SetNotificationRead(ctx context.Context, id string, read bool) (Notification, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Notification{}, err
	}
	at := sql.NullTime{}
	if read {
		at = nt(s.now())
	}
	n, err := store.New(s.DB.Write).SetNotificationRead(ctx, store.SetNotificationReadParams{ID: id, UserID: a.User.ID, ReadAt: at})
	if err != nil {
		return Notification{}, notFound(err)
	}
	act, err := s.DB.R.GetActivity(ctx, n.ActivityID)
	if err != nil {
		return Notification{}, err
	}
	acts, err := s.activitiesOut(ctx, []store.Activity{act})
	if err != nil {
		return Notification{}, err
	}
	if c, err := s.DB.R.CountUnreadNotifications(ctx, a.User.ID); err == nil {
		s.publishUnread(a.User.ID, c)
	}
	return Notification{Id: n.ID, Read: n.ReadAt.Valid, CreatedAt: n.CreatedAt, Activity: acts[0]}, nil
}

func (s *Service) ReadAllNotifications(ctx context.Context) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	if err := store.New(s.DB.Write).MarkAllNotificationsRead(ctx, store.MarkAllNotificationsReadParams{UserID: a.User.ID, ReadAt: nt(s.now())}); err != nil {
		return err
	}
	s.publishUnread(a.User.ID, 0)
	return nil
}

var prefKeys = []string{PrefCardMember, PrefMention, PrefComment, PrefDue, PrefInvite, PrefRecurrencePaused, PrefSessionWaiting}

func prefsOut(p notify.Prefs) NotificationPrefs {
	b := func(k string) *bool { return ptr(p.On(k)) }
	return NotificationPrefs{
		CardMember: b(PrefCardMember), Mention: b(PrefMention), Comment: b(PrefComment), Due: b(PrefDue),
		Invite: b(PrefInvite), RecurrencePaused: b(PrefRecurrencePaused), SessionWaiting: b(PrefSessionWaiting),
	}
}

func (s *Service) GetNotificationPrefs(ctx context.Context) (NotificationPrefs, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return NotificationPrefs{}, err
	}
	row, err := s.DB.R.GetNotificationPrefs(ctx, a.User.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return NotificationPrefs{}, err
	}
	return prefsOut(notify.ParsePrefs(row.Settings)), nil
}

func (s *Service) PutNotificationPrefs(ctx context.Context, in NotificationPrefs) (NotificationPrefs, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return NotificationPrefs{}, err
	}
	if !a.CanWrite() {
		return NotificationPrefs{}, forbidden("this token is read-only")
	}
	p := notify.Prefs{}
	set := func(k string, v *bool) {
		if v != nil {
			p[k] = *v
		}
	}
	set(PrefCardMember, in.CardMember)
	set(PrefMention, in.Mention)
	set(PrefComment, in.Comment)
	set(PrefDue, in.Due)
	set(PrefInvite, in.Invite)
	set(PrefRecurrencePaused, in.RecurrencePaused)
	set(PrefSessionWaiting, in.SessionWaiting)
	if err := store.New(s.DB.Write).UpsertNotificationPrefs(ctx, store.UpsertNotificationPrefsParams{UserID: a.User.ID, Settings: mustJSON(p), UpdatedAt: s.now()}); err != nil {
		return NotificationPrefs{}, err
	}
	return prefsOut(p), nil
}
