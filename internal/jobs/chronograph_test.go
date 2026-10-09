package jobs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChronographImport(t *testing.T) {
	const canonical = "https://www.chronograph.pe/jobs/?gh_jid=5187048007"
	for _, raw := range []string{canonical, canonical + "&utm_source=test#application", "https://chronograph.pe/jobs?gh_jid=5187048007"} {
		t.Run(raw, func(t *testing.T) {
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://boards-api.greenhouse.io/v1/boards/chronograph/jobs/5187048007" || r.Header.Get("Accept") != "application/json" {
					t.Fatalf("unexpected request: %s %v", r.URL, r.Header)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"title":"Sr. Software Engineer, Platform Engineering","location":{"name":"Remote"},"content":"&lt;h2&gt;Requirements&lt;/h2&gt;&lt;p&gt;Build infrastructure &amp;amp; services.&lt;/p&gt;"}`))}, nil
			})}
			j, err := (Importer{client}).Fetch(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if j.Company != "Chronograph" || j.Provider != "greenhouse" || j.URL != canonical || j.Title != "Sr. Software Engineer, Platform Engineering" || j.Location != "Remote" || j.DescriptionText != "Requirements\n\nBuild infrastructure & services." {
				t.Fatalf("unexpected posting: %+v", j)
			}
		})
	}
}

func TestRejectChronographURLs(t *testing.T) {
	for _, raw := range []string{
		"https://www.chronograph.pe/jobs/",
		"https://www.chronograph.pe/jobs/?gh_jid=abc",
		"https://www.chronograph.pe/jobs/?gh_jid=0",
		"https://www.chronograph.pe/jobs/?gh_jid=5187048007&gh_jid=123",
		"https://www.chronograph.pe/jobs/?gh_jid=5187048007&bad=%zz",
		"https://www.chronograph.pe/other/?gh_jid=5187048007",
		"https://www.chronograph.pe/jobs/extra?gh_jid=5187048007",
		"https://www.chronograph.pe.evil.test/jobs/?gh_jid=5187048007",
		"https://chronograph.pe.evil.test/jobs/?gh_jid=5187048007",
		"https://www.chronograph.pe:444/jobs/?gh_jid=5187048007",
		"https://user@www.chronograph.pe/jobs/?gh_jid=5187048007",
	} {
		if _, err := parse(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
