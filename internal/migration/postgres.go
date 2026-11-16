package migration

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed schema.sql
var schemaSQL string

//go:embed schema_auth.sql
var authSchemaSQL string

// InitializeSchema applies reviewed schema versions explicitly in a transaction.
// It is used by the migration command, never by the web server.
func InitializeSchema(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, schemaSQL); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, authSchemaSQL)
	return err
}

// Identity must come from the intended OIDC provider and verified login subject.
// An Authentik numeric admin user ID is not necessarily its OIDC subject.
type Identity struct {
	Issuer  string
	Subject string
}

func (i Identity) validate() error {
	u, err := url.Parse(i.Issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(i.Subject) == "" {
		return errors.New("owner requires an HTTPS OIDC issuer and nonblank verified subject")
	}
	return nil
}

// Result reports an import verified inside the same transaction before commit.
type Result struct {
	Summary
	Inserted  int `json:"inserted"`
	Unchanged int `json:"unchanged"`
	Verified  int `json:"verified"`
}

// Apply imports an archive atomically. Schema changes require explicit opt-in.
// Conflicts fail without overwriting; a failed/ambiguous commit is returned as an
// error. Re-running the same backup/identity reconciles a possibly committed run.
func Apply(ctx context.Context, conn *pgx.Conn, a *Archive, owner Identity, applySchema bool) (Result, error) {
	r := Result{Summary: a.Summary()}
	if err := owner.validate(); err != nil {
		return r, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// Serialize this migration tool's runs, including schema initialization.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(749180142)"); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return r, err
	}
	if applySchema {
		if err = InitializeSchema(ctx, tx); err != nil {
			return r, fmt.Errorf("apply schema: %w", err)
		}
	}
	var version int
	if err = tx.QueryRow(ctx, "SELECT max(version) FROM jobtracker.schema_versions").Scan(&version); err != nil || (version != 1 && version != 2) {
		return r, errors.New("expected Jobtracker schema version 1 or 2; initialize explicitly with -apply-schema")
	}
	userID := fmt.Sprintf("%x", uuidBytes())
	if _, err = tx.Exec(ctx, `INSERT INTO jobtracker.users (id, issuer, subject) VALUES ($1,$2,$3) ON CONFLICT (issuer, subject) DO NOTHING`, userID, owner.Issuer, owner.Subject); err != nil {
		return r, err
	}
	if err = tx.QueryRow(ctx, `SELECT id::text FROM jobtracker.users WHERE issuer=$1 AND subject=$2`, owner.Issuer, owner.Subject).Scan(&userID); err != nil {
		return r, err
	}
	var priorOwner string
	err = tx.QueryRow(ctx, `SELECT owner_id::text FROM jobtracker.archive_imports WHERE archive_sha256=$1`, a.sha256).Scan(&priorOwner)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return r, err
	}
	if err == nil && priorOwner != userID {
		return r, errors.New("backup was already imported for a different owner")
	}
	completed := err == nil
	for _, rec := range a.records {
		if !completed {
			tag, err := tx.Exec(ctx, `INSERT INTO jobtracker.jobs (owner_id,id,document,source_json) VALUES ($1,$2,$3::jsonb,$4) ON CONFLICT (owner_id,id) DO NOTHING`, userID, rec.id, string(rec.raw), rec.raw)
			if err != nil {
				return r, fmt.Errorf("import record: %w", err)
			}
			if tag.RowsAffected() == 1 {
				r.Inserted++
			} else {
				r.Unchanged++
			}
		} else {
			r.Unchanged++
		}
		var raw, doc []byte
		if err := tx.QueryRow(ctx, `SELECT source_json,document FROM jobtracker.jobs WHERE owner_id=$1 AND id=$2 FOR UPDATE`, userID, rec.id).Scan(&raw, &doc); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return r, errors.New("previously imported record is missing; refusing to resurrect deleted data")
			}
			return r, err
		}
		if !bytes.Equal(raw, rec.raw) || !sameJSON(rec.raw, doc) {
			return r, errors.New("existing record conflicts with backup; import rolled back without overwriting")
		}
		r.Verified++
	}
	if _, err = tx.Exec(ctx, `INSERT INTO jobtracker.archive_imports (archive_sha256,owner_id,record_count) VALUES ($1,$2,$3) ON CONFLICT (archive_sha256) DO NOTHING`, a.sha256, userID, len(a.records)); err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, fmt.Errorf("commit outcome may be unknown; rerun the same backup and identity to reconcile: %w", err)
	}
	return r, nil
}

func uuidBytes() []byte {
	// crypto/rand.Read never returns an error with supported Go versions.
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return b
}

func sameJSON(a, b []byte) bool {
	decode := func(raw []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		err := d.Decode(&v)
		return v, err
	}
	x, e := decode(a)
	y, f := decode(b)
	return e == nil && f == nil && reflect.DeepEqual(x, y)
}

// Records is a preliminary owner-scoped data access layer, not wired to the app.
// The authenticated owner must be supplied by trusted server code, never a form.
type Records struct{ conn *pgx.Conn }

func NewRecords(conn *pgx.Conn) *Records { return &Records{conn} }

func (s *Records) Get(ctx context.Context, ownerID, id string) (json.RawMessage, error) {
	var b []byte
	err := s.conn.QueryRow(ctx, `SELECT document FROM jobtracker.jobs WHERE owner_id=$1 AND id=$2`, ownerID, id).Scan(&b)
	return json.RawMessage(b), err
}

func (s *Records) List(ctx context.Context, ownerID string) ([]json.RawMessage, error) {
	rows, err := s.conn.Query(ctx, `SELECT document FROM jobtracker.jobs WHERE owner_id=$1 ORDER BY id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}

// UpdateApplication preserves the immutable posting fields and migration source.
func (s *Records) UpdateApplication(ctx context.Context, ownerID, id string, application json.RawMessage) (bool, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(application, &obj) != nil || obj == nil {
		return false, errors.New("application must be a JSON object")
	}
	tag, err := s.conn.Exec(ctx, `UPDATE jobtracker.jobs SET document=jsonb_set(document,'{application}',$3::jsonb) WHERE owner_id=$1 AND id=$2`, ownerID, id, string(application))
	return tag.RowsAffected() == 1, err
}

func (s *Records) Delete(ctx context.Context, ownerID, id string) (bool, error) {
	tag, err := s.conn.Exec(ctx, `DELETE FROM jobtracker.jobs WHERE owner_id=$1 AND id=$2`, ownerID, id)
	return tag.RowsAffected() == 1, err
}
