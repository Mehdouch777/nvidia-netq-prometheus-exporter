package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"repos.apps.bluvalt.com/BluvaltCloud/netq-exporter/internal/config"
	"repos.apps.bluvalt.com/BluvaltCloud/netq-exporter/internal/exporter"
	"repos.apps.bluvalt.com/BluvaltCloud/netq-exporter/internal/netq"
)

func main() {
	// Build the exporter from environment-backed config and expose a dedicated
	// registry so the surface area stays limited to Go/process metrics plus the
	// NetQ collector itself.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	client := netq.NewClient(cfg.NetQ)
	exp := exporter.New(client, cfg.PollInterval, time.Now)

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		prometheus.NewGoCollector(),
		exp,
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", exp.HealthHandler)
	mux.HandleFunc("/readyz", exp.ReadyHandler)

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		// The exporter owns its own polling loop and updates in-memory gauges that
		// the HTTP handler exposes on demand.
		if err := exp.Run(ctx); err != nil {
			log.Printf("exporter stopped with error: %v", err)
			stop()
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
	}()

	log.Printf(
		"starting netq-exporter host=%s poll_interval=%s timeout=%s listen_address=%s insecure_skip_verify=%t",
		cfg.NetQ.Host,
		cfg.PollInterval,
		cfg.NetQ.Timeout,
		cfg.ListenAddress,
		cfg.NetQ.InsecureSkipVerify,
	)
	log.Printf("exporter ready to receive requests on /metrics via %s", cfg.ListenAddress)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}
