// Package postgres implements owner-scoped application persistence.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyler180/jobtracker/internal/identity"
	"github.com/tyler180/jobtracker/internal/jobs"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool} }

func owner(ctx context.Context) (string, error) {
	id := identity.UserID(ctx)
	if id == "" {
		return "", errors.New("authenticated owner is required")
	}
	return id, nil
}

func (s *Store) List(ctx context.Context) ([]jobs.Job, error) {
	id, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT document FROM jobtracker.jobs WHERE owner_id=$1 ORDER BY (document->>'saved_at') DESC,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []jobs.Job{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var j jobs.Job
		if err := json.Unmarshal(b, &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (jobs.Job, error) {
	uid, err := owner(ctx)
	if err != nil {
		return jobs.Job{}, err
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT document FROM jobtracker.jobs WHERE owner_id=$1 AND id=$2`, uid, id).Scan(&raw); err != nil {
		return jobs.Job{}, notFound(err)
	}
	var j jobs.Job
	err = json.Unmarshal(raw, &j)
	return j, err
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return os.ErrNotExist
	}
	return err
}

func (s *Store) Save(ctx context.Context, j jobs.Job) (jobs.Job, bool, error) {
	uid, err := owner(ctx)
	if err != nil {
		return jobs.Job{}, false, err
	}
	key := []byte(j.URL)
	if j.URL == "" {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return jobs.Job{}, false, err
		}
	}
	sum := sha256.Sum256(key)
	j.ID = hex.EncodeToString(sum[:])
	j.SavedAt = time.Now().UTC()
	raw, err := json.Marshal(j)
	if err != nil {
		return jobs.Job{}, false, err
	}
	// ON CONFLICT waits for an in-flight identical save. A subsequent SELECT
	// observes the committed winner without overwriting its immutable snapshot.
	tag, err := s.pool.Exec(ctx, `INSERT INTO jobtracker.jobs(owner_id,id,document,source_json) VALUES($1,$2,$3::jsonb,$4) ON CONFLICT(owner_id,id) DO NOTHING`, uid, j.ID, string(raw), raw)
	if err != nil {
		return jobs.Job{}, false, err
	}
	if tag.RowsAffected() == 1 {
		return j, true, nil
	}
	old, err := s.Get(ctx, j.ID)
	return old, false, err
}

func (s *Store) UpdateApplication(ctx context.Context, id string, a jobs.Application) (jobs.Job, error) {
	if err := a.Validate(); err != nil {
		return jobs.Job{}, err
	}
	return s.change(ctx, id, func(j jobs.Job) (jobs.Application, error) {
		a.Company = strings.TrimSpace(a.Company)
		a.Title = strings.TrimSpace(a.Title)
		if a.Status != j.Application.Status || a.InterviewStage != j.Application.InterviewStage {
			a.DefaultDates(jobs.Today())
		}
		return a, nil
	})
}

func (s *Store) UpdateDates(ctx context.Context, id string, dates map[string]string) (jobs.Job, error) {
	return s.change(ctx, id, func(j jobs.Job) (jobs.Application, error) {
		a := j.Application
		for name, value := range dates {
			if value != "" {
				if _, err := time.Parse("2006-01-02", value); err != nil {
					return a, errors.New("invalid date")
				}
			}
			switch name {
			case "applied_date":
				a.AppliedDate = value
			case "response_date":
				a.ResponseDate = value
			case "screening_date":
				a.ScreeningDate = value
			case "round_1_date":
				a.Round1Date = value
			case "round_2_date":
				a.Round2Date = value
			case "round_3_date":
				a.Round3Date = value
			default:
				return a, errors.New("unknown date field")
			}
		}
		return a, nil
	})
}

func (s *Store) change(ctx context.Context, id string, fn func(jobs.Job) (jobs.Application, error)) (jobs.Job, error) {
	uid, err := owner(ctx)
	if err != nil {
		return jobs.Job{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return jobs.Job{}, err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT document FROM jobtracker.jobs WHERE owner_id=$1 AND id=$2 FOR UPDATE`, uid, id).Scan(&raw); err != nil {
		return jobs.Job{}, notFound(err)
	}
	var j jobs.Job
	if err := json.Unmarshal(raw, &j); err != nil {
		return j, err
	}
	a, err := fn(j)
	if err != nil {
		return j, err
	}
	// Merge known application fields, preserving future fields and the posting.
	b, err := json.Marshal(a)
	if err != nil {
		return j, err
	}
	if err := tx.QueryRow(ctx, `UPDATE jobtracker.jobs SET document=jsonb_set(document,'{application}',COALESCE(NULLIF(document->'application','null'::jsonb),'{}'::jsonb) || $3::jsonb) WHERE owner_id=$1 AND id=$2 RETURNING document`, uid, id, string(b)).Scan(&raw); err != nil {
		return j, err
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return j, err
	}
	if err := tx.Commit(ctx); err != nil {
		return jobs.Job{}, err
	}
	return j, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	uid, err := owner(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM jobtracker.jobs WHERE owner_id=$1 AND id=$2`, uid, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return nil
}
