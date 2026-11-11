package server_test

import (
	"bufio"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	. "kowhai/internal/apitypes"
	"kowhai/internal/testenv"
)

func setup(t *testing.T) (*testenv.Env, *testenv.Client, Workspace, Board, []List) {
	e := testenv.New(t)
	alice := e.Login("alice@example.com")
	var ws Workspace
	alice.Must(201, "POST", "/workspaces", CreateWorkspace{Name: "Team"}, &ws)
	var b Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "Roadmap"}, &b)
	var lists []List
	for _, n := range []string{"To do", "Doing", "Done"} {
		var l List
		alice.Must(201, "POST", "/boards/"+b.Id+"/lists", CreateList{Name: n}, &l)
		lists = append(lists, l)
	}
	return e, alice, ws, b, lists
}

func TestLoginAndMe(t *testing.T) {
	for _, alg := range []string{"ES256", "RS256"} {
		t.Run(alg, func(t *testing.T) {
			e := testenv.New(t, testenv.WithAlg(alg))
			c := e.Login("alice@example.com")
			var me Me
			c.Must(200, "GET", "/me", nil, &me)
			if me.User.Email != "alice@example.com" || !me.User.IsAdmin {
				t.Fatalf("me = %+v", me.User)
			}
			bob := e.Login("bob@example.com")
			bob.Must(200, "GET", "/me", nil, &me)
			if me.User.IsAdmin {
				t.Fatal("bob should not be admin")
			}
		})
	}
}

func TestAnonymousAndCSRF(t *testing.T) {
	e := testenv.New(t)
	anon := &testenv.Client{E: e, HTTP: e.HTTP()}
	if got := anon.Do("GET", "/me", nil, nil); got != 401 {
		t.Fatalf("anonymous /me = %d", got)
	}
	c := e.Login("alice@example.com")
	csrf := c.CSRF
	c.CSRF = ""
	if got := c.Do("POST", "/workspaces", CreateWorkspace{Name: "x"}, nil); got != 403 {
		t.Fatalf("missing CSRF = %d, want 403", got)
	}
	c.CSRF = "wrong"
	if got := c.Do("POST", "/workspaces", CreateWorkspace{Name: "x"}, nil); got != 403 {
		t.Fatalf("wrong CSRF = %d, want 403", got)
	}
	c.CSRF = csrf
	c.Must(201, "POST", "/workspaces", CreateWorkspace{Name: "x"}, nil)
}

func TestBoardFlow(t *testing.T) {
	_, alice, _, b, lists := setup(t)

	// Cards keep their order through creates and moves.
	var c1, c2, c3 Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "One"}, &c1)
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Two"}, &c2)
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Zero", BeforeId: &c1.Id}, &c3)
	var full BoardFull
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	if got := titles(full.Cards, lists[0].Id); got != "Zero,One,Two" {
		t.Fatalf("order = %s", got)
	}
	alice.Must(200, "POST", "/cards/"+c2.Id+"/move", MoveCard{ListId: lists[0].Id, AfterId: &c3.Id}, nil)
	alice.Must(200, "POST", "/cards/"+c1.Id+"/move", MoveCard{ListId: lists[1].Id}, nil)
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	if got := titles(full.Cards, lists[0].Id); got != "Zero,Two" {
		t.Fatalf("order after move = %s", got)
	}
	if got := titles(full.Cards, lists[1].Id); got != "One" {
		t.Fatalf("list 2 = %s", got)
	}

	// Optimistic locking.
	var upd Card
	alice.Must(200, "PATCH", "/cards/"+c1.Id, UpdateCard{Title: ptr("One!")}, &upd, "If-Match", "2")
	if got := alice.Do("PATCH", "/cards/"+c1.Id, UpdateCard{Title: ptr("stale")}, nil, "If-Match", "2"); got != 412 {
		t.Fatalf("stale If-Match = %d, want 412", got)
	}

	// Labels, checklists, comments and counts.
	var labels []Label
	alice.Must(200, "GET", "/boards/"+b.Id+"/labels", nil, &labels)
	if len(labels) != 6 {
		t.Fatalf("default labels = %d", len(labels))
	}
	alice.Must(200, "POST", "/cards/"+c1.Id+"/labels", LabelRefInput{LabelId: labels[0].Id}, nil)
	var cl Checklist
	alice.Must(201, "POST", "/cards/"+c1.Id+"/checklists", CreateChecklist{Title: "Steps"}, &cl)
	var item ChecklistItem
	alice.Must(201, "POST", "/checklists/"+cl.Id+"/items", CreateChecklistItem{Text: "Write tests"}, &item)
	alice.Must(201, "POST", "/checklists/"+cl.Id+"/items", CreateChecklistItem{Text: "Ship"}, nil)
	alice.Must(200, "PATCH", "/checklist-items/"+item.Id, UpdateChecklistItem{Done: ptr(true)}, nil)
	alice.Must(201, "POST", "/cards/"+c1.Id+"/comments", CommentInput{Body: "Looks good"}, nil)
	var detail CardDetail
	alice.Must(200, "GET", "/cards/"+c1.Id, nil, &detail)
	if detail.Counts.ChecklistItems != 2 || detail.Counts.ChecklistDone != 1 || detail.Counts.Comments != 1 || len(detail.LabelIds) != 1 {
		t.Fatalf("counts = %+v labels=%v", detail.Counts, detail.LabelIds)
	}

	// Archive hides the card from the board.
	alice.Must(200, "PATCH", "/cards/"+c3.Id, map[string]any{"archived": true}, nil)
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)
	if got := titles(full.Cards, lists[0].Id); got != "Two" {
		t.Fatalf("after archive = %s", got)
	}
	alice.Must(200, "GET", "/boards/"+b.Id+"/full?archived=true", nil, &full)
	if got := titles(full.Cards, lists[0].Id); got != "Zero,Two" {
		t.Fatalf("with archived = %s", got)
	}

	// Due dates can be set and cleared.
	alice.Must(200, "PATCH", "/cards/"+c1.Id, map[string]any{"due_at": "2026-11-20T05:00:00Z"}, &upd)
	if upd.DueAt == nil {
		t.Fatal("due_at not set")
	}
	var cleared Card
	alice.Must(200, "PATCH", "/cards/"+c1.Id, map[string]any{"due_at": nil}, &cleared)
	if cleared.DueAt != nil {
		t.Fatal("due_at not cleared")
	}

	// Activity is recorded, newest first.
	var acts ActivityPage
	alice.Must(200, "GET", "/cards/"+c1.Id+"/activity", nil, &acts)
	if len(acts.Items) < 5 || acts.Items[0].Type != "card.updated" {
		t.Fatalf("activity = %d items, first %s", len(acts.Items), acts.Items[0].Type)
	}

	// Delete.
	alice.Must(204, "DELETE", "/cards/"+c2.Id, nil, nil)
	if got := alice.Do("GET", "/cards/"+c2.Id, nil, nil); got != 404 {
		t.Fatalf("deleted card = %d", got)
	}
}

func titles(cards []Card, listID string) string {
	var out []string
	for _, c := range cards {
		if c.ListId == listID {
			out = append(out, c.Title)
		}
	}
	return strings.Join(out, ",")
}

func ptr[T any](v T) *T { return &v }

func TestMembershipAndVisibility(t *testing.T) {
	e, alice, ws, b, lists := setup(t)
	bob := e.Login("bob@example.com")
	carol := e.Login("carol@example.com")
	var me Me
	bob.Must(200, "GET", "/me", nil, &me)
	bobID := me.User.Id
	carol.Must(200, "GET", "/me", nil, &me)
	carolID := me.User.Id

	// Bob cannot see the workspace until he is added.
	if got := bob.Do("GET", "/boards/"+b.Id, nil, nil); got != 404 {
		t.Fatalf("outsider board = %d", got)
	}
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/members", AddWorkspaceMember{UserId: bobID, Role: "member"}, nil)
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/members", AddWorkspaceMember{UserId: carolID, Role: "viewer"}, nil)

	// Members edit workspace boards. Viewers only read.
	bob.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "By Bob"}, nil)
	carol.Must(200, "GET", "/boards/"+b.Id+"/full", nil, nil)
	if got := carol.Do("POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "By Carol"}, nil); got != 403 {
		t.Fatalf("viewer create = %d", got)
	}
	if got := carol.Do("POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "x"}, nil); got != 403 {
		t.Fatalf("viewer board create = %d", got)
	}

	// Private boards are hidden from plain members.
	var priv Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "Secret", Visibility: ptr(CreateBoardVisibility("private"))}, &priv)
	if got := bob.Do("GET", "/boards/"+priv.Id, nil, nil); got != 404 {
		t.Fatalf("private board for member = %d", got)
	}
	var boards []Board
	bob.Must(200, "GET", "/workspaces/"+ws.Id+"/boards", nil, &boards)
	for _, x := range boards {
		if x.Id == priv.Id {
			t.Fatal("private board listed for member")
		}
	}
	// Adding Bob to the board shows it to him.
	alice.Must(201, "POST", "/boards/"+priv.Id+"/members", AddBoardMember{UserId: bobID, Role: "viewer"}, nil)
	bob.Must(200, "GET", "/boards/"+priv.Id, nil, nil)
	var pl List
	if got := bob.Do("POST", "/boards/"+priv.Id+"/lists", CreateList{Name: "x"}, &pl); got != 403 {
		t.Fatalf("board viewer edit = %d", got)
	}

	// Bob was told about both invites.
	var notes NotificationPage
	bob.Must(200, "GET", "/me/notifications", nil, &notes)
	if notes.Unread != 2 {
		t.Fatalf("bob unread = %d", notes.Unread)
	}

	// The last owner cannot leave.
	alice.Must(200, "GET", "/me", nil, &me)
	if got := alice.Do("DELETE", "/workspaces/"+ws.Id+"/members/"+me.User.Id, nil, nil); got != 409 {
		t.Fatalf("last owner leave = %d", got)
	}
	// Members cannot promote themselves.
	if got := bob.Do("PATCH", "/workspaces/"+ws.Id+"/members/"+bobID, UpdateWorkspaceMember{Role: "admin"}, nil); got != 403 {
		t.Fatalf("self promote = %d", got)
	}
}

func TestInviteAcceptedOnLogin(t *testing.T) {
	e := testenv.New(t)
	alice := e.Login("alice@example.com")
	var ws Workspace
	alice.Must(201, "POST", "/workspaces", CreateWorkspace{Name: "Team"}, &ws)
	var res InviteResult
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/invites", CreateInvite{Email: "dave@example.com", Role: "member"}, &res)
	if res.Invite == nil {
		t.Fatal("expected a pending invite")
	}
	dave := e.Login("dave@example.com")
	var page WorkspacePage
	dave.Must(200, "GET", "/workspaces", nil, &page)
	if len(page.Items) != 1 || page.Items[0].Role != "member" {
		t.Fatalf("dave workspaces = %+v", page.Items)
	}
	var invites []WorkspaceInvite
	alice.Must(200, "GET", "/workspaces/"+ws.Id+"/invites", nil, &invites)
	if len(invites) != 0 {
		t.Fatal("invite not consumed")
	}
}

func TestTokens(t *testing.T) {
	e, alice, ws, b, lists := setup(t)
	var other Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "Other"}, &other)

	var ro, scoped CreatedToken
	alice.Must(201, "POST", "/me/tokens", CreateToken{Name: "ro", Scopes: []TokenScope{"read"}}, &ro)
	alice.Must(201, "POST", "/me/tokens", CreateToken{Name: "claude", Scopes: []TokenScope{"write"}, BoardId: &b.Id, ActorLabel: ptr("Claude")}, &scoped)
	if !strings.HasPrefix(ro.Token, "kowhai_pat_") {
		t.Fatalf("token = %q", ro.Token)
	}

	r := e.WithToken(ro.Token)
	r.Must(200, "GET", "/boards/"+b.Id+"/full", nil, nil)
	if got := r.Do("POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "x"}, nil); got != 403 {
		t.Fatalf("read-only write = %d", got)
	}
	// Bearer requests skip CSRF.
	s := e.WithToken(scoped.Token)
	var c Card
	s.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "From Claude"}, &c)
	if got := s.Do("GET", "/boards/"+other.Id, nil, nil); got != 404 {
		t.Fatalf("board-limited token on other board = %d", got)
	}
	if got := s.Do("POST", "/workspaces", CreateWorkspace{Name: "x"}, nil); got != 403 {
		t.Fatalf("board-limited token creating workspace = %d", got)
	}
	if got := s.Do("POST", "/me/tokens", CreateToken{Name: "x", Scopes: []TokenScope{"read"}}, nil); got != 403 {
		t.Fatalf("token minting token = %d", got)
	}
	// The actor label shows on activity.
	var acts ActivityPage
	alice.Must(200, "GET", "/cards/"+c.Id+"/activity", nil, &acts)
	if acts.Items[0].ActorLabel != "Claude" {
		t.Fatalf("actor label = %q", acts.Items[0].ActorLabel)
	}
	// Revoked tokens stop working.
	alice.Must(204, "DELETE", "/me/tokens/"+ro.Id, nil, nil)
	if got := r.Do("GET", "/me", nil, nil); got != 401 {
		t.Fatalf("revoked token = %d", got)
	}
	if got := e.WithToken("kowhai_pat_nope").Do("GET", "/me", nil, nil); got != 401 {
		t.Fatalf("bad token = %d", got)
	}
}

func TestSearch(t *testing.T) {
	e, alice, ws, b, lists := setup(t)
	var c Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Fix the kōwhai tree", Description: ptr("Prune branches")}, &c)
	alice.Must(201, "POST", "/cards/"+c.Id+"/comments", CommentInput{Body: "Needs a ladder"}, nil)
	var other Card
	alice.Must(201, "POST", "/lists/"+lists[1].Id+"/cards", CreateCard{Title: "Unrelated"}, &other)

	var res SearchResults
	alice.Must(200, "GET", "/search?q=kowhai", nil, &res)
	if len(res.Groups) != 1 || res.Groups[0].Hits[0].CardId != c.Id {
		t.Fatalf("search kowhai = %+v", res)
	}
	alice.Must(200, "GET", "/search?q=ladd", nil, &res)
	if len(res.Groups) != 1 || res.Groups[0].Hits[0].EntityType != "comment" {
		t.Fatalf("prefix search on comment = %+v", res)
	}
	// Filters.
	var labels []Label
	alice.Must(200, "GET", "/boards/"+b.Id+"/labels", nil, &labels)
	alice.Must(200, "POST", "/cards/"+other.Id+"/labels", LabelRefInput{LabelId: labels[3].Id}, nil)
	alice.Must(200, "GET", "/search?q=label:red", nil, &res)
	if len(res.Groups) != 1 || res.Groups[0].Hits[0].CardId != other.Id {
		t.Fatalf("label filter = %+v", res)
	}
	alice.Must(200, "PATCH", "/cards/"+other.Id, map[string]any{"due_at": "2026-11-10T00:00:00Z"}, nil)
	alice.Must(200, "GET", "/search?q=due:overdue", nil, &res)
	if len(res.Groups) != 1 || res.Groups[0].Hits[0].CardId != other.Id {
		t.Fatalf("overdue filter = %+v", res)
	}
	// Outsiders see nothing.
	bob := e.Login("bob@example.com")
	bob.Must(200, "GET", "/search?q=kowhai", nil, &res)
	if len(res.Groups) != 0 {
		t.Fatal("outsider found results")
	}
	_ = ws
	// Syntax that would break FTS is made safe.
	alice.Must(200, "GET", "/search?q=%22AND%28*", nil, nil)
}

func TestAttachments(t *testing.T) {
	e, alice, _, _, lists := setup(t)
	var c Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Pics"}, &c)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "../../evil\".png")
	fw.Write(png)
	mw.Close()
	req, _ := http.NewRequest("POST", e.URL+"/api/v1/cards/"+c.Id+"/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Kowhai-CSRF", alice.CSRF)
	resp, err := alice.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload = %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	var d CardDetail
	alice.Must(200, "GET", "/cards/"+c.Id, nil, &d)
	if len(d.Attachments) != 1 || d.Attachments[0].ContentType != "image/png" || d.Attachments[0].Filename != "evil.png" {
		t.Fatalf("attachment = %+v", d.Attachments)
	}
	resp, _ = alice.HTTP.Get(e.URL + d.Attachments[0].Url)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, png) || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("download mismatch, disposition %q", resp.Header.Get("Content-Disposition"))
	}
	// Outsiders cannot download.
	bob := e.Login("bob@example.com")
	resp, _ = bob.HTTP.Get(e.URL + d.Attachments[0].Url)
	if resp.StatusCode != 404 {
		t.Fatalf("outsider download = %d", resp.StatusCode)
	}
	// Delete queues the object; the worker removes it.
	alice.Must(204, "DELETE", "/attachments/"+d.Attachments[0].Id, nil, nil)
	if n, _ := e.Svc.CleanupBlobs(t.Context()); n != 1 || len(e.Blob.Objs) != 0 {
		t.Fatalf("cleanup removed %d, %d left", n, len(e.Blob.Objs))
	}
}

func TestUploadTooLarge(t *testing.T) {
	e, alice, _, _, lists := setup(t)
	var c Card
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Big"}, &c)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "big.bin")
	fw.Write(bytes.Repeat([]byte("x"), 2<<20))
	mw.Close()
	req, _ := http.NewRequest("POST", e.URL+"/api/v1/cards/"+c.Id+"/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Kowhai-CSRF", alice.CSRF)
	resp, err := alice.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

// readEvents reads SSE events from a stream until n "activity" or
// "resync" events arrive or the timeout passes.
func readEvents(t *testing.T, body io.Reader, n int, timeout time.Duration) []string {
	t.Helper()
	ch := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(body)
		name := ""
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				name = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok && (name == "activity" || name == "resync") {
				ch <- name + " " + v
			}
		}
		close(ch)
	}()
	var out []string
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
	return out
}

func TestRealTime(t *testing.T) {
	e, alice, _, b, lists := setup(t)
	var full BoardFull
	alice.Must(200, "GET", "/boards/"+b.Id+"/full", nil, &full)

	resp, err := alice.HTTP.Get(e.URL + "/api/v1/boards/" + b.Id + "/events?last_event_id=" + *full.LastEventId)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
	}
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Live"}, nil)
	evs := readEvents(t, resp.Body, 1, 3*time.Second)
	if len(evs) != 1 || !strings.Contains(evs[0], `"type":"card.created"`) || !strings.Contains(evs[0], `"title":"Live"`) {
		t.Fatalf("events = %v", evs)
	}

	// Replay: events made while disconnected arrive on reconnect.
	alice.Must(201, "POST", "/lists/"+lists[0].Id+"/cards", CreateCard{Title: "Missed"}, nil)
	req, _ := http.NewRequest("GET", e.URL+"/api/v1/boards/"+b.Id+"/events", nil)
	req.Header.Set("Last-Event-ID", *full.LastEventId)
	resp2, err := alice.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	evs = readEvents(t, resp2.Body, 2, 3*time.Second)
	if len(evs) != 2 || !strings.Contains(evs[1], "Missed") {
		t.Fatalf("replay = %v", evs)
	}

	// Outsiders cannot subscribe.
	bob := e.Login("bob@example.com")
	r3, _ := bob.HTTP.Get(e.URL + "/api/v1/boards/" + b.Id + "/events")
	r3.Body.Close()
	if r3.StatusCode != 404 {
		t.Fatalf("outsider stream = %d", r3.StatusCode)
	}
}

func TestHealthAndDocs(t *testing.T) {
	e := testenv.New(t)
	for _, p := range []string{"/healthz", "/readyz", "/api/docs", "/api/v1/openapi.yaml"} {
		resp, err := e.HTTP().Get(e.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s = %d", p, resp.StatusCode)
		}
	}
	resp, _ := e.HTTP().Get(e.URL + "/api/v1/nope")
	if resp.StatusCode != 404 || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("unknown API path = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestRateLimit(t *testing.T) {
	e := testenv.New(t, testenv.WithRateLimit(2))
	c := e.Login("alice@example.com")
	limited := false
	for i := 0; i < 20; i++ {
		if c.Do("GET", "/me", nil, nil) == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("never rate limited")
	}
}
