package jobs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestProviders(t *testing.T) {
	cases := []struct{ name, url, endpoint, body string }{
		{"ashby", "https://jobs.ashbyhq.com/acme/abc?utm_source=test", "https://api.ashbyhq.com/posting-api/job-board/acme", `{"jobs":[{"title":"Engineer","location":"Remote","jobUrl":"https://jobs.ashbyhq.com/acme/abc","descriptionHtml":"<p>Build things</p>"}]}`},
		{"greenhouse", "https://boards.greenhouse.io/acme/jobs/123", "https://boards-api.greenhouse.io/v1/boards/acme/jobs/123", `{"title":"Engineer","content":"&lt;p&gt;Build things&lt;/p&gt;","location":{"name":"Remote"}}`},
		{"workday", "https://acme.wd5.myworkdayjobs.com/en-US/External/job/Denver/Engineer_R123/apply", "https://acme.wd5.myworkdayjobs.com/wday/cxs/acme/External/job/Denver/Engineer_R123", `{"jobPostingInfo":{"title":"Engineer","jobDescription":"<p>Build things</p>","location":"Remote"}}`},
		{"upstart", "https://careers.upstart.com/jobs/senior-data-platform-engineer-076371c4-e43b-4489-8132-a304724cc54f?utm_source=test", "https://careers.upstart.com/jobs/senior-data-platform-engineer-076371c4-e43b-4489-8132-a304724cc54f", `<h1>Engineer</h1><div class="job-description"><p>Build things</p></div><footer>Cookies</footer>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != c.endpoint {
					t.Fatalf("endpoint: %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(c.body)), Header: make(http.Header)}, nil
			})}
			j, err := (Importer{client}).Fetch(context.Background(), c.url)
			if err != nil {
				t.Fatal(err)
			}
			if j.DescriptionText != "Build things" || j.Title != "Engineer" {
				t.Fatalf("bad job: %+v", j)
			}
		})
	}
}
func TestRejectURLs(t *testing.T) {
	for _, u := range []string{"http://jobs.ashbyhq.com/acme/abc", "https://jobs.ashbyhq.com.evil.test/acme/abc", "https://user:pass@jobs.ashbyhq.com/acme/abc", "https://jobs.ashbyhq.com:444/acme/abc", "https://localhost/job/a", "https://acme.myworkdayjobs.com/site/job/a", "https://jobs.ashbyhq.com/acme/../abc", "https://jobs.ashbyhq.com/acme/%2Fabc", "https://jobs.ashbyhq.com/acme", "https://careers.upstart.com.evil.test/jobs/abc", "https://careers.upstart.com/jobs/abc", "https://careers.upstart.com/jobs/senior-data-platform-engineer-076371c4-e43b-4489-8132-a304724cc54f/apply"} {
		if _, err := parse(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
}
func TestFailedImports(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
	}{{404, `{}`}, {200, `not json`}, {200, `{"title":"Engineer","content":""}`}} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body))}, nil
		})}
		if _, err := (Importer{client}).Fetch(context.Background(), "https://boards.greenhouse.io/acme/jobs/123"); err == nil {
			t.Fatal("expected failure")
		}
	}
}
func TestPlainText(t *testing.T) {
	got := PlainText(`<h2>Requirements</h2><ul><li>Go &amp; SQL</li><li>Kindness</li></ul><script>evil()</script>`)
	if !strings.Contains(got, "• Go & SQL") || strings.Contains(got, "evil") {
		t.Fatal(got)
	}
}
func TestStorePreservesSnapshot(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := Job{URL: "https://jobs.ashbyhq.com/acme/abc", Title: "Original", DescriptionText: "First"}
	first, created, err := s.Save(original)
	if err != nil || !created {
		t.Fatalf("%v", err)
	}
	original.Title = "Changed"
	second, created, err := s.Save(original)
	if err != nil || created || second.Title != first.Title {
		t.Fatal("snapshot changed")
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Title != "Original" {
		t.Fatalf("restart: %v %v", list, err)
	}
}
