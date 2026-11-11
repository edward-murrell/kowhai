package service

import (
	"context"
	"encoding/json"
	"slices"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/events"
	"github.com/edward-murrell/kowhai/internal/store"
)

// Activity types. The web app renders each one.
const (
	TWorkspaceCreated      = "workspace.created"
	TWorkspaceUpdated      = "workspace.updated"
	TWorkspaceMemberAdded  = "workspace.member_added"
	TWorkspaceMemberRemove = "workspace.member_removed"
	TWorkspaceInvited      = "workspace.invited"
	TBoardCreated          = "board.created"
	TBoardUpdated          = "board.updated"
	TBoardDeleted          = "board.deleted"
	TBoardMemberAdded      = "board.member_added"
	TBoardMemberRemoved    = "board.member_removed"
	TLabelCreated          = "label.created"
	TLabelUpdated          = "label.updated"
	TLabelDeleted          = "label.deleted"
	TListCreated           = "list.created"
	TListUpdated           = "list.updated"
	TListMoved             = "list.moved"
	TCardCreated           = "card.created"
	TCardUpdated           = "card.updated"
	TCardMoved             = "card.moved"
	TCardDeleted           = "card.deleted"
	TCardMemberAdded       = "card.member_added"
	TCardMemberRemoved     = "card.member_removed"
	TCardLabelAdded        = "card.label_added"
	TCardLabelRemoved      = "card.label_removed"
	TCardDueSoon           = "card.due_soon"
	TCardOverdue           = "card.overdue"
	TChecklistCreated      = "checklist.created"
	TChecklistUpdated      = "checklist.updated"
	TChecklistDeleted      = "checklist.deleted"
	TItemCreated           = "checklist_item.created"
	TItemUpdated           = "checklist_item.updated"
	TItemDeleted           = "checklist_item.deleted"
	TCommentCreated        = "comment.created"
	TCommentUpdated        = "comment.updated"
	TCommentDeleted        = "comment.deleted"
	TAttachmentAdded       = "attachment.added"
	TAttachmentDeleted     = "attachment.deleted"
	TLinkCreated           = "link.created"
	TLinkUpdated           = "link.updated"
	TLinkStatus            = "link.status"
	TLinkDeleted           = "link.deleted"
	TRecurrenceCreated     = "recurrence.created"
	TRecurrenceUpdated     = "recurrence.updated"
	TRecurrenceDeleted     = "recurrence.deleted"
	TRecurrencePaused      = "recurrence.paused"
)

// Notification preference keys.
const (
	PrefCardMember       = "card_member"
	PrefMention          = "mention"
	PrefComment          = "comment"
	PrefDue              = "due"
	PrefInvite           = "invite"
	PrefRecurrencePaused = "recurrence_paused"
	PrefSessionWaiting   = "session_waiting"
)

type notifyGroup struct {
	Pref  string
	Users []string
	// IncludeActor notifies the actor too. A Claude session acting with
	// your token still needs to tell you it is waiting.
	IncludeActor bool
}

type activityInput struct {
	WorkspaceID string
	BoardID     string
	CardID      string
	Type        string
	Payload     map[string]any
	// Notify lists users to notify, grouped by preference key. The actor
	// is never notified about their own change.
	Notify []notifyGroup
}

// record writes an activity row and its notifications inside tx. After
// commit it publishes the activity to the board stream and the
// notifications to each user's stream.
func (s *Service) record(ctx context.Context, tx *store.Tx, a *Actor, in activityInput) (Activity, error) {
	if in.Payload == nil {
		in.Payload = map[string]any{}
	}
	p := store.CreateActivityParams{
		ID:          s.NewID(),
		WorkspaceID: in.WorkspaceID,
		BoardID:     ns(in.BoardID),
		CardID:      ns(in.CardID),
		Type:        in.Type,
		Payload:     mustJSON(in.Payload),
		CreatedAt:   s.now(),
	}
	var actorRef *UserRef
	if a != nil {
		p.ActorID = ns(a.User.ID)
		p.TokenID = a.tokenID()
		p.ActorLabel = a.Label()
		actorRef = userRef(a.User)
	}
	row, err := tx.CreateActivity(ctx, p)
	if err != nil {
		return Activity{}, err
	}
	act := activityOut(row, actorRef)

	notified := map[string]bool{}
	if a != nil {
		notified[a.User.ID] = true
	}
	type sent struct {
		user string
		n    Notification
	}
	var out []sent
	sentTo := map[string]bool{}
	for _, g := range in.Notify {
		for _, uid := range g.Users {
			if uid == "" || (notified[uid] && !(g.IncludeActor && a != nil && uid == a.User.ID && !sentTo[uid])) {
				continue
			}
			sentTo[uid] = true
			notified[uid] = true
			on, err := s.prefOn(ctx, tx, uid, g.Pref)
			if err != nil {
				return act, err
			}
			if !on {
				continue
			}
			n, err := tx.CreateNotification(ctx, store.CreateNotificationParams{
				ID: s.NewID(), UserID: uid, ActivityID: row.ID, CreatedAt: s.now(),
			})
			if err != nil {
				return act, err
			}
			out = append(out, sent{uid, Notification{Id: n.ID, CreatedAt: n.CreatedAt, Activity: act}})
		}
	}

	tx.AfterCommit(func() {
		if in.BoardID != "" {
			s.publishActivity(in.BoardID, act)
		}
		for _, n := range out {
			s.publishNotification(n.user, n.n)
		}
	})
	return act, nil
}

func (s *Service) publishActivity(boardID string, act Activity) {
	if s.Hub == nil {
		return
	}
	b, _ := json.Marshal(act)
	s.Hub.Publish(events.BoardTopic(boardID), events.Event{ID: act.Id, Name: "activity", Data: b})
}

func (s *Service) publishNotification(userID string, n Notification) {
	if s.Hub == nil {
		return
	}
	b, _ := json.Marshal(n)
	s.Hub.Publish(events.UserTopic(userID), events.Event{ID: n.Id, Name: "notification", Data: b})
	if c, err := s.DB.R.CountUnreadNotifications(context.Background(), userID); err == nil {
		s.publishUnread(userID, c)
	}
}

func (s *Service) publishUnread(userID string, count int64) {
	if s.Hub == nil {
		return
	}
	b, _ := json.Marshal(map[string]int64{"unread": count})
	s.Hub.Publish(events.UserTopic(userID), events.Event{Name: "unread", Data: b})
}

// PublishResync tells clients on a board to re-fetch it.
func (s *Service) PublishResync(boardID string) {
	if s.Hub == nil {
		return
	}
	s.Hub.Publish(events.BoardTopic(boardID), events.Event{Name: "resync", Data: []byte("{}")})
}

// ---- Listing ----

// cursor pagination for newest-first lists: the cursor is the last ID seen.
func (s *Service) activityPage(ctx context.Context, rows []store.Activity, limit int) (ActivityPage, error) {
	page := ActivityPage{Items: []Activity{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = ptr(rows[len(rows)-1].ID)
	}
	acts, err := s.activitiesOut(ctx, rows)
	if err != nil {
		return page, err
	}
	page.Items = acts
	return page, nil
}

func (s *Service) activitiesOut(ctx context.Context, rows []store.Activity) ([]Activity, error) {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.ActorID.Valid && !slices.Contains(ids, r.ActorID.String) {
			ids = append(ids, r.ActorID.String)
		}
	}
	users, err := s.usersByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Activity, len(rows))
	for i, r := range rows {
		var ref *UserRef
		if u, ok := users[r.ActorID.String]; ok {
			ref = userRef(u)
		}
		out[i] = activityOut(r, ref)
	}
	return out, nil
}

func (s *Service) ListBoardActivity(ctx context.Context, boardID string, cursor *string, limit *int) (ActivityPage, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return ActivityPage{}, err
	}
	if _, err := s.loadBoard(ctx, a, boardID, ActBoardView); err != nil {
		return ActivityPage{}, err
	}
	l := clampLimit(limit, 50, 200)
	rows, err := s.DB.R.ListBoardActivity(ctx, store.ListBoardActivityParams{BoardID: ns(boardID), Before: deref(cursor, ""), Limit: int64(l + 1)})
	if err != nil {
		return ActivityPage{}, err
	}
	return s.activityPage(ctx, rows, l)
}

func (s *Service) ListCardActivity(ctx context.Context, cardID string, cursor *string, limit *int) (ActivityPage, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return ActivityPage{}, err
	}
	if _, _, err := s.loadCard(ctx, a, cardID, ActBoardView); err != nil {
		return ActivityPage{}, err
	}
	l := clampLimit(limit, 50, 200)
	rows, err := s.DB.R.ListCardActivity(ctx, store.ListCardActivityParams{CardID: ns(cardID), Before: deref(cursor, ""), Limit: int64(l + 1)})
	if err != nil {
		return ActivityPage{}, err
	}
	return s.activityPage(ctx, rows, l)
}

// ReplayBoard returns activities after lastID for an SSE reconnect. ok is
// false when the gap is too large and the client should re-fetch.
func (s *Service) ReplayBoard(ctx context.Context, boardID, lastID string) (acts []Activity, ok bool, err error) {
	const maxReplay = 500
	rows, err := s.DB.R.ListBoardActivitySince(ctx, store.ListBoardActivitySinceParams{BoardID: ns(boardID), ID: lastID, Limit: maxReplay + 1})
	if err != nil {
		return nil, false, err
	}
	if len(rows) > maxReplay {
		return nil, false, nil
	}
	acts, err = s.activitiesOut(ctx, rows)
	return acts, err == nil, err
}

// LastBoardActivityID returns the newest activity ID on a board.
func (s *Service) lastBoardActivityID(ctx context.Context, boardID string) string {
	rows, err := s.DB.R.ListBoardActivity(ctx, store.ListBoardActivityParams{BoardID: ns(boardID), Limit: 1})
	if err != nil || len(rows) == 0 {
		return ""
	}
	return rows[0].ID
}
