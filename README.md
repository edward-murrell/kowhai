# Kōwhai

[![ci](https://github.com/edward-murrell/kowhai/actions/workflows/ci.yml/badge.svg)](https://github.com/edward-murrell/kowhai/actions/workflows/ci.yml)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)

Kōwhai is a self-hosted board app for teams of up to 1000 people. It has workspaces, boards, lists and cards, with live updates, search, notifications, templates and repeating cards. Claude Code can read and update cards through an MCP server, and a hook keeps each card's session status up to date.

It runs as one Go binary with the web app built in. Data lives in SQLite. Attachments and backups go to Garage or any S3-compatible store. People sign in with OIDC, tested against Kanidm and Authelia.

## Install

Releases include binaries for Linux and macOS, and a container image:

```sh
docker pull ghcr.io/edward-murrell/kowhai:1
```

To get only the `kowhai link` and `kowhai hook` commands for Claude Code:

```sh
go install github.com/edward-murrell/kowhai/cmd/kowhai@latest
```

A binary built this way has no web app inside, so use a release build to run the server.

## Quick start

To try it on your machine with the built-in test sign-in:

```sh
make web build
make dev
```

Open http://localhost:8080 and pick a test user. The dev sign-in lets anyone in, so never turn it on in production. Attachments are off unless an S3 store is configured.

To run the full stack with Garage and a local Authelia:

```sh
cd deploy
cp .env.dev.example .env
docker compose --profile dev up -d --build
```

Sign in as `alice` or `bob` with the password `kowhai`. Authelia uses a self-signed certificate, so your browser warns once.

## Production

See [docs/deploy.md](docs/deploy.md). In short, copy `deploy/.env.example` to `deploy/.env`, fill it in, register an OIDC client, and run `docker compose up -d` in `deploy/`. Put a reverse proxy in front for TLS.

## Claude Code

See [docs/claude-code.md](docs/claude-code.md). In a project folder:

```sh
kowhai link                 # pick or create a card
kowhai link --print-config  # prints the hooks and the `claude mcp add` command
```

## Development

| Task | Command |
|---|---|
| Install code generators | `make tools` |
| Regenerate code after changing SQL or `api/openapi.yaml` | `make gen` |
| Go and web tests | `make test` |
| Browser tests (Playwright) | `make e2e` |
| Container image | `make docker` |

The API contract is `api/openapi.yaml`. Server stubs, Go models and the TypeScript client are all generated from it. Database queries live in `internal/store/queries` and go through `sqlc`. Migrations run at start-up.

The Go tests start a full server in-process with a test OIDC provider. Every API call in those tests is checked against the OpenAPI spec.

API docs are served at `/api/docs`.

## Layout

```
cmd/kowhai/          main: serve, hook and link commands
api/openapi.yaml     the API contract
internal/api/        generated server code and handlers
internal/auth/       OIDC, sessions, CSRF and tokens
internal/service/    business rules and permission checks
internal/store/      SQLite, migrations and sqlc queries
internal/mcp/        MCP server
internal/hook/       Claude Code hook and link commands
web/                 Vue app
deploy/              Compose, Garage, Authelia and CI checks
```

## Licence

Kōwhai is free software under the [GNU General Public License version 3](LICENSE). See [CONTRIBUTING.md](CONTRIBUTING.md) to help and [SECURITY.md](SECURITY.md) to report a security problem.
