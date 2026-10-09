package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var ErrURL = errors.New("Enter a direct public HTTPS job posting URL without credentials or a custom port. If automatic import is unavailable, paste the description and check the company and job title")
var numericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var segment = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type Job struct {
	Application     Application `json:"application"`
	ID              string      `json:"id"`
	Provider        string      `json:"provider"`
	URL             string      `json:"url"`
	Title           string      `json:"title"`
	Company         string      `json:"company"`
	Location        string      `json:"location"`
	DescriptionHTML string      `json:"description_html"`
	DescriptionText string      `json:"description_text"`
	SavedAt         time.Time   `json:"saved_at"`
}

type target struct{ provider, company, id, canonical, endpoint string }

func parse(raw string) (target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || strings.HasSuffix(u.Host, ":") {
		return target{}, ErrURL
	}
	host := strings.ToLower(u.Hostname())
	known := host == "myjobs.adp.com" || host == "www.chronograph.pe" || host == "chronograph.pe" || host == "jobs.apple.com" || host == "octane.co" || host == "www.octane.co" || host == "www.brixton.net" || host == "brixton.net" || host == "www.careers.ford.com" || host == "careers.principal.com" || host == "www.linkedin.com" || host == "linkedin.com" || host == "careers.upstart.com" || host == "jobs.ashbyhq.com" || host == "boards.greenhouse.io" || host == "job-boards.greenhouse.io" || host == "boards.eu.greenhouse.io" || host == "job-boards.eu.greenhouse.io" || strings.HasSuffix(host, ".myworkdayjobs.com")
	if !known {
		for _, providerHost := range []string{"myjobs.adp.com", "www.chronograph.pe", "chronograph.pe", "jobs.apple.com", "octane.co", "www.octane.co", "www.brixton.net", "brixton.net", "www.careers.ford.com", "careers.principal.com", "www.linkedin.com", "linkedin.com", "careers.upstart.com", "jobs.ashbyhq.com", "boards.greenhouse.io", "job-boards.greenhouse.io", "boards.eu.greenhouse.io", "job-boards.eu.greenhouse.io"} {
			if strings.HasPrefix(host, providerHost+".") {
				return target{}, ErrURL
			}
		}
		return genericTarget(u)
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	for _, s := range p {
		if !segment.MatchString(s) {
			return target{}, ErrURL
		}
	}
	t := target{}
	switch {
	case host == "myjobs.adp.com":
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(p) != 3 || p[1] != "cx" || p[2] != "job-details" || len(query["reqId"]) != 1 || !numericID.MatchString(query.Get("reqId")) {
			return t, ErrURL
		}
		id := query.Get("reqId")
		t = target{"adp", p[0], id, "https://myjobs.adp.com/" + p[0] + "/cx/job-details?reqId=" + id, "https://myjobs.adp.com/public/staffing/v1/career-site/" + p[0]}
	case host == "jobs.apple.com":
		if len(p) != 4 || !appleLocale.MatchString(p[0]) || p[1] != "details" || !numericID.MatchString(p[2]) {
			return t, ErrURL
		}
		canonical := "https://jobs.apple.com/" + strings.ToLower(strings.Join(p, "/"))
		t = target{"apple", "Apple", p[2], canonical, canonical}
	case host == "www.chronograph.pe" || host == "chronograph.pe":
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(p) != 1 || p[0] != "jobs" || len(query["gh_jid"]) != 1 || !numericID.MatchString(query.Get("gh_jid")) {
			return t, ErrURL
		}
		id := query.Get("gh_jid")
		t = target{"greenhouse", "Chronograph", id, "https://www.chronograph.pe/jobs/?gh_jid=" + id, "https://boards-api.greenhouse.io/v1/boards/chronograph/jobs/" + id}
	case host == "octane.co" || host == "www.octane.co":
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || strings.Join(p, "/") != "o/who-we-are/careers/jobs-open" || len(query["gh_jid"]) != 1 || !numericID.MatchString(query.Get("gh_jid")) {
			return t, ErrURL
		}
		id := query.Get("gh_jid")
		t = target{"greenhouse", "Octane", id, "https://octane.co/o/who-we-are/careers/jobs-open/?gh_jid=" + id, "https://boards-api.greenhouse.io/v1/boards/octanelending/jobs/" + id}
	case host == "www.brixton.net" || host == "brixton.net":
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(p) != 1 || p[0] != "job-detail" || len(query["job"]) != 1 || !segment.MatchString(query.Get("job")) || len(query.Get("job")) > 250 {
			return t, ErrURL
		}
		id := query.Get("job")
		canonical := "https://www.brixton.net/job-detail/?job=" + url.QueryEscape(id)
		t = target{"brixton", "The Brixton Group", id, canonical, canonical}
	case host == "www.careers.ford.com":
		if len(p) != 5 || p[0] != "job" || p[3] != "48560" || !numericID.MatchString(p[4]) {
			return t, ErrURL
		}
		canonical := "https://www.careers.ford.com/" + strings.Join(p, "/")
		t = target{"ford", "Ford Motor Company", p[4], canonical, canonical}
	case host == "careers.principal.com":
		if len(p) != 3 || p[0] != "careers-home" || p[1] != "jobs" || !numericID.MatchString(p[2]) {
			return t, ErrURL
		}
		canonical := "https://careers.principal.com/careers-home/jobs/" + p[2]
		t = target{"principal", "Principal Financial Group", p[2], canonical, canonical}
	case host == "www.linkedin.com" || host == "linkedin.com":
		if len(p) != 3 || p[0] != "jobs" || p[1] != "view" {
			return t, ErrURL
		}
		id := linkedinJobID(p[2])
		if id == "" {
			return t, ErrURL
		}
		t = target{"linkedin", "", id, "https://www.linkedin.com/jobs/view/" + id + "/", "https://www.linkedin.com/jobs-guest/jobs/api/jobPosting/" + id}
	case host == "careers.upstart.com":
		if len(p) != 2 || p[0] != "jobs" || !upstartID.MatchString(p[1]) {
			return t, ErrURL
		}
		canonical := "https://careers.upstart.com/jobs/" + p[1]
		t = target{"upstart", "Upstart", p[1], canonical, canonical}
	case host == "jobs.ashbyhq.com":
		if len(p) != 2 && !(len(p) == 3 && p[2] == "application") {
			return t, ErrURL
		}
		t = target{"ashby", p[0], p[1], "https://" + host + "/" + strings.Join(p[:2], "/"), "https://api.ashbyhq.com/posting-api/job-board/" + p[0]}
	case host == "boards.greenhouse.io" || host == "job-boards.greenhouse.io":
		if len(p) != 3 || p[1] != "jobs" {
			return t, ErrURL
		}
		t = target{"greenhouse", p[0], p[2], "https://job-boards.greenhouse.io/" + p[0] + "/jobs/" + p[2], "https://boards-api.greenhouse.io/v1/boards/" + p[0] + "/jobs/" + p[2]}
	case host == "boards.eu.greenhouse.io" || host == "job-boards.eu.greenhouse.io":
		if len(p) != 3 || p[1] != "jobs" {
			return t, ErrURL
		}
		t = target{"greenhouse", p[0], p[2], "https://job-boards.eu.greenhouse.io/" + p[0] + "/jobs/" + p[2], "https://boards-api.eu.greenhouse.io/v1/boards/" + p[0] + "/jobs/" + p[2]}
	case strings.HasSuffix(host, ".myworkdayjobs.com"):
		labels := strings.Split(host, ".")
		if len(labels) != 4 || !segment.MatchString(labels[0]) || !regexp.MustCompile(`^wd[0-9]+$`).MatchString(labels[1]) {
			return t, ErrURL
		}
		// Locale is optional: /en-US/site/job/location/title_id.
		if len(p) > 0 && regexp.MustCompile(`^[a-z]{2}-[A-Z]{2}$`).MatchString(p[0]) {
			p = p[1:]
		}
		if len(p) < 3 || p[1] != "job" {
			return t, ErrURL
		}
		if p[len(p)-1] == "apply" {
			p = p[:len(p)-1]
		}
		t = target{"workday", labels[0], p[len(p)-1], "https://" + host + "/" + strings.Join(p, "/"), "https://" + host + "/wday/cxs/" + labels[0] + "/" + p[0] + "/" + strings.Join(p[1:], "/")}
	default:
		return t, ErrURL
	}
	return t, nil
}

// NewClient restricts every resolved address and disables proxies and redirects.
// This prevents posting URLs or provider redirects from accessing cluster services.
func NewClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 20, IdleConnTimeout: 60 * time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no public address")
		}
		for _, ip := range ips {
			if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return nil, errors.New("provider resolved to a non-public address")
			}
		}
		var last error
		for _, ip := range ips {
			c, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	return &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("provider redirect refused") }}
}

type Importer struct{ Client *http.Client }

func (i Importer) Fetch(ctx context.Context, raw string) (Job, error) {
	t, err := parse(raw)
	if err != nil {
		return Job{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", t.endpoint, nil)
	if err != nil {
		return Job{}, err
	}
	req.Header.Set("Accept", "application/json")
	if t.provider == "apple" || t.provider == "brixton" || t.provider == "upstart" || t.provider == "linkedin" || t.provider == "ford" || t.provider == "principal" || t.provider == "generic" {
		req.Header.Set("Accept", "text/html")
	}
	req.Header.Set("User-Agent", "JobTracker/0.1")
	res, err := i.Client.Do(req)
	if err != nil {
		return Job{}, fmt.Errorf("fetch posting: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Job{}, fmt.Errorf("provider returned HTTP %d; posting may be unavailable", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil {
		return Job{}, err
	}
	if len(body) > 8<<20 {
		return Job{}, errors.New("provider response exceeds 8 MiB")
	}
	j := Job{Provider: t.provider, Company: t.company, URL: t.canonical}
	switch t.provider {
	case "adp":
		if err := i.importADP(ctx, body, t, &j); err != nil {
			return Job{}, err
		}
	case "apple":
		if err := importApple(body, t, &j); err != nil {
			return Job{}, err
		}
	case "brixton":
		if err := importBrixton(body, t, &j); err != nil {
			return Job{}, err
		}
	case "ford", "principal", "generic":
		if err := importStructuredPosting(body, t, &j); err != nil {
			return Job{}, err
		}
	case "linkedin":
		if err := importLinkedIn(body, &j); err != nil {
			return Job{}, err
		}
	case "upstart":
		if err := importUpstart(body, &j); err != nil {
			return Job{}, err
		}
	case "ashby":
		var data struct {
			Jobs []struct{ Title, Location, DescriptionHTML, DescriptionPlain, JobURL string }
		}
		if err = json.Unmarshal(body, &data); err != nil {
			return Job{}, err
		}
		found := false
		for _, v := range data.Jobs {
			u, e := url.Parse(v.JobURL)
			if e == nil && strings.Trim(u.Path, "/") == t.company+"/"+t.id {
				j.Title = v.Title
				j.Location = v.Location
				j.DescriptionHTML = v.DescriptionHTML
				j.DescriptionText = v.DescriptionPlain
				found = true
				break
			}
		}
		if !found {
			return Job{}, errors.New("posting is no longer listed on the Ashby board")
		}
	case "greenhouse":
		var data struct {
			Name     string
			Title    string
			Content  string
			Location struct{ Name string }
		}
		if err = json.Unmarshal(body, &data); err != nil {
			return Job{}, err
		}
		j.Title = data.Title
		j.Location = data.Location.Name
		j.DescriptionHTML = data.Content
		// Greenhouse returns entity-encoded markup in content.
		if !strings.Contains(j.DescriptionHTML, "<") {
			j.DescriptionHTML = html.UnescapeString(j.DescriptionHTML)
		}
	case "workday":
		var data struct {
			JobPostingInfo struct {
				Title, JobDescription, Location string
				AdditionalLocations             []string
			}
		}
		if err = json.Unmarshal(body, &data); err != nil {
			return Job{}, err
		}
		v := data.JobPostingInfo
		j.Title = v.Title
		j.Location = strings.Join(append([]string{v.Location}, v.AdditionalLocations...), "; ")
		j.DescriptionHTML = v.JobDescription
	}
	if j.DescriptionText == "" {
		j.DescriptionText = PlainText(j.DescriptionHTML)
	}
	if strings.TrimSpace(j.Title) == "" || strings.TrimSpace(j.Company) == "" || strings.TrimSpace(j.DescriptionText) == "" {
		return Job{}, errors.New("provider returned no company, job title, or description; nothing was saved")
	}
	return j, nil
}

func PlainText(source string) string {
	n, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		block := n.Type == html.ElementNode && strings.Contains("|p|div|li|ul|ol|h1|h2|h3|h4|br|section|", "|"+n.Data+"|")
		if block {
			b.WriteByte('\n')
		}
		if n.Type == html.ElementNode && n.Data == "li" {
			b.WriteString("• ")
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if block {
			b.WriteByte('\n')
		}
	}
	walk(n)
	lines := []string{}
	for _, line := range strings.Split(b.String(), "\n") {
		if s := strings.Join(strings.Fields(line), " "); s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n\n")
}
