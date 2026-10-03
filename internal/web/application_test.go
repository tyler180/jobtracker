package web

import (
	"encoding/json"
	"github.com/tyler180/jobtracker/internal/jobs"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplicationLifecycle(t *testing.T) {
	dir := t.TempDir()
	store, err := jobs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := New(store, &fake{})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/jobs", `{"url":"https://jobs.ashbyhq.com/acme/abc","company":"Acme","title":"Senior Engineer","status":"applied"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var original jobs.Job
	if err := json.Unmarshal(w.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	path := "/api/jobs/" + original.ID + "/application"
	for _, body := range []string{`{"company":"Acme","title":"Engineer","status":"invalid"}`, `{"company":"Acme","title":"Engineer","status":"interview"}`, `{"company":"Acme","title":"Engineer","status":"applied","status":"interview"}`, `{"company":"Acme","title":"Engineer","status":"applied"} {}`, `{"company":null}`, `{"company":"Acme","title":"Engineer","status":"interview","interview_stage":"round 4"}`} {
		if w := send("PUT", path, body); w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	w = send("PUT", path, `{"company":"Acme & Co","title":"Staff Engineer","status":"interview","interview_stage":"round 2","interview_notes":"Went well <script>alert(1)</script>","next_steps":"Meet the team"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	reopened, err := jobs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := reopened.Get(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if j.Application.InterviewStage != "round 2" || j.Application.NextSteps != "Meet the team" || j.DescriptionText != original.DescriptionText || j.Title != original.Title || !j.SavedAt.Equal(original.SavedAt) {
		t.Fatalf("Unexpected saved job: %+v", j)
	}
	w = send("GET", "/jobs/"+j.ID, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Build") {
		t.Fatal(w.Code, w.Body)
	}
	if w := send("GET", "/jobs/bad", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = send("POST", "/api/jobs", `{"url":"https://jobs.ashbyhq.com/acme/abc"}`)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var duplicate jobs.Job
	json.Unmarshal(w.Body.Bytes(), &duplicate)
	if duplicate.Application.Status != "interview" {
		t.Fatal("Duplicate import overwrote application")
	}
}

func TestDeleteJob(t *testing.T) {
	dir := t.TempDir()
	store, err := jobs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	removed, _, err := store.Save(jobs.Job{URL: "https://jobs.ashbyhq.com/acme/remove", Title: "Remove me"})
	if err != nil {
		t.Fatal(err)
	}
	kept, _, err := store.Save(jobs.Job{URL: "https://jobs.ashbyhq.com/acme/keep", Title: "Keep me"})
	if err != nil {
		t.Fatal(err)
	}
	h := New(store, &fake{})
	request := func(id, site string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("DELETE", "/api/jobs/"+id, nil)
		r.Header.Set("Sec-Fetch-Site", site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(removed.ID, "cross-site"); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	if _, err := store.Get(removed.ID); err != nil {
		t.Fatal("cross-site request removed job", err)
	}
	if w := request("invalid", ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(removed.ID, "same-origin"); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(removed.ID, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	reopened, err := jobs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != kept.ID {
		t.Fatalf("Unexpected remaining archive: %+v", list)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/jobs/"+removed.ID, nil))
	if w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
}

func TestImportedApplicationTitle(t *testing.T) {
	for _, tc := range []struct{ name, body, title string }{
		{"omitted", `{"url":"https://jobs.ashbyhq.com/acme/abc","company":"Acme","status":"applied"}`, "Engineer"},
		{"blank", `{"url":"https://jobs.ashbyhq.com/acme/abc","company":"Acme","title":"  ","status":"applied"}`, "Engineer"},
		{"override", `{"url":"https://jobs.ashbyhq.com/acme/abc","company":"Acme","title":"Custom title","status":"applied"}`, "Custom title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := jobs.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			h := New(store, &fake{})
			r := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 201 {
				t.Fatal(w.Code, w.Body)
			}
			list, err := store.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 || list[0].Application.Title != tc.title {
				t.Fatalf("Unexpected application: %+v", list)
			}
		})
	}
}

func TestPreviewDoesNotSave(t *testing.T) {
	store, err := jobs.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := New(store, &fake{})
	r := httptest.NewRequest("POST", "/api/jobs/preview", strings.NewReader(`{"url":"https://jobs.ashbyhq.com/acme/abc"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var posting jobs.Job
	if err := json.Unmarshal(w.Body.Bytes(), &posting); err != nil {
		t.Fatal(err)
	}
	if posting.Title != "Engineer" {
		t.Fatal(posting.Title)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatal("Preview saved posting")
	}
}

func TestCompanyAndDateDefaults(t *testing.T) {
	store, err := jobs.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	importer := &fake{}
	h := New(store, importer)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/jobs", `{"url":"https://jobs.ashbyhq.com/acme/abc","status":"applied","applied_date":"2026-09-30"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var j jobs.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if j.Application.Company != "acme" || j.Application.Title != "Engineer" || j.Application.AppliedDate != "2026-09-30" || j.Application.ResponseDate != "" {
		t.Fatalf("Unexpected application: %+v", j.Application)
	}
	path := "/api/jobs/" + j.ID + "/application"
	w = send("PUT", path, `{"company":"Custom company","title":"Engineer","status":"interview","interview_stage":"round 1","round_1_date":"2026-10-08"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	j, err = store.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if j.Application.AppliedDate != "2026-09-30" || j.Application.ResponseDate != jobs.Today() || j.Application.Round1Date != "2026-10-08" {
		t.Fatalf("Unexpected dates: %+v", j.Application)
	}
	w = send("PUT", path, `{"company":"Custom company","title":"Engineer","status":"not moving forward"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	j, err = store.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if j.Application.Round1Date != "2026-10-08" {
		t.Fatal("Status change lost interview history")
	}
	calls := importer.calls
	w = send("POST", "/api/jobs", `{"url":"https://jobs.ashbyhq.com/acme/abc","status":"applied","applied_date":"2026-02-30"}`)
	if w.Code != 400 || importer.calls != calls {
		t.Fatal("Invalid date triggered import", w.Code)
	}
}
