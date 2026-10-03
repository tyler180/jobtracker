package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tyler180/jobtracker/internal/jobs"
)

type blockedPosting struct{}

func (blockedPosting) Fetch(context.Context, string) (jobs.Job, error) {
	return jobs.Job{}, errors.New("provider returned HTTP 202")
}

func TestDescriptionMetadataPreviewAndSave(t *testing.T) {
	store, err := jobs.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := New(store, blockedPosting{})
	source := "https://careers.upstart.com/jobs/senior-data-platform-engineer-076371c4-e43b-4489-8132-a304724cc54f"
	send := func(path string, fields map[string]string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(fields)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := send("/api/jobs/preview", map[string]string{"url": source})
	var preview struct {
		jobs.Job
		DescriptionRequired bool `json:"description_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil || w.Code != 200 || !preview.DescriptionRequired || preview.Company != "Upstart" || preview.Title != "Senior Data Platform Engineer" {
		t.Fatalf("fallback preview: %d %s", w.Code, w.Body)
	}
	w = send("/api/jobs", map[string]string{"url": source, "status": "applied"})
	if w.Code != 502 {
		t.Fatalf("URL-only blocked save: %d", w.Code)
	}
	description := "Staff Platform Engineer\nAbout Upstart\nBuild things."
	w = send("/api/jobs/preview", map[string]string{"url": source, "description_text": description})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"title":"Staff Platform Engineer"`) {
		t.Fatalf("description preview: %d %s", w.Code, w.Body)
	}
	list, err := store.List()
	if err != nil || len(list) != 0 {
		t.Fatal("preview saved a job")
	}
	w = send("/api/jobs", map[string]string{"url": source, "description_text": description, "company": "", "title": "", "status": "applied"})
	if w.Code != 201 {
		t.Fatalf("inferred save: %d %s", w.Code, w.Body)
	}
	var saved jobs.Job
	json.Unmarshal(w.Body.Bytes(), &saved)
	if saved.Application.Company != "Upstart" || saved.Application.Title != "Staff Platform Engineer" || saved.DescriptionText != description {
		t.Fatal("inferred metadata or description was lost")
	}
}
