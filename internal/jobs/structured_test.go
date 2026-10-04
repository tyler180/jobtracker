package jobs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const fordURL = "https://www.careers.ford.com/job/dearborn/senior-hybrid-cloud-and-mainframe-architect/48560/99029085584"
const principalURL = "https://careers.principal.com/careers-home/jobs/51499"

func structuredFixture(url, title string, location any) map[string]any {
	return map[string]any{"@type": "JobPosting", "url": url, "title": title, "hiringOrganization": map[string]string{"name": "Posting company"}, "jobLocation": location, "description": `<h2>Responsibilities</h2><p>Build cloud services.</p><ul><li>Final qualification</li></ul><h3>Benefits</h3><p>Salary and benefits</p><script>evil()</script>`}
}
func structuredHTML(value any) string {
	b, _ := json.Marshal(value)
	return `<nav>Unrelated navigation</nav><script type="application/ld+json">` + string(b) + `</script><footer>Cookie preferences</footer>`
}
func TestStructuredProviders(t *testing.T) {
	for _, tc := range []struct{ url, provider string }{{fordURL, "ford"}, {principalURL, "principal"}} {
		t.Run(tc.provider, func(t *testing.T) {
			for _, wrapper := range []string{"single", "array", "graph"} {
				posting := structuredFixture(tc.url+"?utm_source=test", "Cloud Engineer", map[string]any{"address": map[string]any{"addressLocality": "Des Moines", "addressRegion": "Iowa", "addressCountry": map[string]string{"name": "United States"}}})
				var data any = posting
				if wrapper == "array" {
					data = []any{map[string]string{"@type": "BreadcrumbList"}, posting}
				}
				if wrapper == "graph" {
					posting["@type"] = []string{"Thing", "JobPosting"}
					posting["jobLocation"] = []any{posting["jobLocation"]}
					data = map[string]any{"@graph": []any{posting}}
				}
				client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() != tc.url || r.Header.Get("Accept") != "text/html" || r.Header.Get("Cookie") != "" {
						t.Fatalf("incorrect request %s", r.URL)
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(structuredHTML(data)))}, nil
				})}
				j, err := (Importer{client}).Fetch(context.Background(), tc.url+"/?tracking=test#job")
				if err != nil {
					t.Fatal(err)
				}
				if j.Title != "Cloud Engineer" || j.Company != "Posting company" || j.Provider != tc.provider || j.URL != tc.url || j.Location != "Des Moines, Iowa, United States" {
					t.Fatalf("wrong metadata: %+v", j)
				}
				for _, text := range []string{"Build cloud services.", "• Final qualification", "Salary and benefits"} {
					if !strings.Contains(j.DescriptionText, text) {
						t.Fatal("missing", text)
					}
				}
				for _, text := range []string{"Unrelated navigation", "Cookie preferences", "evil()"} {
					if strings.Contains(j.DescriptionText, text) {
						t.Fatal("unexpected", text)
					}
				}
				manual, err := ManualPreview(tc.url+"/?tracking=test", "Job title: Cloud Engineer\nBuild cloud services.")
				if err != nil || manual.URL != j.URL || manual.Title != j.Title || manual.Company == "" {
					t.Fatalf("manual fallback: %+v %v", manual, err)
				}
			}
		})
	}
}
func TestStructuredRejectURLs(t *testing.T) {
	for _, url := range []string{
		"http://careers.principal.com/careers-home/jobs/51499", "https://careers.principal.com.evil.test/careers-home/jobs/51499", "https://user:pass@careers.principal.com/careers-home/jobs/51499", "https://careers.principal.com:444/careers-home/jobs/51499", "https://careers.principal.com/careers-home/jobs/0", "https://careers.principal.com/careers-home/jobs/title", "https://careers.principal.com/careers-home/jobs/51499/apply", "https://careers.principal.com/careers-home/jobs/%2f51499",
		"https://www.careers.ford.com.evil.test/job/dearborn/title/48560/99029085584", "https://www.careers.ford.com/job/dearborn/title/99999/99029085584", "https://www.careers.ford.com/job/dearborn/title/48560/no-id", fordURL + "/apply", "https://www.careers.ford.com/search-jobs"} {
		if _, err := parse(url); err == nil {
			t.Fatal("accepted", url)
		}
	}
}
func TestStructuredIncompleteResponses(t *testing.T) {
	for _, url := range []string{fordURL, principalURL} {
		for _, scenario := range []string{"missing", "wrong job", "duplicate", "missing title", "missing description", "invalid title", "invalid json", "script only", "bad location", "unrelated"} {
			t.Run(url+"/"+scenario, func(t *testing.T) {
				posting := structuredFixture(url, "Cloud Engineer", nil)
				var data any = posting
				switch scenario {
				case "missing":
					data = map[string]string{"@type": "WebPage"}
				case "wrong job":
					posting["url"] = strings.Replace(url, "99029085584", "99029085585", 1)
					if url == principalURL {
						posting["url"] = strings.Replace(url, "51499", "51498", 1)
					}
				case "duplicate":
					data = []any{posting, posting}
				case "missing title":
					posting["title"] = ""
				case "missing description":
					posting["description"] = ""
				case "invalid title":
					posting["title"] = 123
				case "script only":
					posting["description"] = "<script>evil()</script>"
				case "bad location":
					posting["jobLocation"] = "unexpected"
				case "unrelated":
					posting["url"] = "https://evil.test/jobs/51499"
				}
				body := structuredHTML(data)
				if scenario == "invalid json" {
					body = `<script type="application/ld+json">broken</script>`
				}
				client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
				})}
				if _, err := (Importer{client}).Fetch(context.Background(), url); err == nil {
					t.Fatal("accepted incomplete response")
				}
			})
		}
	}
}

func TestGenericPosting(t *testing.T) {
	const rawURL = "https://careers.example.com/open%20roles?job=42&utm_source=test#description"
	const canonical = "https://careers.example.com/open%20roles?job=42"
	for _, scenario := range []string{"url", "relative", "no url"} {
		posting := structuredFixture(canonical, "Generic Engineer", nil)
		if scenario == "relative" {
			posting["url"] = "/open%20roles?job=42"
		}
		if scenario == "no url" {
			delete(posting, "url")
		}
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != canonical || r.Header.Get("Accept") != "text/html" {
				t.Fatalf("request %s", r.URL)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(structuredHTML(posting)))}, nil
		})}
		got, err := (Importer{client}).Fetch(context.Background(), rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if got.URL != canonical || got.Provider != "generic" || got.Title != "Generic Engineer" || got.Company != "Posting company" {
			t.Fatalf("unexpected: %+v", got)
		}
		manual, err := ManualPreview(rawURL, "Company: Posting company\nJob title: Generic Engineer\nDescription.")
		if err != nil || manual.URL != canonical {
			t.Fatal(manual, err)
		}
	}
}

func TestGenericIncompletePosting(t *testing.T) {
	const url = "https://careers.example.com/jobs/42"
	for _, scenario := range []string{"ambiguous", "no company", "wrong url", "missing description", "login", "redirect", "blocked", "invalid url"} {
		posting := structuredFixture(url, "Engineer", nil)
		var data any = posting
		status := 200
		switch scenario {
		case "ambiguous":
			delete(posting, "url")
			data = []any{posting, posting}
		case "no company":
			delete(posting, "hiringOrganization")
		case "wrong url":
			posting["url"] = "https://careers.example.com/jobs/43"
		case "missing description":
			delete(posting, "description")
		case "invalid url":
			posting["url"] = 123
		case "redirect":
			status = 302
		case "blocked":
			status = 403
		}
		body := structuredHTML(data)
		if scenario == "login" {
			body = "<h1>Sign in</h1>"
		}
		client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := (Importer{client}).Fetch(context.Background(), url); err == nil {
			t.Fatal("accepted", scenario)
		}
	}
}

func TestGenericURLBoundary(t *testing.T) {
	for _, url := range []string{"http://careers.example.com/jobs/1", "https://localhost/jobs/1", "https://127.0.0.1/jobs/1", "https://[::1]/jobs/1", "https://169.254.169.254/latest/meta-data/", "https://careers.example.com:444/jobs/1", "https://careers.example.com:/jobs/1", "https://user:pass@careers.example.com/jobs/1", "https://-bad.example.com/jobs/1"} {
		if _, err := parse(url); err == nil {
			t.Fatal("accepted", url)
		}
	}
	for _, url := range []string{"https://127.0.0.1/", "https://192.168.1.1/", "https://169.254.169.254/", "https://[::1]/"} {
		if response, err := NewClient().Get(url); err == nil {
			response.Body.Close()
			t.Fatal("client connected to private address", url)
		}
	}
}
