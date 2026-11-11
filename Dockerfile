# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY api /src/api
COPY web ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/kowhai ./cmd/kowhai

FROM litestream/litestream:0.5.18 AS litestream

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
 && adduser -D -H -u 10001 kowhai \
 && mkdir -p /data && chown kowhai:kowhai /data
COPY --from=litestream /usr/local/bin/litestream /usr/local/bin/litestream
COPY --from=build /out/kowhai /usr/local/bin/kowhai
COPY deploy/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY deploy/litestream.yml /etc/litestream.yml
USER kowhai
VOLUME /data
ENV KOWHAI_DB_PATH=/data/kowhai.db KOWHAI_LISTEN=:8080 KOWHAI_METRICS_LISTEN=:9090
EXPOSE 8080 9090
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["serve"]
