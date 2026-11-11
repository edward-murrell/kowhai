package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"kowhai/internal/auth"
	"kowhai/internal/auth/oidctest"
	"kowhai/internal/blob"
	"kowhai/internal/config"
	"kowhai/internal/events"
	"kowhai/internal/server"
	"kowhai/internal/service"
	"kowhai/internal/store"
	"kowhai/internal/worker"
)

func newLogger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var bs *blob.S3
	var blobStore service.Blob
	if cfg.S3Endpoint != "" {
		bs, err = blob.New(blob.Config{Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket, AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey})
		if err != nil {
			return err
		}
		blobStore = bs
	} else {
		log.Warn("KOWHAI_S3_ENDPOINT is not set. Attachments are turned off.")
	}

	hub := events.NewHub()
	svc := service.New(db, hub, blobStore, service.Config{
		MaxUploadBytes: int64(cfg.MaxUploadMB) << 20,
		AdminEmails:    cfg.AdminEmailList(),
		AdminGroup:     cfg.OIDCAdminGroup,
	}, log)

	extra := map[string]http.Handler{}
	authCfg := auth.Config{BaseURL: cfg.BaseURL, Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret}
	if cfg.OIDCCAFile != "" {
		pem, err := os.ReadFile(cfg.OIDCCAFile)
		if err != nil {
			return fmt.Errorf("KOWHAI_OIDC_CA_FILE: %w", err)
		}
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		pool.AppendCertsFromPEM(pem)
		authCfg.HTTPClient = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	}
	if cfg.DevOIDC {
		log.Warn("KOWHAI_DEV_OIDC is on. Anyone can sign in as a test user. Never use this in production.")
		issuer := strings.TrimRight(cfg.BaseURL, "/") + "/dev-oidc"
		p, err := oidctest.New(issuer, "kowhai-dev", "dev-secret", "ES256", devUsers())
		if err != nil {
			return err
		}
		extra["/dev-oidc/"] = p
		authCfg.Issuer, authCfg.ClientID, authCfg.ClientSecret = issuer, "kowhai-dev", "dev-secret"
		authCfg.HTTPClient = &http.Client{Transport: localTransport{h: p}}
	}
	au, err := auth.New(authCfg, svc, log)
	if err != nil {
		return err
	}
	go func() {
		if err := au.Discover(ctx); err != nil {
			log.Warn("OIDC discovery failed. Will retry on the next sign-in.", "err", err)
		}
	}()
	proxies, _ := cfg.Proxies()
	ready := map[string]server.Pinger{"db": db}
	if bs != nil {
		ready["s3"] = bs
	}
	h := server.Handler(server.Options{
		Service: svc, Hub: hub, Auth: au, Log: log, BaseURL: cfg.BaseURL, Version: version,
		Proxies: proxies, RateLimit: cfg.RateLimitRPS, Ready: ready, Extra: extra,
	})

	srv := &http.Server{
		Addr: cfg.Listen, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
	if cfg.MetricsListen != "" {
		go serveMetrics(log, cfg.MetricsListen, hub)
	}

	var backup func(context.Context) error
	if cfg.BackupDir != "" || cfg.S3BackupBucket != "" {
		backup = backupJob(log, db, cfg, bs)
	}
	go worker.Run(ctx, log, worker.Jobs(svc, backup))

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "base_url", cfg.BaseURL, "version", version)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

func devUsers() []oidctest.User {
	return []oidctest.User{
		{Subject: "dev-alice", Email: "alice@example.com", Name: "Alice Admin", Groups: []string{"kowhai-admins"}},
		{Subject: "dev-bob", Email: "bob@example.com", Name: "Bob Builder"},
		{Subject: "dev-carol", Email: "carol@example.com", Name: "Carol Viewer"},
	}
}

// localTransport sends the server's own calls to the dev provider
// straight to its handler.
type localTransport struct{ h http.Handler }

func (t localTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := newRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.result(r), nil
}

