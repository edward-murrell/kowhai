package hook

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/testenv"
)

func event(t *testing.T, name, cwd string) *strings.Reader {
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(strings.ReplaceAll(string(b), "CWD", cwd))
}

func TestHookEvents(t *testing.T) {
	e := testenv.New(t)
	alice := e.Login("alice@example.com")
	var ws Workspace
	alice.Must(201, "POST", "/workspaces", CreateWorkspace{Name: "Team"}, &ws)
	var b Board
	alice.Must(201, "POST", "/workspaces/"+ws.Id+"/boards", CreateBoard{Name: "B"}, &b)
	var l List
	alice.Must(201, "POST", "/boards/"+b.Id+"/lists", CreateList{Name: "L"}, &l)
	var c Card
	alice.Must(201, "POST", "/lists/"+l.Id+"/cards", CreateCard{Title: "Hook card", Description: ptr("Do the thing")}, &c)
	var cl Checklist
	alice.Must(201, "POST", "/cards/"+c.Id+"/checklists", CreateChecklist{Title: "Steps"}, &cl)
	alice.Must(201, "POST", "/checklists/"+cl.Id+"/items", CreateChecklistItem{Text: "First step"}, nil)
	var tok CreatedToken
	alice.Must(201, "POST", "/me/tokens", CreateToken{Name: "hook", Scopes: []TokenScope{"write"}, BoardId: &b.Id}, &tok)

	// A linked project, with the hook run from a sub-folder.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectFile), []byte("server = \""+e.URL+"\"\ncard = \""+c.Id+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "src", "pkg")
	os.MkdirAll(sub, 0o755)
	client := &Client{Server: e.URL, Token: tok.Token, HTTP: e.HTTP()}

	status := func() string {
		var links []CardLink
		alice.Must(200, "GET", "/cards/"+c.Id+"/links", nil, &links)
		if len(links) != 1 || links[0].Status == nil {
			t.Fatalf("links = %+v", links)
		}
		return string(*links[0].Status)
	}

	var out bytes.Buffer
	if err := run(context.Background(), event(t, "session_start", sub), &out, client); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Hook card") || !strings.Contains(out.String(), "First step") || !strings.Contains(out.String(), "Do the thing") {
		t.Fatalf("context = %s", out.String())
	}
	if status() != "working" {
		t.Fatal("not working after start")
	}
	for _, step := range []struct{ ev, want string }{
		{"notification", "waiting"}, {"user_prompt_submit", "working"}, {"stop", "idle"}, {"session_end", "ended"},
	} {
		out.Reset()
		if err := run(context.Background(), event(t, step.ev, sub), &out, client); err != nil {
			t.Fatalf("%s: %v", step.ev, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s printed %q", step.ev, out.String())
		}
		if got := status(); got != step.want {
			t.Fatalf("after %s: %s, want %s", step.ev, got, step.want)
		}
	}
	var links []CardLink
	alice.Must(200, "GET", "/cards/"+c.Id+"/links", nil, &links)
	if links[0].Host == "" || links[0].ExternalKey != "abc123-session" {
		t.Fatalf("link = %+v", links[0])
	}
}

func TestHookIgnoresUnlinkedFolders(t *testing.T) {
	client := &Client{Server: "http://unused.invalid", Token: "x"}
	if err := run(context.Background(), event(t, "stop", t.TempDir()), &bytes.Buffer{}, client); err != nil {
		t.Fatal(err)
	}
}

// When the server is down the hook gives up within its timeout and
// Run returns normally, so Claude Code is never held up.
func TestHookServerDown(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ProjectFile), []byte("server = \"http://10.255.255.1:9\"\ncard = \"c1\"\n"), 0o644)
	t.Setenv("KOWHAI_TOKEN", "kowhai_pat_x")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	start := time.Now()
	Run(event(t, "stop", root), &bytes.Buffer{}, nil)
	if d := time.Since(start); d > Timeout+time.Second {
		t.Fatalf("hook took %v", d)
	}
	logged, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "kowhai", "hook.log"))
	if !strings.Contains(string(logged), "Stop") {
		t.Fatalf("failure not logged: %q", logged)
	}
}

func TestHookBadInput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	Run(strings.NewReader("not json"), &bytes.Buffer{}, nil)
}

func TestRepoFromRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:me/app.git":     "me/app",
		"https://github.com/me/app.git": "me/app",
		"ssh://git@host:22/team/app":    "team/app",
	} {
		if got := repoFromRemote(in); got != want {
			t.Errorf("%s = %s", in, got)
		}
	}
}

func TestHookSettings(t *testing.T) {
	h := HookSettings()["hooks"].(map[string]any)
	if len(h) != 5 {
		t.Fatalf("hooks = %v", h)
	}
}

func ptr[T any](v T) *T { return &v }
