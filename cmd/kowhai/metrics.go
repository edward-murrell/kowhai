package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"kowhai/internal/events"
)

// serveMetrics serves /metrics on its own port, away from the public one.
func serveMetrics(log *slog.Logger, addr string, hub *events.Hub) {
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "kowhai_sse_subscribers", Help: "Open server-sent event streams.",
	}, func() float64 { return float64(hub.Count()) }))
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("metrics listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Error("metrics server", "err", err)
	}
}
