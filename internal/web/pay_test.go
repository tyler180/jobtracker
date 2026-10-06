package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tyler180/jobtracker/internal/jobs"
)

func TestPayAPI(t *testing.T) {
	store, _ := jobs.Open(t.TempDir())
	handler := New(store, &fake{})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/jobs", `{"company":"Acme","title":"Engineer","description_text":"Original description","pay_min":"130000","pay_max":"180000","pay_type":"salary"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var j jobs.Job
	json.Unmarshal(w.Body.Bytes(), &j)
	path := "/api/jobs/" + j.ID + "/application"
	w = send("PUT", path, `{"company":"Acme","title":"Engineer","pay_min":"65.50","pay_max":"90","pay_type":"hourly"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = send("PUT", path, `{"company":"Acme","title":"New title"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	saved, _ := store.Get(j.ID)
	if saved.Application.PayMin != "65.50" || saved.Application.PayType != "hourly" || saved.DescriptionText != "Original description" {
		t.Fatal(saved)
	}
	w = send("GET", "/entries", "")
	if !strings.Contains(w.Body.String(), "$65.50 – $90 / hour") {
		t.Fatal("pay missing from entries")
	}
	w = send("PUT", path, `{"company":"Acme","title":"Engineer","pay_min":"100","pay_max":"90","pay_type":"hourly"}`)
	if w.Code != 400 {
		t.Fatalf("invalid range: %d", w.Code)
	}
	w = send("PUT", path, `{"company":"Acme","title":"Engineer","pay_min":"","pay_max":"","pay_type":""}`)
	if w.Code != 200 {
		t.Fatalf("clear: %d", w.Code)
	}
	saved, _ = store.Get(j.ID)
	if saved.Application.PayLabel() != "" {
		t.Fatal("pay not cleared")
	}
}
