package jobs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUpstartUnavailable(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
	}{
		{202, ""},
		{200, `<h1>Not found</h1><p>Cookies</p>`},
		{200, `<h1>Engineer</h1><div class="job-description"></div>`},
		{200, `<div class="job-description">Build things</div>`},
		{200, `<h1>Engineer</h1><div class="job-description">One</div><div class="job-description">Two</div>`},
	} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Accept") != "text/html" {
				t.Fatal("not requesting HTML")
			}
			return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body))}, nil
		})}
		if _, err := (Importer{client}).Fetch(context.Background(), "https://careers.upstart.com/jobs/engineer-076371c4-e43b-4489-8132-a304724cc54f"); err == nil {
			t.Fatal("accepted missing or ambiguous description")
		}
	}
}
