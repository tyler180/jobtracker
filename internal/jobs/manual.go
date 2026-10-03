package jobs

import (
	"errors"
	"net/url"
	"strings"
)

// Manual creates a snapshot from pasted text without fetching the source URL.
func Manual(raw, company, title, description string) (Job, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || strings.HasSuffix(u.Host, ":") {
		return Job{}, errors.New("Enter a direct HTTPS posting URL without credentials or a custom port")
	}
	company, title = strings.TrimSpace(company), strings.TrimSpace(title)
	if company == "" || title == "" || len(company) > 300 || len(title) > 300 {
		return Job{}, errors.New("Enter a company and job title, each at most 300 bytes, when pasting a description")
	}
	if strings.TrimSpace(description) == "" || len(description) > 64000 {
		return Job{}, errors.New("Paste a job description of at most 64,000 bytes")
	}
	u.Host = strings.ToLower(u.Host)
	u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
	u.ForceQuery = false
	return Job{Provider: "manual", URL: u.String(), Company: company, Title: title, DescriptionText: description}, nil
}
