-- +goose Up
-- All IDs are ULIDs stored as text. All timestamps are written by the
-- application in UTC.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    oidc_issuer   TEXT NOT NULL,
    oidc_subject  TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    display_name  TEXT NOT NULL DEFAULT '',
    avatar_url    TEXT NOT NULL DEFAULT '',
    groups        TEXT NOT NULL DEFAULT '[]',
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    disabled_at   DATETIME,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (oidc_issuer, oidc_subject)
);
CREATE INDEX users_email ON users (email);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY, -- SHA-256 of the cookie value
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at    DATETIME NOT NULL,
    last_seen_at  DATETIME NOT NULL,
    user_agent    TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);

-- Short-lived state for the OIDC authorisation code flow.
CREATE TABLE oidc_states (
    state          TEXT PRIMARY KEY,
    nonce          TEXT NOT NULL,
    code_verifier  TEXT NOT NULL,
    return_to      TEXT NOT NULL DEFAULT '/',
    expires_at     DATETIME NOT NULL,
    created_at     DATETIME NOT NULL
);

CREATE TABLE workspaces (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    slug         TEXT NOT NULL UNIQUE,
    description  TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL REFERENCES users (id),
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE workspace_members (
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role          TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member', 'viewer')),
    created_at    DATETIME NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX workspace_members_user ON workspace_members (user_id);

CREATE TABLE workspace_invites (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    invited_by    TEXT NOT NULL REFERENCES users (id),
    expires_at    DATETIME NOT NULL,
    created_at    DATETIME NOT NULL,
    UNIQUE (workspace_id, email)
);
CREATE INDEX workspace_invites_email ON workspace_invites (email);

CREATE TABLE boards (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    visibility    TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('private', 'workspace')),
    background    TEXT NOT NULL DEFAULT '',
    is_template   BOOLEAN NOT NULL DEFAULT FALSE,
    -- JSON. Holds the Claude Code status to list mapping.
    settings      TEXT NOT NULL DEFAULT '{}',
    archived_at   DATETIME,
    created_by    TEXT NOT NULL REFERENCES users (id),
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX boards_workspace ON boards (workspace_id);

CREATE TABLE board_members (
    board_id    TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    created_at  DATETIME NOT NULL,
    PRIMARY KEY (board_id, user_id)
);
CREATE INDEX board_members_user ON board_members (user_id);

CREATE TABLE api_tokens (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,
    prefix        TEXT NOT NULL,
    scopes        TEXT NOT NULL, -- comma separated: read, write
    workspace_id  TEXT REFERENCES workspaces (id) ON DELETE CASCADE,
    board_id      TEXT REFERENCES boards (id) ON DELETE CASCADE,
    actor_label   TEXT NOT NULL DEFAULT '',
    expires_at    DATETIME,
    last_used_at  DATETIME,
    revoked_at    DATETIME,
    created_at    DATETIME NOT NULL
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);

CREATE TABLE lists (
    id           TEXT PRIMARY KEY,
    board_id     TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    position     TEXT NOT NULL,
    archived_at  DATETIME,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX lists_board ON lists (board_id, position);

CREATE TABLE cards (
    id                    TEXT PRIMARY KEY,
    list_id               TEXT NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    board_id              TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    title                 TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    position              TEXT NOT NULL,
    due_at                DATETIME,
    due_done              BOOLEAN NOT NULL DEFAULT FALSE,
    start_at              DATETIME,
    cover_attachment_id   TEXT,
    is_template           BOOLEAN NOT NULL DEFAULT FALSE,
    recurrence_id         TEXT,
    archived_at           DATETIME,
    -- Set by the due-date worker so each reminder is sent once.
    -- Cleared when due_at changes.
    due_soon_notified_at  DATETIME,
    overdue_notified_at   DATETIME,
    created_by            TEXT NOT NULL REFERENCES users (id),
    created_at            DATETIME NOT NULL,
    updated_at            DATETIME NOT NULL,
    version               INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX cards_list ON cards (list_id, position);
CREATE INDEX cards_board ON cards (board_id);
CREATE INDEX cards_due ON cards (due_at) WHERE due_at IS NOT NULL AND due_done = FALSE AND archived_at IS NULL;

CREATE TABLE card_members (
    card_id     TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at  DATETIME NOT NULL,
    PRIMARY KEY (card_id, user_id)
);
CREATE INDEX card_members_user ON card_members (user_id);

CREATE TABLE card_watchers (
    card_id     TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at  DATETIME NOT NULL,
    PRIMARY KEY (card_id, user_id)
);

CREATE TABLE labels (
    id          TEXT PRIMARY KEY,
    board_id    TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    colour      TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX labels_board ON labels (board_id);

CREATE TABLE card_labels (
    card_id   TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    label_id  TEXT NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
CREATE INDEX card_labels_label ON card_labels (label_id);

CREATE TABLE checklists (
    id          TEXT PRIMARY KEY,
    card_id     TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    position    TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX checklists_card ON checklists (card_id, position);

CREATE TABLE checklist_items (
    id            TEXT PRIMARY KEY,
    checklist_id  TEXT NOT NULL REFERENCES checklists (id) ON DELETE CASCADE,
    card_id       TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    text          TEXT NOT NULL,
    done          BOOLEAN NOT NULL DEFAULT FALSE,
    position      TEXT NOT NULL,
    assignee_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    due_at        DATETIME,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX checklist_items_checklist ON checklist_items (checklist_id, position);
CREATE INDEX checklist_items_card ON checklist_items (card_id);

CREATE TABLE comments (
    id          TEXT PRIMARY KEY,
    card_id     TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    author_id   TEXT NOT NULL REFERENCES users (id),
    token_id    TEXT,
    actor_label TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    edited_at   DATETIME,
    deleted_at  DATETIME,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX comments_card ON comments (card_id, id);

CREATE TABLE attachments (
    id            TEXT PRIMARY KEY,
    card_id       TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    board_id      TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    uploader_id   TEXT NOT NULL REFERENCES users (id),
    object_key    TEXT NOT NULL,
    filename      TEXT NOT NULL,
    content_type  TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL,
    sha256        TEXT NOT NULL,
    created_at    DATETIME NOT NULL
);
CREATE INDEX attachments_card ON attachments (card_id);

-- Objects waiting for the cleanup worker to remove them from S3.
CREATE TABLE blob_deletions (
    object_key  TEXT PRIMARY KEY,
    attempts    INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL
);

CREATE TABLE card_links (
    id             TEXT PRIMARY KEY,
    card_id        TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    board_id       TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('claude_code', 'url')),
    external_key   TEXT NOT NULL,
    title          TEXT NOT NULL DEFAULT '',
    repo           TEXT NOT NULL DEFAULT '',
    branch         TEXT NOT NULL DEFAULT '',
    host           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT '' CHECK (status IN ('', 'working', 'waiting', 'idle', 'ended', 'stale')),
    status_detail  TEXT NOT NULL DEFAULT '',
    last_seen_at   DATETIME,
    meta           TEXT NOT NULL DEFAULT '{}',
    created_by     TEXT NOT NULL REFERENCES users (id),
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    UNIQUE (kind, external_key)
);
CREATE INDEX card_links_card ON card_links (card_id);
CREATE INDEX card_links_active ON card_links (last_seen_at) WHERE status IN ('working', 'waiting');

CREATE TABLE card_recurrences (
    id                  TEXT PRIMARY KEY,
    board_id            TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    template_card_id    TEXT REFERENCES cards (id) ON DELETE SET NULL,
    target_list_id      TEXT NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    rrule               TEXT NOT NULL,
    -- Local wall-clock start time, e.g. 2026-11-16T09:00:00, read in timezone.
    dtstart             TEXT NOT NULL,
    timezone            TEXT NOT NULL,
    due_offset_minutes  INTEGER,
    next_run_at         DATETIME,
    last_run_at         DATETIME,
    paused_at           DATETIME,
    created_by          TEXT NOT NULL REFERENCES users (id),
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL,
    version             INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX card_recurrences_board ON card_recurrences (board_id);
CREATE INDEX card_recurrences_next ON card_recurrences (next_run_at) WHERE paused_at IS NULL;

CREATE TABLE activities (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    board_id      TEXT REFERENCES boards (id) ON DELETE CASCADE,
    card_id       TEXT,
    actor_id      TEXT REFERENCES users (id),
    token_id      TEXT,
    actor_label   TEXT NOT NULL DEFAULT '',
    type          TEXT NOT NULL,
    payload       TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL
);
CREATE INDEX activities_board ON activities (board_id, id);
CREATE INDEX activities_card ON activities (card_id, id);
CREATE INDEX activities_workspace ON activities (workspace_id, id);

CREATE TABLE notifications (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    activity_id  TEXT NOT NULL REFERENCES activities (id) ON DELETE CASCADE,
    read_at      DATETIME,
    created_at   DATETIME NOT NULL
);
CREATE INDEX notifications_user ON notifications (user_id, id);
CREATE INDEX notifications_unread ON notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE notification_prefs (
    user_id     TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    settings    TEXT NOT NULL DEFAULT '{}',
    updated_at  DATETIME NOT NULL
);

-- +goose Down
DROP TABLE notification_prefs;
DROP TABLE notifications;
DROP TABLE activities;
DROP TABLE card_recurrences;
DROP TABLE card_links;
DROP TABLE blob_deletions;
DROP TABLE attachments;
DROP TABLE comments;
DROP TABLE checklist_items;
DROP TABLE checklists;
DROP TABLE card_labels;
DROP TABLE labels;
DROP TABLE card_watchers;
DROP TABLE card_members;
DROP TABLE cards;
DROP TABLE lists;
DROP TABLE api_tokens;
DROP TABLE board_members;
DROP TABLE boards;
DROP TABLE workspace_invites;
DROP TABLE workspace_members;
DROP TABLE workspaces;
DROP TABLE oidc_states;
DROP TABLE sessions;
DROP TABLE users;
