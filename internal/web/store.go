package web

import (
	"context"
	"net/http"

	"github.com/tyler180/jobtracker/internal/jobs"
)

// Store owns persistence and enforces ownership using the authenticated context.
type Store interface {
	List(context.Context) ([]jobs.Job, error)
	Save(context.Context, jobs.Job) (jobs.Job, bool, error)
	Get(context.Context, string) (jobs.Job, error)
	UpdateApplication(context.Context, string, jobs.Application) (jobs.Job, error)
	UpdateDates(context.Context, string, map[string]string) (jobs.Job, error)
	Delete(context.Context, string) error
}

// NewWithStore is wrapped by authentication middleware in PostgreSQL mode.
func NewWithStore(store Store, importer Fetcher) http.Handler {
	return newServer(store, importer, false)
}

type fileStore struct{ *jobs.Store }

func (s fileStore) List(_ context.Context) ([]jobs.Job, error) { return s.Store.List() }
func (s fileStore) Save(_ context.Context, j jobs.Job) (jobs.Job, bool, error) {
	return s.Store.Save(j)
}
func (s fileStore) Get(_ context.Context, id string) (jobs.Job, error) { return s.Store.Get(id) }
func (s fileStore) UpdateApplication(_ context.Context, id string, a jobs.Application) (jobs.Job, error) {
	return s.Store.UpdateApplication(id, a)
}
func (s fileStore) UpdateDates(_ context.Context, id string, d map[string]string) (jobs.Job, error) {
	return s.Store.UpdateDates(id, d)
}
func (s fileStore) Delete(_ context.Context, id string) error { return s.Store.Delete(id) }
