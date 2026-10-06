package web

import (
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/tyler180/jobtracker/internal/jobs"
)

func TestEntriesReadingDoesNotMutate(t *testing.T) {
	store, err := jobs.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	importer := &fake{}
	handler := New(store, importer)
	read := func() string {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/entries", nil))
		if response.Code != 200 {
			t.Fatalf("%d: %s", response.Code, response.Body)
		}
		return response.Body.String()
	}
	if !strings.Contains(read(), "No saved entries yet") {
		t.Fatal("missing empty state")
	}
	_, _, err = store.Save(jobs.Job{URL: "https://example.com/one", Company: "Original company", Title: "Original title", DescriptionText: "<script>alert('description')</script>\nFull description", Application: jobs.Application{Company: "Edited company", Title: "Edited title", Status: "interview", InterviewStage: "round 1", InterviewNotes: "<img src=x onerror=alert(1)>", NextSteps: "Follow up", AppliedDate: "2026-10-01", ResponseDate: "2026-10-02", Round1Date: "2026-10-03"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Save(jobs.Job{URL: "https://example.com/two", Company: "Second company", Title: "Second title", DescriptionText: "Second description"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	body := read()
	for _, want := range []string{"Edited company", "Edited title", "Saved posting only", "2026-10-01", "2026-10-03", "Follow up", "&lt;img", "Search entries", "Entry status", `id="status-filter"`, `value="saved-only"`, "Applied date", "Round 1", "Start date", "End date", `type="date"`, `<details class="note-panel">`, "<summary>Interview notes</summary>", "<summary>Next steps</summary>"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, forbidden := range []string{"<script>alert", "<img src=x", "Full description", "Second description", "<form", "<button", "Save dates", "fetch(", "Initial screening", "Round 2", "Round 3", "<dd>—</dd>"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("unexpected %q", forbidden)
		}
	}
	dateAttributes := regexp.MustCompile(`data-dates="([^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(dateAttributes) != 2 {
		t.Fatalf("expected date attributes for both entries, got %d", len(dateAttributes))
	}
	var populated, empty bool
	for _, attribute := range dateAttributes {
		dates := strings.Fields(attribute[1])
		if len(dates) == 0 {
			empty = true
		} else if reflect.DeepEqual(dates, []string{"2026-10-01", "2026-10-03"}) {
			populated = true
		} else {
			t.Errorf("filter contains dates other than application progress dates: %v", dates)
		}
	}
	if !populated || !empty {
		t.Fatal("expected progress dates on tracked entry and no filter dates on saved-only entry")
	}
	after, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || importer.calls != 0 {
		t.Fatal("reading changed or fetched entries")
	}
}
