package web

import (
	"github.com/tyler180/jobtracker/internal/jobs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestDemoReadOnly(t *testing.T) {
	store, err := jobs.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SeedDemo(store); err != nil {
		t.Fatal(err)
	}
	handler := NewDemo(store)
	before, _ := store.List()
	for _, endpoint := range []struct{ method, path string }{
		{"POST", "/api/jobs"}, {"POST", "/api/jobs/preview"}, {"POST", "/api/jobs/pdf"},
		{"PUT", "/api/jobs/" + before[0].ID + "/application"}, {"PATCH", "/api/jobs/" + before[0].ID + "/dates"}, {"DELETE", "/api/jobs/" + before[0].ID},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`)))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", endpoint.method, endpoint.path, w.Code)
		}
	}
	for _, path := range []string{"/", "/entries", "/jobs/" + before[0].ID} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Demo — mock data") {
			t.Errorf("demo page %s: %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), `href="/new"`) || strings.Contains(w.Body.String(), `href="/edit`) {
			t.Errorf("write links visible on %s", path)
		}
	}
	for _, path := range []string{"/new", "/edit?id=" + before[0].ID} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 303 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	after, _ := store.List()
	if !reflect.DeepEqual(after, before) {
		t.Fatal("demo mutated")
	}
}
