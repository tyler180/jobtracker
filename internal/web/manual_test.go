package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tyler180/jobtracker/internal/jobs"
)

func TestPastedDescription(t *testing.T) {
	dir := t.TempDir()
	store, err := jobs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &fake{}
	handler := New(store, fetcher)
	source := "https://careers.upstart.com/jobs/senior-data-platform-engineer-076371c4-e43b-4489-8132-a304724cc54f"
	description := "Responsibilities\n\n• Build data platforms\n<script>alert('test')</script>\n" + strings.Repeat("More detail. ", 3000)
	fields := map[string]string{"url": source + "?utm_source=test#apply", "company": "Upstart", "title": "Senior Data Platform Engineer", "description_text": description, "status": "applied"}
	send := func(method, path string, fields map[string]string) *httptest.ResponseRecorder {
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	w := send("POST", "/api/jobs", fields)
	if w.Code != 201 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	var saved jobs.Job
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Provider != "manual" || saved.URL != source || saved.DescriptionText != description || saved.Application.Company != "Upstart" {
		t.Fatal("incorrect snapshot fields or description text")
	}
	fields["description_text"] = "Changed"
	w = send("POST", "/api/jobs", fields)
	if w.Code != 200 {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body)
	}
	store, err = jobs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(saved.ID)
	if err != nil || got.DescriptionText != description || got.SavedAt != saved.SavedAt {
		t.Fatalf("snapshot not preserved: %v", err)
	}
	handler = New(store, fetcher)
	w = send("GET", "/jobs/"+saved.ID, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("unsafe description: %d", w.Code)
	}
	if fetcher.calls != 0 {
		t.Fatal("pasted description fetched source URL")
	}
	for _, change := range []map[string]string{{"title": ""}, {"company": ""}, {"description_text": strings.Repeat("x", 64001)}, {"url": "javascript:alert(1)"}, {"url": "https://user:pass@example.com/job"}} {
		invalid := map[string]string{}
		for k, v := range fields {
			invalid[k] = v
		}
		for k, v := range change {
			invalid[k] = v
		}
		w = send("POST", "/api/jobs", invalid)
		if w.Code != 400 {
			t.Fatalf("invalid manual fields: %d %s", w.Code, w.Body)
		}
	}
	if fetcher.calls != 0 {
		t.Fatal("invalid pasted description fetched source URL")
	}
}
