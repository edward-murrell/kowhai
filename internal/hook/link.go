package hook

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	. "kowhai/internal/apitypes"
)

// Link runs `kowhai link`. It picks or creates a card and writes .kowhai
// in the current folder.
func Link(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	server := fs.String("server", "", "Kōwhai URL (default from KOWHAI_URL or ~/.config/kowhai/config.toml)")
	card := fs.String("card", "", "link to this card ID")
	list := fs.String("list", "", "create the card in this list ID")
	title := fs.String("title", "", "title for a new card (default: folder name)")
	printConfig := fs.Bool("print-config", false, "print the Claude Code hook settings and MCP command, then exit")
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprint(stdout, `Usage: kowhai link [flags]

Links this folder to a Kōwhai card by writing a .kowhai file. The file
holds no secrets and can be committed. Without flags, it asks which board,
list and card to use.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	uc := LoadUserConfig()
	if *server == "" {
		*server = uc.Server
	}
	if *printConfig {
		printSettings(stdout, *server)
		return nil
	}
	if *server == "" {
		return errors.New("no server: pass --server, set KOWHAI_URL or add server to ~/.config/kowhai/config.toml")
	}
	if uc.Token == "" {
		return errNoToken
	}
	c := &Client{Server: strings.TrimRight(*server, "/"), Token: uc.Token}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	in := bufio.NewReader(stdin)

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if *title == "" {
		*title = filepath.Base(cwd)
	}
	cardID := *card
	switch {
	case cardID != "":
		var cd CardDetail
		if err := c.do(ctx, "GET", "/cards/"+pathEsc(cardID), nil, &cd); err != nil {
			return fmt.Errorf("check card: %w", err)
		}
		fmt.Fprintf(stdout, "Card: %s\n", cd.Title)
	case *list != "":
		var cd Card
		if err := c.do(ctx, "POST", "/lists/"+pathEsc(*list)+"/cards", CreateCard{Title: *title}, &cd); err != nil {
			return fmt.Errorf("create card: %w", err)
		}
		cardID = cd.Id
		fmt.Fprintf(stdout, "Created card %q.\n", cd.Title)
	default:
		cardID, err = pickCard(ctx, c, in, stdout, *title)
		if err != nil {
			return err
		}
	}
	path := filepath.Join(cwd, ProjectFile)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "# Links this project to a Kōwhai card. No secrets here: the token comes")
	fmt.Fprintln(f, "# from KOWHAI_TOKEN or ~/.config/kowhai/config.toml.")
	if err := toml.NewEncoder(f).Encode(Project{Server: c.Server, Card: cardID}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote %s.\n\nNext, add the hooks and MCP server. Run:\n  kowhai link --print-config\n", path)
	return nil
}

func prompt(in *bufio.Reader, out io.Writer, q string, n int) (int, error) {
	for {
		fmt.Fprintf(out, "%s [1-%d]: ", q, n)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return 0, errors.New("no answer")
		}
		v, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && v >= 1 && v <= n {
			return v - 1, nil
		}
		fmt.Fprintln(out, "Pick a number from the list.")
	}
}

func pickCard(ctx context.Context, c *Client, in *bufio.Reader, out io.Writer, title string) (string, error) {
	var ws WorkspacePage
	if err := c.do(ctx, "GET", "/workspaces", nil, &ws); err != nil {
		return "", err
	}
	type choice struct{ id, label string }
	var boards []choice
	for _, w := range ws.Items {
		var bs []Board
		if err := c.do(ctx, "GET", "/workspaces/"+pathEsc(w.Id)+"/boards", nil, &bs); err != nil {
			return "", err
		}
		for _, b := range bs {
			boards = append(boards, choice{b.Id, w.Name + " / " + b.Name})
		}
	}
	if len(boards) == 0 {
		return "", errors.New("no boards: create one in Kōwhai first")
	}
	for i, b := range boards {
		fmt.Fprintf(out, "%2d) %s\n", i+1, b.label)
	}
	bi, err := prompt(in, out, "Board", len(boards))
	if err != nil {
		return "", err
	}
	var full BoardFull
	if err := c.do(ctx, "GET", "/boards/"+pathEsc(boards[bi].id)+"/full", nil, &full); err != nil {
		return "", err
	}
	if len(full.Lists) == 0 {
		return "", errors.New("that board has no lists")
	}
	for i, l := range full.Lists {
		fmt.Fprintf(out, "%2d) %s\n", i+1, l.Name)
	}
	li, err := prompt(in, out, "List", len(full.Lists))
	if err != nil {
		return "", err
	}
	list := full.Lists[li]
	var cards []Card
	for _, cd := range full.Cards {
		if cd.ListId == list.Id {
			cards = append(cards, cd)
		}
	}
	fmt.Fprintf(out, "%2d) New card: %s\n", 1, title)
	for i, cd := range cards {
		fmt.Fprintf(out, "%2d) %s\n", i+2, cd.Title)
	}
	ci, err := prompt(in, out, "Card", len(cards)+1)
	if err != nil {
		return "", err
	}
	if ci > 0 {
		return cards[ci-1].Id, nil
	}
	var cd Card
	if err := c.do(ctx, "POST", "/lists/"+pathEsc(list.Id)+"/cards", CreateCard{Title: title}, &cd); err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Created card %q.\n", cd.Title)
	return cd.Id, nil
}

// HookSettings is the hooks block for .claude/settings.json.
func HookSettings() map[string]any {
	entry := []map[string]any{{"hooks": []map[string]any{{"type": "command", "command": "kowhai hook", "timeout": 5}}}}
	hooks := map[string]any{}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "Notification", "Stop", "SessionEnd"} {
		hooks[ev] = entry
	}
	return map[string]any{"hooks": hooks}
}

func printSettings(out io.Writer, server string) {
	if server == "" {
		server = "https://<your-kowhai-host>"
	}
	b, _ := json.MarshalIndent(HookSettings(), "", "  ")
	fmt.Fprintf(out, `Add this to .claude/settings.json in the project (or merge it into the
"hooks" you already have):

%s

Add the MCP server so Claude can read and update cards:

  claude mcp add --transport http kowhai %s/mcp --header "Authorization: Bearer $KOWHAI_TOKEN"

Use a token limited to the board this project needs, with an actor label
such as "Claude (%s)". Create one under Account settings > API tokens.
`, b, strings.TrimRight(server, "/"), filepath.Base(mustCwd()))
}

func mustCwd() string {
	d, _ := os.Getwd()
	return d
}
