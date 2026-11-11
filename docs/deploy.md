# Deploying Kōwhai

Kōwhai runs as one container next to Garage. Run one instance only. SQLite and the live update hub both assume a single process.

## 1. Register an OIDC client

The redirect URI is `<KOWHAI_BASE_URL>/auth/callback`. Kōwhai asks for the scopes `openid profile email groups` and uses PKCE with S256.

**Kanidm**

```sh
kanidm system oauth2 create kowhai Kōwhai https://kowhai.example.com
kanidm system oauth2 add-redirect-url kowhai https://kowhai.example.com/auth/callback
kanidm system oauth2 update-scope-map kowhai idm_all_persons openid profile email groups
kanidm system oauth2 show-basic-secret kowhai
```

The issuer is `https://<kanidm-host>/oauth2/openid/kowhai`. Kanidm signs with ES256 and has no logout endpoint, so signing out of Kōwhai does not sign you out of Kanidm.

**Authelia**

Add a client with `require_pkce: true`, `pkce_challenge_method: S256` and the `groups` scope. `deploy/authelia/configuration.yml` is a working example.

## 2. Configure

```sh
cd deploy
cp .env.example .env
```

The example file already holds fresh Garage secrets. Set the base URL, the OIDC values and your proxy's address range.

| Variable | Meaning |
|---|---|
| `KOWHAI_BASE_URL` | Public URL, for example `https://kowhai.example.com` |
| `KOWHAI_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET` | The OIDC client |
| `KOWHAI_OIDC_ADMIN_GROUP` | Members of this group are site admins |
| `KOWHAI_ADMIN_EMAILS` | These people are always site admins |
| `KOWHAI_TRUSTED_PROXIES` | IPs or CIDRs allowed to set `X-Forwarded-For` |
| `KOWHAI_MAX_UPLOAD_MB` | Attachment size limit. The default is 25 |
| `KOWHAI_S3_*` | Garage or other S3 store. Compose sets the endpoint for you |
| `KOWHAI_RATE_LIMIT_RPS` | Requests per second per user or token. The default is 20 |

Settings can also come from a YAML file named by `KOWHAI_CONFIG`. Use the variable name in lower case without the `KOWHAI_` prefix as the key. Environment variables win.

## 3. Start

```sh
docker compose up -d
```

`garage-init` sets up the Garage layout, the access key and the `kowhai-attachments` and `kowhai-backups` buckets. It is safe to run again.

Point your reverse proxy at port 8080. Kōwhai serves plain HTTP and expects the proxy to handle TLS. Live updates use server-sent events, so turn off response buffering for `/api/v1/*/events` if your proxy buffers.

## Backups

Litestream runs inside the Kōwhai container. It replicates every change to `kowhai-backups/litestream/`. On a fresh volume it restores from there before starting.

Once a day Kōwhai also writes a `VACUUM INTO` snapshot to `/data/backups` and uploads it to `kowhai-backups/snapshots/`. The last seven are kept on disk.

To restore a snapshot by hand, stop Kōwhai, copy the snapshot over `/data/kowhai.db`, delete `kowhai.db-wal` and `kowhai.db-shm`, and start it again.

## Health and metrics

- `/healthz` answers when the process is up.
- `/readyz` checks the database and S3.
- Prometheus metrics are on port 9090 at `/metrics`. Keep that port private.
- Logs are JSON on stdout.

## Admin

Site admins see an Admin page in the account menu. They can disable accounts. A disabled user is signed out and their tokens stop working. Site admins get no extra access to boards.
