package web

import (
	"context"
	"github.com/tyler180/jobtracker/internal/jobs"
	"net/http/httptest"
	"strings"
	"testing"
)

type fake struct{ calls int }

func (f *fake) Fetch(context.Context, string) (jobs.Job, error) {
	f.calls++
	return jobs.Job{URL: "https://jobs.ashbyhq.com/acme/abc", Title: "Engineer", DescriptionText: "Build"}, nil
}
func TestSaveBoundary(t *testing.T) {
	s, err := jobs.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	h := New(s, f)
	for _, body := range []string{`{"url":"a","url":"b"}`, `{"url":"a","unknown":true}`, `{"url":"a"} {}`, `{"url":null}`, `[]`, strings.Repeat("x", 5000)} {
		r := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid request had effects")
	}
	for _, status := range []int{201, 200} {
		r := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(`{"url":"https://jobs.ashbyhq.com/acme/abc"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%d: %s", w.Code, w.Body)
		}
	}
	r := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Engineer") {
		t.Fatal(w.Body)
	}
}
