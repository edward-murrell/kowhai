# Contributing

Thanks for helping with Kōwhai.

## Before you start

Open an issue for anything larger than a small fix, so we can agree on the approach first.

## Set up

You need Go (the version in `go.mod`), Node 22 and Docker for the compose tests.

```sh
make tools        # sqlc and oapi-codegen
cd web && npm ci && cd ..
make web build
make dev          # http://localhost:8080 with test users
```

## Changing things

- API changes start in `api/openapi.yaml`. Run `make gen` afterwards.
- Database changes need a new migration in `internal/store/migrations` and queries in `internal/store/queries`. Run `make gen`.
- Permission decisions belong in `service.Authorize`. Handlers never check roles.
- Every write records an activity row in the same transaction.

## Tests

```sh
make test   # Go and web tests
make e2e    # Playwright against a dev server
```

Add tests for new behaviour. CI also runs `make check-gen`, which fails if generated code is out of date.

## Licence

Kōwhai is licensed under the GNU General Public License version 3. By sending a contribution you agree to license it under the same terms.
