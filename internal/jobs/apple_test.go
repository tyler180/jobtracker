package jobs

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

const appleURL = "https://jobs.apple.com/en-us/details/200682046/Senior-Infrastructure-SRE"

func TestAppleImport(t *testing.T) {
	body, err := os.ReadFile("testdata/apple.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{appleURL, appleURL + "?utm_source=test#application", strings.ToLower(appleURL)} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != strings.ToLower(appleURL) || r.Header.Get("Accept") != "text/html" {
				t.Fatalf("unexpected request: %s %v", r.URL, r.Header)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
		})}
		j, err := (Importer{client}).Fetch(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if j.Provider != "apple" || j.Company != "Apple" || j.Title != "Senior Infrastructure SRE" || j.Location != "Cupertino, California, United States" || j.URL != strings.ToLower(appleURL) {
			t.Fatalf("unexpected metadata: %+v", j)
		}
		for _, text := range []string{"Summary", "Responsibilities", "Minimum Qualifications", "Preferred Qualifications", "Go (preferred)", "$150,400 and $277,600", "Pay & Benefits"} {
			if !strings.Contains(j.DescriptionText, text) {
				t.Errorf("missing %q", text)
			}
		}
		if strings.Contains(j.DescriptionText, "loaderData") {
			t.Fatal("included application state")
		}
	}
}

func TestRejectAppleURLs(t *testing.T) {
	for _, raw := range []string{
		"https://jobs.apple.com/en-us/details/abc/title",
		"https://jobs.apple.com/en-us/details/0/title",
		"https://jobs.apple.com/en-us/details/200682046",
		"https://jobs.apple.com/en-us/details/200682046/title/apply",
		"https://jobs.apple.com/en-us/details/200682046/%2Ftitle",
		"https://jobs.apple.com.evil.test/en-us/details/200682046/title",
		"https://jobs.apple.com:443/en-us/details/200682046/title",
		"https://user@jobs.apple.com/en-us/details/200682046/title",
		"https://jobs.apple.com/search",
	} {
		if _, err := parse(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestRejectAppleData(t *testing.T) {
	body, err := os.ReadFile("testdata/apple.html")
	if err != nil {
		t.Fatal(err)
	}
	target, err := parse(appleURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		"<html>Posting unavailable</html>",
		`<script>window.__staticRouterHydrationData = JSON.parse("invalid");</script>`,
		strings.ReplaceAll(string(body), "200682046", "200682047"),
		string(body) + string(body),
	} {
		if err := importApple([]byte(source), target, &Job{}); err == nil {
			t.Fatal("accepted missing, malformed, mismatched, or ambiguous posting")
		}
	}
}
