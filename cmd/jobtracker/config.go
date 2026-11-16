package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyler180/jobtracker/internal/auth"
	"github.com/tyler180/jobtracker/internal/jobs"
	"github.com/tyler180/jobtracker/internal/postgres"
	"github.com/tyler180/jobtracker/internal/web"
)

func buildHandler(ctx context.Context, demo bool) (http.Handler, func(), error) {
	if demo {
		// Never open DATA_DIR or initialize PostgreSQL/OIDC in public demo mode.
		dir, err := os.MkdirTemp("", "jobtracker-demo-")
		if err != nil {
			return nil, func() {}, err
		}
		cleanup := func() { _ = os.RemoveAll(dir) }
		s, err := jobs.Open(dir)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		if err := jobs.SeedDemo(s); err != nil {
			cleanup()
			return nil, func() {}, err
		}
		return web.NewDemo(s), cleanup, nil
	}
	backend := env("STORAGE_BACKEND", "file")
	if backend != "file" && backend != "postgres" {
		return nil, func() {}, errors.New("STORAGE_BACKEND must be file or postgres")
	}
	if backend == "file" {
		for _, key := range []string{"DATABASE_URL", "OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "PUBLIC_URL"} {
			if os.Getenv(key) != "" {
				return nil, func() {}, errors.New("database/sign-in configuration requires STORAGE_BACKEND=postgres; refusing unauthenticated fallback")
			}
		}
		s, err := jobs.Open(env("DATA_DIR", "./data"))
		if err != nil {
			return nil, func() {}, err
		}
		if _, err := s.BackfillPay(); err != nil {
			return nil, func() {}, err
		}
		client := jobs.NewClient()
		return web.New(s, jobs.Importer{Client: client}), client.CloseIdleConnections, nil
	}
	if os.Getenv("DATABASE_URL") == "" {
		return nil, func() {}, errors.New("DATABASE_URL is required in postgres mode")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, func() {}, errors.New("invalid DATABASE_URL (details withheld)")
	}
	cfg.MaxConns = 10
	cfg.MinConns = 0
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startup, cfg)
	if err != nil {
		return nil, func() {}, errors.New("initialize PostgreSQL pool")
	}
	var version int
	if err := pool.QueryRow(startup, "SELECT max(version) FROM jobtracker.schema_versions").Scan(&version); err != nil || version != 2 {
		pool.Close()
		return nil, func() {}, errors.New("PostgreSQL schema version 2 required; run the explicit migration before starting")
	}
	signin, err := auth.New(startup, pool, auth.Config{Issuer: os.Getenv("OIDC_ISSUER_URL"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), PublicURL: os.Getenv("PUBLIC_URL")})
	if err != nil {
		pool.Close()
		return nil, func() {}, err
	}
	client := jobs.NewClient()
	cleanup := func() { client.CloseIdleConnections(); pool.Close() }
	return signin.Handler(web.NewWithStore(postgres.New(pool), jobs.Importer{Client: client})), cleanup, nil
}
