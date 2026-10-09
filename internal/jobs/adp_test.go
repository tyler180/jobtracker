package jobs

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestADPImport(t *testing.T) {
	const raw = "https://myjobs.adp.com/ecsfederal/cx/job-details?reqId=5001221044506"
	fixture, err := os.ReadFile("testdata/adp.json")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := string(fixture)
		if calls == 1 {
			if r.URL.String() != "https://myjobs.adp.com/public/staffing/v1/career-site/ecsfederal" {
				t.Fatal(r.URL)
			}
			body = `{"domain":"ecsfederal","name":"Everforth ECS","active":true,"myJobsToken":"public-context","links":[{"href":"http://127.0.0.1"}]}`
		} else {
			if r.URL.String() != "https://my.adp.com/myadp_prefix/mycareer/public/staffing/v1/job-requisitions/search-meta/5001221044506" || r.Header.Get("myJobsToken") != "public-context" {
				t.Fatalf("unexpected request %s", r.URL)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	j, err := (Importer{client}).Fetch(context.Background(), raw+"&utm_source=test#apply")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || j.Provider != "adp" || j.URL != raw || j.Company != "Everforth ECS" || j.Title != "Cloud/Network Infrastructure Engineer, Senior" || j.Location != "Work from home, Virginia, United States" || !strings.Contains(j.DescriptionText, "AWS Associate or Professional-level certification") {
		t.Fatalf("unexpected posting metadata: %s / %s / %s", j.Company, j.Title, j.Location)
	}
	pay := InferPay(j.DescriptionText)
	if pay.PayMin != "123000" || pay.PayMax != "184000" || pay.PayType != "salary" {
		t.Fatalf("pay: %+v", pay)
	}
}

func TestRejectADPURLs(t *testing.T) {
	for _, raw := range []string{
		"http://myjobs.adp.com/acme/cx/job-details?reqId=1",
		"https://myjobs.adp.com.evil.test/acme/cx/job-details?reqId=1",
		"https://user@myjobs.adp.com/acme/cx/job-details?reqId=1",
		"https://myjobs.adp.com:443/acme/cx/job-details?reqId=1",
		"https://myjobs.adp.com/acme/cx/job-details?reqId=1&reqId=2",
		"https://myjobs.adp.com/acme/cx/job-details?reqId=abc",
		"https://myjobs.adp.com/acme/cx/job-details?reqId=0",
		"https://myjobs.adp.com/acme/cx/job-details?reqId=1&bad=%zz",
		"https://myjobs.adp.com/acme/cx/job-details/extra?reqId=1",
	} {
		if _, err := parse(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestRejectADPData(t *testing.T) {
	target, _ := parse("https://myjobs.adp.com/acme/cx/job-details?reqId=123")
	for _, body := range []string{`{}`, `{"domain":"other","active":true,"name":"Acme"}`, `{"domain":"acme","active":false,"name":"Acme"}`} {
		if err := (Importer{}).importADP(context.Background(), []byte(body), target, &Job{}); err == nil {
			t.Fatal("accepted invalid site")
		}
	}
	for _, body := range []string{`{}`, `{"jobRequisitions":[{"itemID":124}]}`, `{"jobRequisitions":[{"itemID":123},{"itemID":123}]}`, `not json`} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if err := (Importer{client}).importADP(context.Background(), []byte(`{"domain":"acme","active":true,"name":"Acme"}`), target, &Job{}); err == nil {
			t.Fatal("accepted invalid posting")
		}
	}
}
