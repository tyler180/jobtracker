package jobs

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestBrixton(t *testing.T) {
	body, err := os.ReadFile("testdata/brixton.html")
	if err != nil {
		t.Fatal(err)
	}
	source := "https://www.brixton.net/job-detail/?job=sr-ai-platform-engineer-cloud-database-fort-collins-26-00902"
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != source || r.Header.Get("Accept") != "text/html" {
			t.Fatalf("request: %v", r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	j, err := (Importer{client}).Fetch(context.Background(), source+"&utm_source=test#apply")
	if err != nil {
		t.Fatal(err)
	}
	if j.Provider != "brixton" || j.Company != "The Brixton Group" || j.Title != "Sr. AI Platform Engineer (Cloud & Database)" || j.Location != "Fort Collins, CO" || j.URL != source {
		t.Fatalf("metadata: %+v", j)
	}
	pay := InferPay(j.DescriptionText)
	if pay.PayMin != "140000" || pay.PayMax != "195000" || pay.PayType != "salary" {
		t.Fatalf("pay: %+v", pay)
	}
	for _, text := range []string{"$140k-195k", "Requirements:", "Responsibilities:", "Ideal Profile:", "26-00902", "AI-driven capabilities."} {
		if !strings.Contains(j.DescriptionText, text) {
			t.Errorf("missing %q", text)
		}
	}
	for _, body := range []string{"<h1>Job not found</h1>", strings.ReplaceAll(string(body), "26-00902", "26-00903"), strings.Split(string(body), "<span style=\"margin-bottom")[0], string(body) + string(body)} {
		target, _ := parse(source)
		if err := importBrixton([]byte(body), target, &Job{}); err == nil {
			t.Error("accepted incomplete or mismatched posting")
		}
	}
}

func TestBrixtonURLs(t *testing.T) {
	for _, source := range []string{"https://www.brixton.net/job-detail/", "https://www.brixton.net/job-detail/?job=a&job=b", "https://www.brixton.net/job-detail/?job=a%2Fb", "https://www.brixton.net/jobs/?job=a", "https://www.brixton.net.evil.test/job-detail/?job=a"} {
		if _, err := parse(source); err == nil {
			t.Errorf("accepted %s", source)
		}
	}
	target, err := parse("https://brixton.net/job-detail/?job=engineer-26-00902")
	if err != nil || target.canonical != "https://www.brixton.net/job-detail/?job=engineer-26-00902" {
		t.Fatalf("canonical: %+v %v", target, err)
	}
}
