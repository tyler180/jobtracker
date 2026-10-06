package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tyler180/jobtracker/internal/jobs"
)

func TestPayPreviewAndInterviewTimePersistence(t *testing.T) {
	dir := t.TempDir()
	store, _ := jobs.Open(dir)
	handler := New(store, &fake{})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/jobs/preview", `{"description_text":"Company: Acme\nTitle: Engineer\nHourly rate $65.50 - $90/hour"}`)
	var preview jobs.Job
	json.Unmarshal(w.Body.Bytes(), &preview)
	if w.Code != 200 || preview.Application.PayType != "hourly" || preview.Application.PayMin != "65.5" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = send("POST", "/api/jobs", `{"company":"Acme","title":"Engineer","description_text":"Salary $130,000 - $180,000","milestones":[{"stage":"round 1","date":"2026-10-12","time":"14:30","timezone":"America/Denver"}]}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var saved jobs.Job
	json.Unmarshal(w.Body.Bytes(), &saved)
	if saved.Application.PayMin != "130000" {
		t.Fatal("save did not infer pay")
	}
	w = send("PUT", "/api/jobs/"+saved.ID+"/application", `{"company":"Acme","title":"Engineer","pay_min":"140000","pay_max":"160000","pay_type":"salary"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	reopened, _ := jobs.Open(dir)
	j, _ := reopened.Get(saved.ID)
	if j.Application.Milestones[0].Time != "14:30" || j.Application.Milestones[0].Timezone != "America/Denver" || j.Application.PayMin != "140000" {
		t.Fatal(j.Application)
	}
	w = send("GET", "/entries", "")
	if !strings.Contains(w.Body.String(), "2:30 PM (America/Denver)") || !strings.Contains(w.Body.String(), "data-dates=\" 2026-10-12 ") {
		t.Fatal("time display or date filter regression")
	}
	w = send("POST", "/api/jobs", `{"company":"Acme","title":"Engineer","description_text":"Salary $130000 - $180000","pay_min":"","pay_max":"","pay_type":"salary"}`)
	json.Unmarshal(w.Body.Bytes(), &saved)
	if w.Code != 201 || saved.Application.PayMin != "" {
		t.Fatal("explicit cleared pay overwritten")
	}
}
