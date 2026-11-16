// jobtracker-migrate validates backups by default; database writes are opt-in.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyler180/jobtracker/internal/migration"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	archive := flag.String("archive", "", "tar.gz backup path")
	sha := flag.String("sha256", "", "expected checksum from verified backup report")
	issuer := flag.String("issuer", "", "exact OIDC issuer URL (test identity for local rehearsal)")
	subject := flag.String("subject", "", "verified OIDC subject (test identity for local rehearsal)")
	apply := flag.Bool("apply", false, "write and verify import using MIGRATION_DATABASE_URL")
	schema := flag.Bool("apply-schema", false, "explicitly initialize schema in the target database")
	initOnly := flag.Bool("init-schema", false, "initialize database schema without an archive; requires -apply")
	flag.Parse()
	var a *migration.Archive
	var err error
	if *initOnly {
		if !*apply || *archive != "" || *sha != "" || *issuer != "" || *subject != "" || *schema {
			return fmt.Errorf("-init-schema requires -apply and no archive/identity flags")
		}
	} else {
		a, err = migration.Load(*archive, *sha)
		if err != nil {
			return err
		}
	}
	if !*apply {
		if *schema {
			return fmt.Errorf("-apply-schema requires -apply")
		}
		return json.NewEncoder(os.Stdout).Encode(a.Summary())
	}
	dsn := os.Getenv("MIGRATION_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("MIGRATION_DATABASE_URL must be set explicitly")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("could not connect to migration database (connection details withheld)")
	}
	defer conn.Close(context.Background())
	if *initOnly {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(cleanup)
		}()
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(749180142)"); err != nil {
			return err
		}
		if err := migration.InitializeSchema(ctx, tx); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]int{"schema_version": 2})
	}
	r, err := migration.Apply(ctx, conn, a, migration.Identity{Issuer: *issuer, Subject: *subject}, *schema)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}
