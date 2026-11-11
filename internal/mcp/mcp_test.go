package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/testenv"
)

type bearer struct {
	tok string
	rt  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return b.rt.RoundTrip(r)
}

func connect(t *testing.T, e *testenv.Env, tok string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: e.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{tok, e.Transport}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any, out any) (string, bool) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if out != nil && !res.IsError {
		if err := json.Unmarshal([]byte(text), out); err != nil {
			t.Fatalf("%s: %v: %s", name, err, text)
		}
	}
	return text, res.IsError
}

func TestMCPTools(t *testing.T) {
	e := testenv.New(t)
	alice := e.Login("alice@example.com")
	var ws Workspace
	alice.Must(201, "POST", "/workspaces", CreateWorkspace{Name: "Team"}, &ws)
	var b, other Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "Agents"}, &b)
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "Private stuff"}, &other)
	var todo, done List
	alice.Must(201, "POST", "/boards/"+b.Id+"/lists", CreateList{Name: "To do"}, &todo)
	alice.Must(201, "POST", "/boards/"+b.Id+"/lists", CreateList{Name: "Done"}, &done)
	var hidden List
	alice.Must(201, "POST", "/boards/"+other.Id+"/lists", CreateList{Name: "x"}, &hidden)
	var secret Card
	alice.Must(201, "POST", "/lists/"+hidden.Id+"/cards", CreateCard{Title: "Secret plan"}, &secret)

	var tok CreatedToken
	alice.Must(201, "POST", "/me/tokens", CreateToken{Name: "claude", Scopes: []TokenScope{"write"}, BoardId: &b.Id, ActorLabel: ptr("Claude")}, &tok)
	s := connect(t, e, tok.Token)

	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 9 {
		t.Fatalf("tools = %d", len(tools.Tools))
	}
	for _, tl := range tools.Tools {
		ro := strings.HasPrefix(tl.Name, "list_") || strings.HasPrefix(tl.Name, "get_") || strings.HasPrefix(tl.Name, "search_")
		if tl.Annotations == nil || tl.Annotations.ReadOnlyHint != ro {
			t.Errorf("%s readOnlyHint wrong", tl.Name)
		}
	}

	// The token only reaches its board.
	var boards []map[string]any
	call(t, s, "list_boards", nil, &boards)
	if len(boards) != 1 || boards[0]["id"] != b.Id {
		t.Fatalf("boards = %v", boards)
	}
	if _, isErr := call(t, s, "get_card", map[string]any{"card_id": secret.Id}, nil); !isErr {
		t.Fatal("got a card outside the token's board")
	}

	var created map[string]any
	call(t, s, "create_card", map[string]any{"list_id": todo.Id, "title": "Write MCP tests", "description": "Cover all tools", "labels": []string{"green"}}, &created)
	id := created["id"].(string)
	call(t, s, "set_checklist_item", map[string]any{"card_id": id, "text": "List tools"}, nil)
	var item map[string]any
	call(t, s, "set_checklist_item", map[string]any{"card_id": id, "text": "Call tools", "checklist": "Checklist"}, &item)
	call(t, s, "set_checklist_item", map[string]any{"card_id": id, "item_id": item["id"], "done": true}, nil)
	call(t, s, "add_comment", map[string]any{"card_id": id, "body": "Started"}, nil)
	call(t, s, "update_card", map[string]any{"card_id": id, "title": "Write MCP tests!", "due_at": "2026-12-01T00:00:00Z", "add_labels": []string{"red"}}, nil)
	call(t, s, "move_card", map[string]any{"card_id": id, "list_id": done.Id}, nil)

	var card map[string]any
	call(t, s, "get_card", map[string]any{"card_id": id}, &card)
	if card["title"] != "Write MCP tests!" || card["list"] != "Done" || card["due"] == nil {
		t.Fatalf("card = %v", card)
	}
	if labels := card["labels"].([]any); len(labels) != 2 {
		t.Fatalf("labels = %v", labels)
	}
	cls := card["checklists"].([]any)
	items := cls[0].(map[string]any)["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["done"] != true {
		t.Fatalf("checklist = %v", cls)
	}
	comments := card["recent_comments"].([]any)
	if len(comments) != 1 || !strings.Contains(comments[0].(map[string]any)["author"].(string), "(Claude)") {
		t.Fatalf("comments = %v", comments)
	}

	var board map[string]any
	call(t, s, "get_board", map[string]any{"board_id": b.Id}, &board)
	lists := board["lists"].([]any)
	if len(lists[1].(map[string]any)["cards"].([]any)) != 1 {
		t.Fatalf("board = %v", board)
	}

	var hits []map[string]any
	call(t, s, "search_cards", map[string]any{"query": "MCP"}, &hits)
	if len(hits) == 0 || hits[0]["card_id"] != id {
		t.Fatalf("search = %v", hits)
	}
	call(t, s, "search_cards", map[string]any{"query": "secret"}, &hits)
	if len(hits) != 0 {
		t.Fatal("search leaked another board")
	}

	// Changes went through the service: activity shows the actor label.
	var acts ActivityPage
	alice.Must(200, "GET", "/cards/"+id+"/activity", nil, &acts)
	for _, a := range acts.Items {
		if a.ActorLabel != "Claude" {
			t.Fatalf("activity %s has label %q", a.Type, a.ActorLabel)
		}
	}
}

func TestMCPReadOnlyAndAuth(t *testing.T) {
	e := testenv.New(t)
	alice := e.Login("alice@example.com")
	var ws Workspace
	alice.Must(201, "POST", "/workspaces", CreateWorkspace{Name: "Team"}, &ws)
	var b Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "B"}, &b)
	var l List
	alice.Must(201, "POST", "/boards/"+b.Id+"/lists", CreateList{Name: "L"}, &l)
	var tok CreatedToken
	alice.Must(201, "POST", "/me/tokens", CreateToken{Name: "ro", Scopes: []TokenScope{"read"}}, &tok)
	s := connect(t, e, tok.Token)
	if text, isErr := call(t, s, "create_card", map[string]any{"list_id": l.Id, "title": "x"}, nil); !isErr || !strings.Contains(text, "read-only") {
		t.Fatalf("read-only token wrote: %s", text)
	}
	// No token, no MCP.
	resp, err := e.HTTP().Post(e.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous mcp = %d", resp.StatusCode)
	}
}

func ptr[T any](v T) *T { return &v }
