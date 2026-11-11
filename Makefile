# Kōwhai build tasks. Run `make help` for the list.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
E2E_PORT ?= 18080

.PHONY: help tools gen web build test test-go test-web e2e dev docker check-gen

help:
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

tools: ## Install sqlc and oapi-codegen into ./bin
	GOBIN=$(CURDIR)/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
	GOBIN=$(CURDIR)/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1

gen: ## Regenerate store, API and web client code
	bin/sqlc generate
	bin/oapi-codegen -config api/models.yaml api/openapi.yaml
	bin/oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd web && npm run gen

web: ## Build the web app into web/dist
	cd web && npm ci && npm run build

build: ## Build bin/kowhai with the web app embedded
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/kowhai ./cmd/kowhai

test: test-go test-web ## Run all unit and integration tests

test-go:
	go vet ./...
	go test ./...

test-web:
	cd web && npm run typecheck && npm test

e2e: build ## Run Playwright against a fresh dev server
	mkdir -p bin/e2e
	rm -f bin/e2e/kowhai.db bin/e2e/kowhai.db-wal bin/e2e/kowhai.db-shm
	KOWHAI_DEV_OIDC=1 KOWHAI_BASE_URL=http://localhost:$(E2E_PORT) KOWHAI_LISTEN=127.0.0.1:$(E2E_PORT) \
	  KOWHAI_METRICS_LISTEN= KOWHAI_DB_PATH=bin/e2e/kowhai.db KOWHAI_RATE_LIMIT_RPS=500 \
	  bin/kowhai serve > bin/e2e/server.log 2>&1 & echo $$! > bin/e2e/pid
	for i in $$(seq 1 50); do curl -sf http://localhost:$(E2E_PORT)/healthz >/dev/null && break; sleep 0.2; done
	cd web && KOWHAI_E2E_URL=http://localhost:$(E2E_PORT) npx playwright test; status=$$?; kill $$(cat ../bin/e2e/pid); exit $$status

dev: build ## Run the server with the built-in dev sign-in on :8080
	mkdir -p data
	KOWHAI_DEV_OIDC=1 KOWHAI_ADMIN_EMAILS=alice@example.com bin/kowhai serve

docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t kowhai:$(VERSION) .

check-gen: gen ## Fail if generated code is out of date
	git diff --exit-code -- internal/store internal/api internal/apitypes web/src/api
