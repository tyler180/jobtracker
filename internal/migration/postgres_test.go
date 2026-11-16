package migration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This test intentionally creates/drops jobtracker schema in a disposable local
// database only. It never accepts a NAS or production connection string.
func TestPostgresMigration(t *testing.T) {
	dsn := os.Getenv("JOBTRACKER_MIGRATION_TEST_URL")
	if dsn == "" {
		t.Skip("set JOBTRACKER_MIGRATION_TEST_URL for PostgreSQL integration tests")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if (cfg.Host != "127.0.0.1" && cfg.Host != "localhost" && cfg.Host != "::1") || !strings.HasSuffix(cfg.Database, "_test") {
		t.Fatal("integration tests require loopback host and database ending in _test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect to local test database")
	}
	defer conn.Close(context.Background())
	reset := func() {
		t.Helper()
		if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS jobtracker CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	alice := Identity{"https://identity.example.test/application/o/jobtracker/", "test-owner-a"}
	bob := Identity{alice.Issuer, "test-owner-b"}
	a := loadFixture(t, jobFile("2", ""), jobFile("3", ""))
	r, err := Apply(ctx, conn, a, alice, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inserted != 2 || r.Verified != 2 {
		t.Fatalf("first import: %+v", r)
	}
	r, err = Apply(ctx, conn, a, alice, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inserted != 0 || r.Unchanged != 2 || r.Verified != 2 {
		t.Fatalf("rerun: %+v", r)
	}
	if _, err := Apply(ctx, conn, a, bob, false); err == nil {
		t.Fatal("allowed rebinding archive to a different user")
	}
	var users int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM jobtracker.users").Scan(&users); err != nil || users != 1 {
		t.Fatal("failed import left a user behind", err)
	}
	// Insert sorts before conflict: proves earlier successful inserts roll back.
	conflict := loadFixture(t, jobFile("1", ""), jobFile("2", `,"new_field":"conflict"`))
	if _, err := Apply(ctx, conn, conflict, alice, false); err == nil {
		t.Fatal("conflicting import succeeded")
	}
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM jobtracker.jobs").Scan(&count); err != nil || count != 2 {
		t.Fatal("partial import survived rollback", err)
	}
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM jobtracker.archive_imports").Scan(&count); err != nil || count != 1 {
		t.Fatal("failed import recorded as completed", err)
	}
	var aliceID string
	if err := conn.QueryRow(ctx, "SELECT id::text FROM jobtracker.users WHERE subject=$1", alice.Subject).Scan(&aliceID); err != nil {
		t.Fatal(err)
	}
	bobID := "00000000-0000-4000-8000-000000000002"
	if _, err := conn.Exec(ctx, "INSERT INTO jobtracker.users(id,issuer,subject) VALUES($1,$2,$3)", bobID, bob.Issuer, bob.Subject); err != nil {
		t.Fatal(err)
	}
	repo := NewRecords(conn)
	checkIsolation(t, ctx, repo, aliceID, bobID, a)
	// Different users may independently save the same posting ID.
	b := loadFixture(t, jobFile("2", ""), testFile{name: ".pay-backfill-v1", body: "done"})
	if _, err := Apply(ctx, conn, b, bob, false); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.List(ctx, bobID); err != nil || len(rows) != 1 {
		t.Fatal("second user cannot own same posting ID", err)
	}
	changed, err := repo.UpdateApplication(ctx, bobID, a.records[0].id, json.RawMessage(`{"general_notes":"Bob only"}`))
	if err != nil || !changed {
		t.Fatal("owner update failed", err)
	}
	actual, err := repo.Get(ctx, aliceID, a.records[0].id)
	if err != nil || !sameJSON(actual, a.records[0].raw) {
		t.Fatal("other user's update changed original owner", err)
	}
	// Never overwrite a user's later edits with an old backup.
	if _, err := Apply(ctx, conn, b, bob, false); err == nil {
		t.Fatal("stale backup overwrote application edits")
	}
	changed, err = repo.Delete(ctx, bobID, a.records[0].id)
	if err != nil || !changed {
		t.Fatal("owner delete failed", err)
	}
	if _, err := repo.Get(ctx, aliceID, a.records[0].id); err != nil {
		t.Fatal("other user's delete removed original owner", err)
	}
	if _, err := Apply(ctx, conn, b, bob, false); err == nil {
		t.Fatal("rerun resurrected deleted data")
	}
	if rows, err := repo.List(ctx, bobID); err != nil || len(rows) != 0 {
		t.Fatal("deleted data resurrected", err)
	}
	t.Log("passed exact import, idempotency, owner binding, transaction rollback, scoped list/read/update/delete, shared posting IDs, stale-backup protection")

	if path := os.Getenv("JOBTRACKER_MIGRATION_BACKUP"); path != "" {
		reset()
		real, err := Load(path, os.Getenv("JOBTRACKER_MIGRATION_BACKUP_SHA256"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := Apply(ctx, conn, real, alice, true)
		if err != nil {
			t.Fatal(err)
		}
		if r.Verified != real.Summary().Records || r.Inserted != r.Verified {
			t.Fatal("real archive incomplete")
		}
		r2, err := Apply(ctx, conn, real, alice, false)
		if err != nil || r2.Inserted != 0 || r2.Unchanged != r.Verified {
			t.Fatal("real archive rerun failed", err)
		}
		if err := conn.QueryRow(ctx, "SELECT id::text FROM jobtracker.users WHERE subject=$1", alice.Subject).Scan(&aliceID); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, "INSERT INTO jobtracker.users(id,issuer,subject) VALUES($1,$2,$3)", bobID, bob.Issuer, bob.Subject); err != nil {
			t.Fatal(err)
		}
		checkIsolation(t, ctx, repo, aliceID, bobID, real)
		t.Logf("REAL BACKUP: %d records verified byte-for-byte and field-for-field; rerun inserted 0; second user cannot list/read/edit/delete any of the %d records", r.Verified, r.Verified)
	}
}

func checkIsolation(t *testing.T, ctx context.Context, repo *Records, aliceID, bobID string, a *Archive) {
	t.Helper()
	rows, err := repo.List(ctx, aliceID)
	if err != nil || len(rows) != len(a.records) {
		t.Fatal("owner list incomplete", err)
	}
	rows, err = repo.List(ctx, bobID)
	if err != nil || len(rows) != 0 {
		t.Fatal("list exposed another owner's records", err)
	}
	for _, rec := range a.records {
		actual, err := repo.Get(ctx, aliceID, rec.id)
		if err != nil || !sameJSON(rec.raw, actual) {
			t.Fatal("owner data differs", err)
		}
		if _, err := repo.Get(ctx, bobID, rec.id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("cross-owner read allowed", err)
		}
		if changed, err := repo.UpdateApplication(ctx, bobID, rec.id, json.RawMessage(`{"general_notes":"intrusion"}`)); err != nil || changed {
			t.Fatal("cross-owner edit allowed", err)
		}
		if changed, err := repo.Delete(ctx, bobID, rec.id); err != nil || changed {
			t.Fatal("cross-owner delete allowed", err)
		}
	}
}
