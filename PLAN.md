# Kōwhai: plan

A self-hosted Trello-style board app for up to 1000 users.

The product name is "Kōwhai" in the UI and docs. Code, binaries, images, env vars and URLs use `kowhai`, since the macron breaks most tooling.

## Decisions so far

| Area | Choice |
|---|---|
| Backend | Go, single binary |
| Frontend | Vue 3 + TypeScript, built and embedded in the Go binary |
| Database | SQLite (WAL mode) |
| File storage | Garage (S3-compatible). Any S3 endpoint works via config. |
| Login | OIDC. Tested against Kanidm and Authelia. |
| API auth | Browser session cookie, plus personal access tokens (PATs) |
| Access model | Workspaces contain boards. Any user can create a workspace. |
| Deployment | Docker Compose: `kowhai` + `garage` |
| v1 features | Boards, lists, cards, comments, activity log, labels, due dates, checklists, attachments, real-time updates, search, in-app notifications, board and card templates, recurring cards |
| Claude Code | MCP server at `/mcp` for Claude to use boards. Hook command for automatic session status on cards. |
| Later | Email notifications, sign-in restrictions by group or email domain, Trello import, OAuth for claude.ai connectors |

## Architecture

```
Browser (Vue SPA)          Claude Code
   │  REST, SSE, /auth/*      │  MCP (/mcp)      `kowhai hook` ──► REST
   ▼                          ▼
kowhai (Go binary)
   ├── HTTP router + middleware (auth, CSRF, logging, rate limit)
   ├── MCP server (thin layer over the service layer)
   ├── Service layer (permissions, business rules)
   ├── Store (sqlc queries) ──► SQLite file (WAL)
   ├── Event hub (in-process pub/sub) ──► SSE clients
   ├── Blob store ──► Garage (S3 API)
   ├── Workers (due-date reminders, recurring cards, stale sessions, cleanup)
   └── Embedded SPA assets
```

One instance only. SQLite and the in-process event hub both assume this. At under 1000 users, one instance is plenty.

## Backend

### Libraries

| Need | Library |
|---|---|
| HTTP routing | `net/http` (Go 1.22+ patterns) or `chi` |
| OpenAPI | Spec-first. `oapi-codegen` generates the strict server interface. |
| SQLite driver | `modernc.org/sqlite` (pure Go, no cgo, simple static builds) |
| Queries | `sqlc` |
| Migrations | `goose`, embedded and run at startup |
| OIDC | `coreos/go-oidc/v3` + `golang.org/x/oauth2` |
| S3 | `aws-sdk-go-v2/service/s3` with path-style addressing |
| Recurrence rules | `teambition/rrule-go` (RFC 5545 RRULE) |
| MCP | `modelcontextprotocol/go-sdk` (streamable HTTP transport) |
| Config | Env vars, optional YAML file |
| Logging | `log/slog` (JSON) |

### SQLite setup

- `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`.
- Two pools. A write pool with one connection. A read pool with several.
- All writes go through short transactions on the write pool. This avoids `SQLITE_BUSY` errors.
- Backups: Litestream replicates the database to a Garage bucket. A nightly `VACUUM INTO` snapshot is a fallback.

### Package layout

```
cmd/kowhai/           main, config, wiring, `serve` / `hook` / `link` subcommands
internal/api/         generated OpenAPI code + handlers
internal/auth/        OIDC flow, sessions, PATs, CSRF
internal/service/     business logic and permission checks
internal/store/       sqlc output, migrations, tx helpers
internal/blob/        S3 client wrapper
internal/events/      event hub, SSE handler
internal/search/      FTS5 indexing and queries
internal/notify/      notification rules (email sender added later)
internal/template/    board and card template copying
internal/recur/       recurrence rules and scheduling
internal/mcp/         MCP tool definitions and handlers
internal/hook/        Claude Code hook client (runs on the developer's machine)
internal/worker/      scheduled jobs
api/openapi.yaml      the API contract
web/                  Vue app
deploy/               compose file, Garage config, init script
```

## Data model

All IDs are ULIDs stored as text. All tables have `created_at`. Mutable tables have `updated_at` and an integer `version` for optimistic locking.

**Users and auth**
- `users`: id, oidc_issuer, oidc_subject (unique pair), email, display_name, avatar_url, is_admin, disabled_at
- `sessions`: id (hashed), user_id, expires_at, last_seen_at, user_agent
- `api_tokens`: id, user_id, name, token_hash, prefix, scopes, workspace_id (nullable), board_id (nullable), actor_label, expires_at, last_used_at, revoked_at

**Access**
- `workspaces`: id, name, slug, created_by
- `workspace_members`: workspace_id, user_id, role (`owner` | `admin` | `member` | `viewer`)
- `workspace_invites`: workspace_id, email, role, invited_by, expires_at. Matched to a user on first login by email.
- `boards`: id, workspace_id, name, description, visibility (`private` | `workspace`), background, is_template, archived_at
- `board_members`: board_id, user_id, role (`admin` | `editor` | `viewer`)

**Content**
- `lists`: id, board_id, name, position, archived_at
- `cards`: id, list_id, board_id, title, description (Markdown), position, due_at, due_done, start_at, cover_attachment_id, is_template, recurrence_id (nullable, set on cards a recurrence created), archived_at
- `card_members`: card_id, user_id
- `card_watchers`: card_id, user_id
- `labels`: id, board_id, name, colour
- `card_labels`: card_id, label_id
- `checklists`: id, card_id, title, position
- `checklist_items`: id, checklist_id, text, done, position, assignee_id, due_at
- `comments`: id, card_id, author_id, body (Markdown), edited_at, deleted_at
- `attachments`: id, card_id, uploader_id, object_key, filename, content_type, size_bytes, sha256
- `card_links`: id, card_id, kind (`claude_code` | `url`), external_key (unique per kind, e.g. the Claude session ID), repo, branch, host, status (`working` | `waiting` | `idle` | `ended` | `stale`), status_detail, last_seen_at, meta (JSON)

**Templates and recurrence**
- `card_recurrences`: id, board_id, template_card_id, target_list_id, rrule, timezone, due_offset_minutes (nullable), next_run_at, last_run_at, paused_at, created_by

**Activity and notifications**
- `activities`: id, workspace_id, board_id, card_id (nullable), actor_id, token_id (nullable), type, payload (JSON)
- `notifications`: id, user_id, activity_id, read_at
- `notification_prefs`: user_id, settings (JSON). Holds per-trigger on/off now. Email settings go here later.

**Search**
- `search_index`: FTS5 virtual table over card titles, descriptions, comments and checklist items. Columns hold board_id and entity type for filtering.

### Ordering

Lists, cards, checklists and checklist items use fractional index keys (text). Moving an item writes one row. The server computes the key from the neighbour IDs the client sends. A rebalance job rewrites keys when they grow too long.

## Permissions

- Any user can create a workspace and becomes its owner.
- Workspace owners and admins invite users, manage members and create boards.
- Workspace members can create boards and see `workspace`-visible boards.
- Workspace viewers can see `workspace`-visible boards but not edit them.
- `private` boards are visible only to board members and workspace owners.
- Board role decides edit rights on that board. A workspace admin has board admin rights on all boards in the workspace.
- A site admin flag (from config or an OIDC group claim) allows user management and disabling accounts.

One function, `service.Authorize(ctx, actor, action, resource)`, makes every decision. Handlers never check roles directly. Table-driven tests cover the full role matrix.

## Authentication

### Browser login
1. `/auth/login` starts the authorisation code flow with PKCE and a `state` + `nonce`.
2. `/auth/callback` verifies the ID token. It finds or creates the user by `(issuer, sub)` and updates their email and name.
3. Pending workspace invites for that email are accepted.
4. The server creates a session row and sets an `HttpOnly`, `Secure`, `SameSite=Lax` cookie.
5. `/auth/logout` deletes the session. It redirects to the provider's end-session endpoint only if discovery lists one.

### Provider notes
- **Kanidm** requires PKCE with S256 and signs ID tokens with ES256 by default. `go-oidc` handles both. Group claims arrive as SPNs (`group@domain`). Kanidm does not offer RP-initiated logout, so logout is local only.
- **Authelia** supports PKCE and the `groups` scope. Group names arrive as plain names.
- The app requests `openid profile email groups`. Group names are stored on login but not used for access in v1.
- The dev Compose profile runs Authelia with a file-based user list, since it is simple to seed. CI runs login tests against both providers.

### Sign-in restrictions (later)
Any user who can log in through the provider gets an account in v1. Restricting this later is a small change. The callback gains one check before it creates a user. Config would be `KOWHAI_ALLOWED_GROUPS` and `KOWHAI_ALLOWED_EMAIL_DOMAINS`. Both providers can also restrict access per OIDC client on their side, which works today with no app change.

### Sessions and CSRF
The SPA never sees OIDC tokens. CSRF protection: state-changing requests need a custom header (`X-Kowhai-CSRF`) that matches a value from a non-HttpOnly cookie.

### Personal access tokens
- Created in the UI. Shown once. Format: `kowhai_pat_<random>`.
- Stored as a SHA-256 hash. A short prefix is kept for display.
- Scopes: `read`, `write`. Optional expiry. Revocable.
- Optional limit to one workspace or one board. A Claude session's token should only reach the boards it needs.
- Optional actor label, such as "Claude (kowhai repo)". Activity shows it next to the user's name, so agent changes are easy to spot.
- Sent as `Authorization: Bearer <token>`. CSRF checks are skipped for bearer auth.
- Tokens act as the user, limited by scope. `service.Authorize` checks the token limits as well as the user's roles.
- The same tokens work for the REST API, the MCP endpoint and the hook command.

## REST API

- Base path `/api/v1`. JSON only. The contract lives in `api/openapi.yaml`.
- Served docs at `/api/docs` (Scalar or Swagger UI). Raw spec at `/api/v1/openapi.yaml`.
- Errors use RFC 9457 `application/problem+json`.
- List endpoints use cursor pagination (`?cursor=&limit=`).
- Updates use `PATCH`. Clients send `If-Match` with the resource version. A stale version returns `412`.
- Rate limiting per user and per token.

### Main resources

```
GET    /me
GET    /me/notifications            PATCH /me/notifications/{id}
GET    /me/tokens                   POST /me/tokens   DELETE /me/tokens/{id}

GET    /workspaces                  POST /workspaces
GET    /workspaces/{id}             PATCH, DELETE
GET    /workspaces/{id}/members     POST, PATCH /{userId}, DELETE /{userId}
POST   /workspaces/{id}/invites

GET    /workspaces/{id}/boards      POST
GET    /workspaces/{id}/templates   board templates in this workspace
POST   /boards/{id}/copy            copy a board or create one from a template
GET    /boards/{id}                 PATCH, DELETE
GET    /boards/{id}/full            board + lists + cards + labels in one call
GET    /boards/{id}/members         POST, PATCH, DELETE
GET    /boards/{id}/labels          POST, PATCH /labels/{id}, DELETE
GET    /boards/{id}/activity
GET    /boards/{id}/events          SSE stream

POST   /boards/{id}/lists           PATCH /lists/{id}, POST /lists/{id}/move
POST   /lists/{id}/cards
GET    /cards/{id}                  PATCH, DELETE, POST /cards/{id}/move
POST   /cards/{id}/members          DELETE /cards/{id}/members/{userId}
POST   /cards/{id}/labels           DELETE /cards/{id}/labels/{labelId}
POST   /cards/{id}/watch            DELETE /cards/{id}/watch
GET    /cards/{id}/comments         POST, PATCH /comments/{id}, DELETE
POST   /cards/{id}/checklists       PATCH /checklists/{id}, DELETE
POST   /checklists/{id}/items       PATCH /checklist-items/{id}, DELETE
POST   /cards/{id}/attachments      multipart upload
GET    /cards/{id}/links            POST, PATCH /links/{id}, DELETE /links/{id}
PUT    /cards/{id}/links/{kind}/{externalKey}    create or update a link (used by the hook)
GET    /attachments/{id}/download   DELETE /attachments/{id}
GET    /boards/{id}/card-templates
POST   /cards/{id}/copy             copy a card or create one from a template

GET    /boards/{id}/recurrences     POST
GET    /recurrences/{id}            PATCH, DELETE
POST   /recurrences/{id}/run        create the next card now
GET    /recurrences/{id}/preview    next N run times

GET    /search?q=&workspace=&board=
```

The SPA uses only this API. That keeps the API complete by design.

## Attachments

- Uploads stream through the Go server to Garage. Garage stays on the internal network. No CORS or public bucket set-up needed.
- Max size set by config (default 25 MB). Content type detected from the bytes, not the client header.
- Object key: `attachments/{board_id}/{attachment_id}`.
- Downloads stream through the server after a permission check. Images are served inline. Other types use `Content-Disposition: attachment`.
- Deleting a card or attachment queues the object for removal by a cleanup worker.
- Image thumbnails can come later.

## Templates

**Board templates**
- Any board can be marked as a template. Templates show in the workspace's "New board" dialog.
- Creating from a template copies lists, labels, and optionally cards with their checklists.
- Members, comments, attachments and activity are not copied.
- Kōwhai ships a few built-in templates (Kanban, Sprint, Personal to-do), seeded per workspace on request.

**Card templates**
- Any card can be marked as a template. Template cards are hidden from normal lists and shown in a "Templates" menu on each list.
- Creating from a template copies title, description, labels, checklists and, optionally, members.
- Attachments are not copied in v1. Copying them means duplicating S3 objects, which can come later.

Copying runs in one transaction and writes one activity row.

## Recurring cards

- A recurrence points at a template card, a target list and an RRULE (RFC 5545).
- The UI offers presets (daily, weekdays, weekly on chosen days, monthly on a date, monthly on the nth weekday). An advanced field accepts a raw RRULE.
- Weeks start on Monday (`WKST=MO`) by default.
- Each recurrence stores an IANA timezone. Run times are calculated in that zone so daylight saving changes do not shift them.
- `due_offset_minutes` sets the new card's due date relative to its creation time.
- The worker runs every minute. It locks due recurrences, creates the cards, and advances `next_run_at`, all in one transaction.
- If the server was down, it creates one card for the most recent missed run, not one per missed run.
- Recurrences can be paused. Deleting the template card pauses its recurrences and notifies the creator.

## Real-time updates

- Every write produces an `activities` row in the same transaction.
- After commit, the service publishes the event to the in-process hub, keyed by board ID.
- Clients open one SSE stream per open board. Events carry the type, the entity ID, the new version and the changed fields.
- The client patches its Pinia store from the event. If it sees a gap or reconnects, it re-fetches `/boards/{id}/full`.
- SSE uses `Last-Event-ID` (the activity ID) so a short reconnect replays missed events.
- A per-user stream for notifications uses the same mechanism.

SSE fits because the browser only receives. All writes go through REST.

## Search

- SQLite FTS5 with the `unicode61` tokeniser and prefix indexes.
- The service layer updates the index in the same transaction as the source row.
- Queries join against the boards the user can see. Results are grouped by board.
- Simple filter syntax for v1: `label:`, `member:`, `due:overdue`, `is:archived`.

## Notifications

Triggers:
- You are added to a card.
- Someone @mentions you in a comment or description.
- Someone comments on a card you watch or are a member of.
- A card you are on is due within 24 hours, or is overdue.
- You are invited to a workspace or board.
- A recurrence you created was paused.

Delivery in v1 is in-app only. A bell icon shows the unread count, pushed over the user SSE stream. A worker runs every minute for due-date checks.

Email comes later. The `notifications` table can gain an `emailed_at` column, and a sender can read unsent rows. No other part of the design changes.

## Claude Code integration

Two parts with separate jobs. Hooks report session status automatically, so the board is always correct. MCP lets Claude read and update cards when it chooses to.

### Linking a project to a card
- `kowhai link` runs in a project folder. It picks or creates a card and writes a `.kowhai` file holding the server URL and card ID.
- `.kowhai` holds no secrets and can be committed. The token comes from `KOWHAI_TOKEN` or `~/.config/kowhai/config.toml`.
- `kowhai link --print-config` prints the hook block and the `claude mcp add` command to paste.

### Hooks: automatic status
`kowhai hook` is a subcommand of the same binary. Claude Code runs it on hook events and passes the event JSON on stdin. It looks for `.kowhai` in the project folder and its parents. It then calls `PUT /cards/{id}/links/claude_code/{session_id}`.

| Claude Code event | Link status | Extra |
|---|---|---|
| `SessionStart` | `working` | Records repo, branch and host. Prints the card title, description and open checklist items to stdout, which Claude Code adds to the session context. |
| `UserPromptSubmit` | `working` | |
| `Notification` | `waiting` | Stores the notification text, such as a permission request. Card members get an in-app notification. |
| `Stop` | `idle` | Claude finished its turn. |
| `SessionEnd` | `ended` | |

Rules for the hook command:
- It must never slow down or break Claude. It uses a 2 second timeout, logs failures to a local file and always exits 0.
- It does nothing in folders without a `.kowhai` file.
- A worker marks a link `stale` if it has said `working` or `waiting` with no update for 30 minutes. This catches crashed sessions.
- One card can have several sessions. The card shows the most urgent status: `waiting` before `working` before `idle`.

### Board behaviour
- Cards show a status badge with the session count, branch and time since last update.
- The card detail shows each session's history from the activity log.
- Board settings can map statuses to lists, for example moving the card to "Waiting on me" when a session asks for input. This is off by default.
- An "Agent projects" built-in board template has lists for Backlog, Working, Waiting on me, Review and Done, with the mapping turned on.

### MCP server
- Served at `/mcp` using the streamable HTTP transport. It uses bearer tokens, the same as the REST API.
- Add it to Claude Code with `claude mcp add --transport http kowhai https://<host>/mcp --header "Authorization: Bearer <token>"`.
- Each tool calls the service layer. Permissions, activity rows, SSE updates and search indexing all apply as normal.
- Read-only tools carry the `readOnlyHint` annotation, so clients can allow them without prompting.
- Tool results are compact JSON with IDs and short text. Long descriptions and comment threads are cut off, with a hint to fetch more.

| Tool | Purpose |
|---|---|
| `list_boards` | Boards the token can see |
| `get_board` | Lists and card summaries for one board |
| `search_cards` | Full-text search with the same filters as the UI |
| `get_card` | Full card: description, checklists, labels, recent comments, links |
| `create_card` | New card in a list, optionally from a card template |
| `update_card` | Title, description, labels, due date |
| `move_card` | Move to another list or position |
| `add_comment` | Comment on a card |
| `set_checklist_item` | Add, tick or untick a checklist item |

Nine tools in v1. More are added only when a real use needs them.

claude.ai web and desktop connectors need OAuth rather than a static header. That comes later. Kanidm or Authelia would issue the tokens, and Kōwhai would publish OAuth protected resource metadata.

## Frontend

| Need | Choice |
|---|---|
| Build | Vite + TypeScript |
| State | Pinia |
| Routing | Vue Router |
| API client | `openapi-typescript` + `openapi-fetch`, generated from the same spec |
| Drag and drop | `vue-draggable-plus` (SortableJS) |
| Styling | Tailwind CSS + Reka UI for accessible primitives |
| Markdown | `markdown-it` + DOMPurify |
| Dates | `date-fns` |
| Tests | Vitest for units, Playwright for end to end |

### Screens
- Workspace list and workspace home (boards grid, members, settings)
- Board view: horizontal lists, cards, drag and drop, filter bar, label and member chips
- Card detail modal: description, labels, members, due date, checklists, attachments, comments, activity, repeat settings
- New board dialog with template picker
- Board settings: labels, members, card templates, recurrences
- Search results
- Notifications panel
- Account settings: profile, notification preferences, API tokens (scope, workspace or board limit, actor label)
- Card status badges and a session list for cards linked to Claude Code
- Admin: users list, disable user

Moves and edits update the UI first, then call the API. On a `412` or error, the client rolls back and re-fetches.

## Deployment

### Compose services
- `kowhai`: the Go binary with the SPA embedded. Volume for the SQLite file.
- `garage`: single-node Garage. Volumes for data and metadata.
- `garage-init`: one-shot job. Sets the layout, creates the key and the `kowhai-attachments` and `kowhai-backups` buckets.
- `litestream`: sidecar, or run inside the `kowhai` container as a supervisor.
- Dev profile only: `authelia` as a local OIDC provider with a file-based user list.

TLS is left to the user's reverse proxy (Caddy, Traefik, nginx). The app reads `X-Forwarded-*` only from trusted proxies.

### Config (env vars)
```
KOWHAI_BASE_URL
KOWHAI_DB_PATH
KOWHAI_OIDC_ISSUER, KOWHAI_OIDC_CLIENT_ID, KOWHAI_OIDC_CLIENT_SECRET
KOWHAI_OIDC_ADMIN_GROUP (optional)
KOWHAI_S3_ENDPOINT, KOWHAI_S3_REGION, KOWHAI_S3_BUCKET, KOWHAI_S3_ACCESS_KEY, KOWHAI_S3_SECRET_KEY
KOWHAI_MAX_UPLOAD_MB
KOWHAI_TRUSTED_PROXIES
```

### Ops
- `/healthz` (process up) and `/readyz` (DB and S3 reachable).
- Prometheus metrics at `/metrics` on a separate port.
- Structured JSON logs.

## Testing

- Store and service tests use a real SQLite file in a temp directory.
- Blob tests use `gofakes3` in unit tests and Garage in CI integration tests.
- Auth unit tests use a mock OIDC provider (`oauth2-proxy/mockoidc` or similar). CI integration tests log in through real Kanidm and Authelia containers.
- Recurrence tests use a fake clock and cover daylight saving changes in `Australia/Sydney` and `Pacific/Auckland`.
- Permission matrix tests cover every role against every action.
- An API contract test checks that handlers match `openapi.yaml`.
- MCP tests run the official SDK client against the server in-process. They check each tool and that token limits apply.
- Hook tests feed recorded Claude Code event JSON to `kowhai hook`. They also check it exits 0 quickly when the server is down.
- Playwright covers login, board create, drag and drop, comments, attachments, and two-browser real-time sync.

## Milestones

1. **Skeleton.** Repo layout, config, migrations, OpenAPI pipeline, SPA embed, Compose with Garage and Authelia, CI.
2. **Auth.** OIDC login (Kanidm and Authelia), sessions, CSRF, user provisioning, PATs.
3. **Core boards.** Workspaces, members, invites, boards, lists, cards, ordering, permissions. Basic board UI with drag and drop.
4. **Card detail.** Description, labels, members, due dates, checklists, comments, archive.
5. **Activity and real-time.** Activity log, event hub, SSE, client patching.
6. **Claude Code integration.** Card links, token limits and actor labels, `kowhai link`, `kowhai hook`, status badges, stale worker, MCP server with the nine tools. `search_cards` matches titles only until milestone 8.
7. **Attachments.** Upload, download, delete, cleanup worker, card covers.
8. **Search.** FTS5 index, search UI, filters. Adds search to the MCP `search_cards` tool.
9. **Notifications.** Rules, in-app panel, due-date worker, "session waiting" notifications.
10. **Templates and recurrence.** Board templates (including "Agent projects"), card templates, recurring cards, worker.
11. **Hardening.** Rate limits, backups with Litestream, metrics, admin screen, API docs, end-to-end tests, release image.

Each milestone ends with a working, deployable build.

**After v1:** email notifications, sign-in restrictions, Trello import, OAuth for claude.ai connectors.
