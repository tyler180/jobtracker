package jobs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const linkedInFixture = `<h2 class="top-card-layout__title topcard__title">Sr. Cloud Platform Engineer</h2>
<a class="topcard__org-name-link">RC Talent</a><span class="topcard__flavor--bullet">United States</span>
<div class="description__text description__text--rich"><section><div class="show-more-less-html__markup show-more-less-html__markup--clamp-after-5"><p>About the role</p><ul><li>Build AWS &amp; Go services.</li><li>Final requirement after the visual clamp.</li></ul><script>evil()</script></div><button>Show more</button><button>Show less</button></section></div>
<div class="show-more-less-html__markup">Unrelated company overview</div><ul><li>Seniority level</li><li>Employment type</li></ul>`

func TestLinkedInURLsAndDescription(t *testing.T) {
	for _, raw := range []string{
		"https://www.linkedin.com/jobs/view/4463106136/",
		"https://linkedin.com/jobs/view/4463106136?trackingId=anything#about",
		"https://www.linkedin.com/jobs/view/sr-cloud-platform-engineer-at-rc-talent-4463106136/",
	} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://www.linkedin.com/jobs-guest/jobs/api/jobPosting/4463106136" || r.Header.Get("Accept") != "text/html" {
				t.Fatalf("unexpected request: %s", r.URL)
			}
			if r.Header.Get("Cookie") != "" {
				t.Fatal("used cookies")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(linkedInFixture))}, nil
		})}
		got, err := (Importer{client}).Fetch(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if got.Provider != "linkedin" || got.URL != "https://www.linkedin.com/jobs/view/4463106136/" || got.Company != "RC Talent" || got.Title != "Sr. Cloud Platform Engineer" || got.Location != "United States" {
			t.Fatalf("incorrect metadata: %+v", got)
		}
		for _, expected := range []string{"• Build AWS & Go services.", "Final requirement after the visual clamp."} {
			if !strings.Contains(got.DescriptionText, expected) {
				t.Fatalf("missing %q", expected)
			}
		}
		for _, excluded := range []string{"Show more", "Show less", "Seniority level", "Employment type", "Unrelated company overview", "evil()"} {
			if strings.Contains(got.DescriptionText, excluded) {
				t.Fatalf("included %q", excluded)
			}
		}
		preview, err := ManualPreview(raw, "About the job\nSr. Cloud Platform Engineer\nCompany: RC Talent\nBuild services.")
		if err != nil || preview.URL != got.URL || preview.Title != got.Title || preview.Company != got.Company {
			t.Fatalf("manual preview: %+v %v", preview, err)
		}
	}
}

func TestLinkedInRejectsURLs(t *testing.T) {
	for _, raw := range []string{
		"https://www.linkedin.com.evil.test/jobs/view/4463106136/",
		"https://www.linkedin.com/jobs/view/not-a-job",
		"https://www.linkedin.com/jobs/view/0/",
		"https://www.linkedin.com/jobs/view/4463106136/extra",
		"https://www.linkedin.com/jobs/search/?currentJobId=4463106136",
		"https://www.linkedin.com/jobs/view/%2F4463106136",
		"https://www.linkedin.com:444/jobs/view/4463106136",
		"https://user:pass@www.linkedin.com/jobs/view/4463106136",
	} {
		if _, err := parse(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestLinkedInIncompleteResponses(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
	}{
		{429, "Slow down"}, {302, "Sign in"}, {200, `<h1>Sign in</h1>`},
		{200, strings.Replace(linkedInFixture, "topcard__org-name-link", "other-class", 1)},
		{200, strings.Replace(linkedInFixture, "description__text description__text--rich", "other-class", 1)},
		{200, linkedInFixture + `<h2 class="top-card-layout__title">Another title</h2>`},
		{200, `<h2 class="top-card-layout__title">Engineer</h2><a class="topcard__org-name-link">Acme</a><div class="description__text"><div class="show-more-less-html__markup"></div></div>`},
	} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body))}, nil
		})}
		if _, err := (Importer{client}).Fetch(context.Background(), "https://www.linkedin.com/jobs/view/4463106136/"); err == nil {
			t.Fatal("accepted blocked or incomplete posting")
		}
	}
}
