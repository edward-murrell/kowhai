package server_test

import (
	"context"
	"testing"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/testenv"
)

func userID(t *testing.T, c *testenv.Client) string {
	var me Me
	c.Must(200, "GET", "/me", nil, &me)
	return me.User.Id
}

func TestRecurringCards(t *testing.T) {
	e, alice, _, b, lists := setup(t)
	ctx := context.Background()
	var tmpl Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Weekly report", IsTemplate: ptr(true)}, &tmpl)
	var cl Checklist
	alice.Must(201, "POST", "/cards/"+tmpl.Id+"/checklists", CreateChecklist{Title: "Steps"}, &cl)
	alice.Must(201, "POST", "/checklists/"+cl.Id+"/items", CreateChecklistItem{Text: "Gather numbers"}, nil)

	// Template cards are hidden from the board.
	var full BoardFull
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	if len(full.Cards) != 0 {
		t.Fatal("template card shown on board")
	}

	var prev RecurrencePreview
	alice.Must(200, "POST", "/recurrences/preview", PreviewRule{Rrule: "FREQ=DAILY", Dtstart: "2026-11-16T09:00:00", Timezone: "Australia/Sydney", Count: ptr(2)}, &prev)
	if prev.Times[0].Format(time.RFC3339) != "2026-11-16T22:00:00Z" {
		t.Fatalf("preview = %v", prev.Times)
	}
	if got := alice.Do("POST", "/recurrences/preview", PreviewRule{Rrule: "FREQ=DAILY", Dtstart: "x", Timezone: "Nowhere/Here"}, nil); got != 400 {
		t.Fatalf("bad rule = %d", got)
	}

	var rec Recurrence
	alice.Must(201, "POST", "/boards/"+b.Id+"/recurrences", CreateRecurrence{
		TemplateCardId: tmpl.Id, TargetListId: lists[1].Id, Rrule: "FREQ=DAILY", Dtstart: "2026-11-16T09:00:00",
		Timezone: "Australia/Sydney", DueOffsetMinutes: ptr(120),
	}, &rec)
	if rec.NextRunAt == nil || rec.NextRunAt.Format(time.RFC3339) != "2026-11-16T22:00:00Z" {
		t.Fatalf("next run = %v", rec.NextRunAt)
	}

	// Nothing is due yet.
	if n, _ := e.Svc.RunDueRecurrences(ctx); n != 0 {
		t.Fatalf("ran %d early", n)
	}
	e.Clock.Set(time.Date(2026, 11, 16, 22, 1, 0, 0, time.UTC))
	if n, err := e.Svc.RunDueRecurrences(ctx); n != 1 || err != nil {
		t.Fatalf("ran %d, %v", n, err)
	}
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	if len(full.Cards) != 1 || full.Cards[0].ListId != lists[1].Id || full.Cards[0].RecurrenceId == nil ||
		full.Cards[0].Counts.ChecklistItems != 1 || full.Cards[0].DueAt == nil {
		t.Fatalf("created card = %+v", full.Cards)
	}
	if want := e.Clock.Now().Add(2 * time.Hour); !full.Cards[0].DueAt.Equal(want) {
		t.Fatalf("due = %v want %v", full.Cards[0].DueAt, want)
	}

	// After three days down, only one card is made for the missed runs.
	e.Clock.Set(time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC))
	if n, _ := e.Svc.RunDueRecurrences(ctx); n != 1 {
		t.Fatalf("catch-up made %d cards", n)
	}
	alice.Must(200, "GET", "/recurrences/"+rec.Id, nil, &rec)
	if rec.NextRunAt.Format(time.RFC3339) != "2026-11-20T22:00:00Z" {
		t.Fatalf("next after catch-up = %v", rec.NextRunAt)
	}

	// Run now does not move the schedule.
	alice.Must(201, "POST", "/recurrences/"+rec.Id+"/run", nil, nil)
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	if len(full.Cards) != 3 {
		t.Fatalf("cards = %d", len(full.Cards))
	}

	// Deleting the template pauses the recurrence and tells its creator.
	bob := e.Login("bob@example.com")
	_ = bob
	alice.Must(204, "DELETE", "/cards/"+tmpl.Id, nil, nil)
	alice.Must(200, "GET", "/recurrences/"+rec.Id, nil, &rec)
	if !rec.Paused {
		t.Fatal("recurrence not paused")
	}
	// Alice deleted it herself, so she is not notified about her own change.
	var notes NotificationPage
	alice.Must(200, "GET", "/me/notifications", nil, &notes)
	for _, n := range notes.Items {
		if n.Activity.Type == "recurrence.paused" {
			t.Fatal("actor notified about own change")
		}
	}
}

func TestRecurrencePausedNotifiesCreator(t *testing.T) {
	e, alice, ws, b, lists := setup(t)
	bob := e.Login("bob@example.com")
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/members", AddWorkspaceMember{UserId: userID(t, bob), Role: "member"}, nil)
	var tmpl Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Standup", IsTemplate: ptr(true)}, &tmpl)
	bob.Must(201, "POST", "/boards/"+b.Id+"/recurrences", CreateRecurrence{TemplateCardId: tmpl.Id, TargetListId: lists[0].Id,
		Rrule: "FREQ=WEEKLY;BYDAY=MO", Dtstart: "2026-11-16T09:00:00", Timezone: "Pacific/Auckland"}, nil)
	alice.Must(204, "DELETE", "/cards/"+tmpl.Id, nil, nil)
	var notes NotificationPage
	bob.Must(200, "GET", "/me/notifications", nil, &notes)
	found := false
	for _, n := range notes.Items {
		found = found || n.Activity.Type == "recurrence.paused"
	}
	if !found {
		t.Fatal("creator not told about pause")
	}
}

func TestDueReminders(t *testing.T) {
	e, alice, ws, _, lists := setup(t)
	ctx := context.Background()
	bob := e.Login("bob@example.com")
	bobID := userID(t, bob)
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/members", AddWorkspaceMember{UserId: bobID, Role: "member"}, nil)
	var c Card
	due := e.Clock.Now().Add(12 * time.Hour)
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Pay invoice", DueAt: &due, MemberIds: &[]string{bobID}}, &c)
	if n, _ := e.Svc.SendDueReminders(ctx); n != 1 {
		t.Fatalf("due soon sent %d", n)
	}
	if n, _ := e.Svc.SendDueReminders(ctx); n != 0 {
		t.Fatalf("due soon sent twice: %d", n)
	}
	e.Clock.Add(13 * time.Hour)
	if n, _ := e.Svc.SendDueReminders(ctx); n != 1 {
		t.Fatalf("overdue sent %d", n)
	}
	var notes NotificationPage
	bob.Must(200, "GET", "/me/notifications", nil, &notes)
	types := map[string]bool{}
	for _, n := range notes.Items {
		types[n.Activity.Type] = true
	}
	if !types["card.due_soon"] || !types["card.overdue"] || !types["card.created"] {
		t.Fatalf("bob notifications = %v", types)
	}
	// Marking done stops reminders. Mark all read works.
	bob.Must(204, "POST", "/me/notifications/read-all", nil, nil)
	bob.Must(200, "GET", "/me/notifications", nil, &notes)
	if notes.Unread != 0 {
		t.Fatal("unread after read-all")
	}
}

func TestMentionsAndPrefs(t *testing.T) {
	e, alice, ws, _, lists := setup(t)
	bob := e.Login("bob@example.com")
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/members", AddWorkspaceMember{UserId: userID(t, bob), Role: "member"}, nil)
	bob.Must(204, "POST", "/me/notifications/read-all", nil, nil)
	var c Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Talk"}, &c)
	alice.Must(201, "POST", "/cards/"+c.Id+"/comments", CommentInput{Body: "@bob can you look?"}, nil)
	var notes NotificationPage
	bob.Must(200, "GET", "/me/notifications?unread=true", nil, &notes)
	if notes.Unread != 1 {
		t.Fatalf("mention unread = %d", notes.Unread)
	}
	bob.Must(200, "PUT", "/me/notification-prefs", NotificationPrefs{Mention: ptr(false)}, nil)
	alice.Must(201, "POST", "/cards/"+c.Id+"/comments", CommentInput{Body: "@bob ping"}, nil)
	bob.Must(200, "GET", "/me/notifications?unread=true", nil, &notes)
	if notes.Unread != 1 {
		t.Fatalf("mention with pref off: unread = %d", notes.Unread)
	}
	// Watchers hear about comments.
	bob.Must(204, "POST", "/cards/"+c.Id+"/watch", nil, nil)
	alice.Must(201, "POST", "/cards/"+c.Id+"/comments", CommentInput{Body: "update"}, nil)
	bob.Must(200, "GET", "/me/notifications?unread=true", nil, &notes)
	if notes.Unread != 2 {
		t.Fatalf("watcher unread = %d", notes.Unread)
	}
}

func TestTemplatesAndCopy(t *testing.T) {
	_, alice, ws, _, _ := setup(t)
	var builtins []BuiltinTemplate
	alice.Must(200, "GET", "/workspaces/"+ws.Id+"/templates/builtin", nil, &builtins)
	if len(builtins) != 4 {
		t.Fatalf("builtins = %d", len(builtins))
	}
	var tb Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/templates/builtin", SeedTemplate{Key: "sprint"}, &tb)
	var again Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/templates/builtin", SeedTemplate{Key: "sprint"}, &again)
	if again.Id != tb.Id || !tb.IsTemplate {
		t.Fatal("seeding twice made two boards")
	}
	var nb Board
	alice.Must(201, "POST", "/boards/"+tb.Id+"/copy", CopyBoard{Name: "Sprint 1"}, &nb)
	var full BoardFull
	alice.Must(200, "GET", "/boards/"+nb.Id+"/full", nil, &full)
	if len(full.Lists) != 5 || len(full.Labels) != 4 || nb.IsTemplate {
		t.Fatalf("copy: %d lists %d labels", len(full.Lists), len(full.Labels))
	}
	var tmpls []Card
	alice.Must(200, "GET", "/boards/"+nb.Id+"/card-templates", nil, &tmpls)
	if len(tmpls) != 1 || tmpls[0].Title != "Story" {
		t.Fatalf("card templates = %+v", tmpls)
	}
	var acts ActivityPage
	alice.Must(200, "GET", "/boards/"+nb.Id+"/activity", nil, &acts)
	if len(acts.Items) != 1 {
		t.Fatalf("copy wrote %d activity rows", len(acts.Items))
	}
	// Card from template keeps its checklist.
	var c Card
	alice.Must(201, "POST", "/cards/"+tmpls[0].Id+"/copy", CopyCard{ListId: full.Lists[0].Id, Title: ptr("Login page"), AsTemplate: ptr(false)}, &c)
	if c.IsTemplate || c.Title != "Login page" || c.Counts.ChecklistItems != 1 {
		t.Fatalf("from template = %+v", c)
	}
}

func TestClaudeStatusMapping(t *testing.T) {
	e, alice, ws, _, _ := setup(t)
	ctx := context.Background()
	var tb, b Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/templates/builtin", SeedTemplate{Key: "agent"}, &tb)
	alice.Must(201, "POST", "/boards/"+tb.Id+"/copy", CopyBoard{Name: "My agents"}, &b)
	if b.Settings.StatusListsEnabled == nil || !*b.Settings.StatusListsEnabled {
		t.Fatal("mapping not on")
	}
	var full BoardFull
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	listByName := map[string]string{}
	for _, l := range full.Lists {
		listByName[l.Name] = l.Id
	}
	var c Card
	alice.Must(201, "POST", "/lists/"+listByName["Backlog"]+"/cards", CreateCard{Title: "Refactor", MemberIds: &[]string{userID(t, alice)}}, &c)
	var tok CreatedToken
	alice.Must(201, "POST", "/me/tokens", CreateToken{Name: "hook", Scopes: []TokenScope{"write"}, BoardId: &b.Id, ActorLabel: ptr("Claude")}, &tok)
	claude := e.WithToken(tok.Token)

	put := func(session string, status LinkStatus, detail string) CardLink {
		var l CardLink
		claude.Must(200, "PUT", "/cards/"+c.Id+"/links/claude_code/"+session, PutCardLink{Status: &status, StatusDetail: &detail, Branch: ptr("main")}, &l)
		return l
	}
	cardList := func() string {
		var d CardDetail
		alice.Must(200, "GET", "/cards/"+c.Id, nil, &d)
		return d.ListId
	}
	put("s1", "working", "")
	if cardList() != listByName["Working"] {
		t.Fatal("working did not move card")
	}
	put("s1", "waiting", "Allow Bash?")
	if cardList() != listByName["Waiting on me"] {
		t.Fatal("waiting did not move card")
	}
	// A second session that is working does not beat waiting.
	put("s2", "working", "")
	if cardList() != listByName["Waiting on me"] {
		t.Fatal("working beat waiting")
	}
	// The member was told the session is waiting.
	var notes NotificationPage
	alice.Must(200, "GET", "/me/notifications", nil, &notes)
	if len(notes.Items) == 0 || notes.Items[0].Activity.Type != "link.status" {
		t.Fatalf("no waiting notice: %+v", notes.Items)
	}
	// Repeating a status writes no new activity row.
	var before, after ActivityPage
	alice.Must(200, "GET", "/cards/"+c.Id+"/activity", nil, &before)
	put("s2", "working", "")
	alice.Must(200, "GET", "/cards/"+c.Id+"/activity", nil, &after)
	if len(after.Items) != len(before.Items) {
		t.Fatal("heartbeat wrote activity")
	}
	// Quiet sessions go stale.
	e.Clock.Add(31 * time.Minute)
	if n, _ := e.Svc.MarkStaleLinks(ctx); n != 2 {
		t.Fatalf("stale = %d", n)
	}
	var links []CardLink
	alice.Must(200, "GET", "/cards/"+c.Id+"/links", nil, &links)
	for _, l := range links {
		if l.Status == nil || *l.Status != "stale" {
			t.Fatalf("link %s status %v", l.ExternalKey, l.Status)
		}
	}
}
