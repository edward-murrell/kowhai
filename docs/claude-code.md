# Claude Code and Kōwhai

There are two parts. The hook reports each session's status to a card on its own. The MCP server lets Claude read and change cards when it decides to.

## 1. Make a token

In Kōwhai, open Account settings and create an API token.

- Limit it to the board the project uses.
- Give it write access.
- Set an actor label such as `Claude (my-repo)`. Changes made with the token show this label next to your name.

Save the token where the `kowhai` command can find it:

```toml
# ~/.config/kowhai/config.toml
server = "https://kowhai.example.com"
token = "kowhai_pat_..."
```

`KOWHAI_TOKEN` and `KOWHAI_URL` override the file.

## 2. Link the project

In the project folder:

```sh
kowhai link
```

It asks for a board, a list and a card, or makes a new card named after the folder. It writes `.kowhai`, which holds the server and card ID. The file has no secrets, so you can commit it. Use `--card <id>` or `--list <id> --title <title>` to skip the questions.

## 3. Add the hooks and MCP server

```sh
kowhai link --print-config
```

This prints a `hooks` block for `.claude/settings.json` and the command to add the MCP server:

```sh
claude mcp add --transport http kowhai https://kowhai.example.com/mcp --header "Authorization: Bearer $KOWHAI_TOKEN"
```

## What the hook does

Claude Code runs `kowhai hook` on these events:

| Event | Card status |
|---|---|
| SessionStart | working. Claude also gets the card's title, description and open checklist items. |
| UserPromptSubmit | working |
| Notification | waiting. Card members get a notification with the message. |
| Stop | idle |
| SessionEnd | ended |

The hook gives up after 2 seconds, logs failures to `~/.local/state/kowhai/hook.log` and always exits 0. It does nothing in folders without a `.kowhai` file.

A session that says working or waiting with no update for 30 minutes is marked stale. A card with several sessions shows the most urgent status.

## Moving cards by status

In Board settings under Claude Code, you can map each status to a list. For example, a card can move to "Waiting on me" when Claude asks for permission. This is off by default. The built-in "Agent projects" template turns it on.

## MCP tools

| Tool | Purpose |
|---|---|
| `list_boards` | Boards the token can see |
| `get_board` | Lists and card summaries for a board |
| `search_cards` | Full-text search with `label:`, `member:`, `due:overdue` and `is:archived` |
| `get_card` | Full card with checklists, recent comments and sessions |
| `create_card` | New card, optionally from a card template |
| `update_card` | Title, description, labels or due date |
| `move_card` | Move to another list or position |
| `add_comment` | Comment on a card |
| `set_checklist_item` | Add, tick or untick an item |

Read-only tools carry `readOnlyHint`, so Claude Code can allow them without asking.
