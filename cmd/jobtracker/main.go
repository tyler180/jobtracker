package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tyler180/jobtracker/internal/jobs"
	"github.com/tyler180/jobtracker/internal/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	demo := env("DEMO_MODE", "false")
	if demo != "true" && demo != "false" {
		slog.Error("DEMO_MODE must be true or false")
		os.Exit(1)
	}
	dataDir := env("DATA_DIR", "./data")
	if demo == "true" {
		// Never open DATA_DIR in demo mode, even if misconfigured to point at a private archive.
		var err error
		dataDir, err = os.MkdirTemp("", "jobtracker-demo-")
		if err != nil {
			slog.Error("create demo archive", "error", err)
			os.Exit(1)
		}
		defer os.RemoveAll(dataDir)
	}
	store, err := jobs.Open(dataDir)
	if err != nil {
		slog.Error("open archive", "error", err)
		os.Exit(1)
	}
	if demo == "true" {
		if err := jobs.SeedDemo(store); err != nil {
			slog.Error("seed demo", "error", err)
			os.Exit(1)
		}
	}
	updated, err := store.BackfillPay()
	if err != nil {
		slog.Error("backfill archived pay", "updated", updated, "error", err)
		os.Exit(1)
	}
	slog.Info("archived pay backfill complete", "updated", updated)
	client := jobs.NewClient()
	defer client.CloseIdleConnections()
	handler := web.New(store, jobs.Importer{Client: client})
	if demo == "true" {
		handler = web.NewDemo(store)
	}
	server := &http.Server{Addr: env("LISTEN_ADDR", "127.0.0.1:8080"), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { slog.Info("job tracker listening", "address", server.Addr); done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("shutdown failed", "error", err)
			server.Close()
		}
	}
}
