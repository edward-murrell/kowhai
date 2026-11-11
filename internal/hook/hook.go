package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "kowhai/internal/apitypes"
)

// Timeout bounds the whole hook run. It must never slow Claude down.
const Timeout = 2 * time.Second

// Event is the JSON Claude Code sends on stdin. Only the fields we use.
type Event struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	Cwd           string `json:"cwd"`
	Message       string `json:"message"`
	Source        string `json:"source"`
	Reason        string `json:"reason"`
}

// statusFor maps a Claude Code event to a link status.
func statusFor(e Event) (LinkStatus, string, bool) {
	switch e.HookEventName {
	case "SessionStart":
		return "working", "", true
	case "UserPromptSubmit":
		return "working", "", true
	case "Notification":
		return "waiting", e.Message, true
	case "Stop":
		return "idle", "", true
	case "SessionEnd":
		return "ended", e.Reason, true
	}
	return "", "", false
}

// Run handles one hook event. It never returns an error and never panics
// out: failures go to a log file and Claude Code carries on.
func Run(stdin io.Reader, stdout io.Writer, _ []string) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	defer func() {
		if v := recover(); v != nil {
			logf("panic: %v", v)
		}
	}()
	if err := run(ctx, stdin, stdout, nil); err != nil {
		logf("%v", err)
	}
}

func logf(format string, args ...any) {
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "hook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// Keep the log small.
	if st, err := f.Stat(); err == nil && st.Size() > 1<<20 {
		_ = f.Truncate(0)
	}
	log.New(f, "", log.LstdFlags).Printf(format, args...)
}

// run does the work. client is for tests; nil builds one from config.
func run(ctx context.Context, stdin io.Reader, stdout io.Writer, client *Client) error {
	var e Event
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&e); err != nil {
		return fmt.Errorf("read event: %w", err)
	}
	status, detail, ok := statusFor(e)
	if !ok || e.SessionID == "" {
		return nil
	}
	cwd := e.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	proj, _, err := FindProject(cwd)
	if err != nil {
		// Not a linked project: do nothing.
		return nil
	}
	if client == nil {
		uc := LoadUserConfig()
		server := proj.Server
		if server == "" {
			server = uc.Server
		}
		if uc.Token == "" {
			return errNoToken
		}
		client = &Client{Server: server, Token: uc.Token}
	}
	host, _ := os.Hostname()
	repo, branch := gitInfo(ctx, cwd)
	if len(detail) > 2000 {
		detail = detail[:2000]
	}
	body := PutCardLink{
		Status: &status, StatusDetail: &detail, Repo: &repo, Branch: &branch, Host: &host,
		Meta: &map[string]any{"cwd": cwd, "event": e.HookEventName},
	}
	if e.Source != "" {
		(*body.Meta)["source"] = e.Source
	}
	path := "/cards/" + pathEsc(proj.Card) + "/links/claude_code/" + pathEsc(e.SessionID)
	if err := client.do(ctx, "PUT", path, body, nil); err != nil {
		return fmt.Errorf("%s: %w", e.HookEventName, err)
	}
	if e.HookEventName == "SessionStart" {
		var c CardDetail
		if err := client.do(ctx, "GET", "/cards/"+pathEsc(proj.Card), nil, &c); err != nil {
			return fmt.Errorf("get card: %w", err)
		}
		_, _ = io.WriteString(stdout, cardContext(c, strings.TrimRight(client.Server, "/")))
	}
	return nil
}

// cardContext is the text Claude Code adds to the session at start.
func cardContext(c CardDetail, server string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This project is linked to the Kōwhai card %q (id %s, %s/c/%s).\n", c.Title, c.Id, server, c.Id)
	if d := strings.TrimSpace(c.Description); d != "" {
		if r := []rune(d); len(r) > 3000 {
			d = string(r[:3000]) + "…"
		}
		fmt.Fprintf(&b, "\nCard description:\n%s\n", d)
	}
	var open []string
	for _, cl := range c.Checklists {
		for _, it := range cl.Items {
			if !it.Done {
				open = append(open, fmt.Sprintf("- [ ] %s (%s, item %s)", it.Text, cl.Title, it.Id))
			}
		}
	}
	if len(open) > 0 {
		b.WriteString("\nOpen checklist items:\n")
		for _, l := range open {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

func gitInfo(ctx context.Context, dir string) (repo, branch string) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if top := run("rev-parse", "--show-toplevel"); top != "" {
		repo = filepath.Base(top)
	}
	if u := run("remote", "get-url", "origin"); u != "" {
		repo = repoFromRemote(u)
	}
	branch = run("branch", "--show-current")
	return repo, branch
}

// repoFromRemote turns git@github.com:me/app.git or
// https://github.com/me/app.git into me/app.
func repoFromRemote(u string) string {
	u = strings.TrimSuffix(strings.TrimSpace(u), ".git")
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.Index(u, "/"); j >= 0 {
			u = u[j+1:]
		}
	} else if i := strings.Index(u, ":"); i >= 0 {
		u = u[i+1:]
	}
	return u
}
