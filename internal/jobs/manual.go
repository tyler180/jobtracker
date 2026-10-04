package jobs

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Manual creates a snapshot from pasted text without fetching the source URL.
func Manual(raw, company, title, description string) (Job, error) {
	j, err := ManualPreview(raw, description)
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(company) == "" {
		company = j.Company
	}
	if strings.TrimSpace(title) == "" {
		title = j.Title
	}
	company, title = strings.TrimSpace(company), strings.TrimSpace(title)
	if company == "" || title == "" || len(company) > 300 || len(title) > 300 {
		return Job{}, errors.New("Could not identify the company and job title. Enter them above before saving")
	}
	if strings.TrimSpace(description) == "" {
		return Job{}, errors.New("Paste the job description before saving")
	}
	j.Company, j.Title = company, title
	return j, nil
}

// ManualPreview infers editable metadata without fetching the source URL.
// Missing fields remain blank rather than guessing from arbitrary prose.
func ManualPreview(raw, description string) (Job, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || strings.HasSuffix(u.Host, ":") {
		return Job{}, errors.New("Enter a direct HTTPS posting URL without credentials or a custom port")
	}
	if len(description) > 64000 {
		return Job{}, errors.New("Paste a job description of at most 64,000 bytes")
	}
	u.Host = strings.ToLower(u.Host)
	u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
	u.ForceQuery = false
	if target, err := parse(raw); err == nil && (target.provider == "linkedin" || target.provider == "ford" || target.provider == "principal" || target.provider == "generic") {
		// Provider aliases and manual imports share the same snapshot identity.
		u, _ = url.Parse(target.canonical)
	}
	company, title := inferDescription(description)
	if t, err := parse(raw); err == nil && (t.provider == "ford" || t.provider == "principal") && company == "" {
		company = t.company
	}
	if t, err := parse(raw); err == nil && t.provider == "upstart" {
		if company == "" {
			company = "Upstart"
		}
		if title == "" {
			slug := upstartID.ReplaceAllString(t.id, "")
			words := strings.Split(slug, "-")
			for i, word := range words {
				if word != "" {
					words[i] = strings.ToUpper(word[:1]) + word[1:]
				}
			}
			title = strings.Join(words, " ")
		}
	}
	return Job{Provider: "manual", URL: u.String(), Company: company, Title: title, DescriptionText: description}, nil
}

var upstartID = regexp.MustCompile(`-[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)
var titleLabel = regexp.MustCompile(`(?i)^(?:job title|position|role|title)\s*:\s*(.+)$`)
var companyLabel = regexp.MustCompile(`(?i)^(?:company(?: name)?\s*:\s*|about\s+)(.+)$`)
var roleHeading = regexp.MustCompile(`(?i)\b(engineer|developer|manager|analyst|architect|designer|scientist|specialist|director|administrator|consultant)\b`)
var roleAbbreviation = regexp.MustCompile(`(?i)^(?:sr|jr)\.\s+`)
var proseRole = regexp.MustCompile(`(?i)\bas (?:a|an) ([^\n,.]{1,150}?) (?:joining|at|you will)\b`)

func inferDescription(description string) (company, title string) {
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.Trim(strings.TrimSpace(line), "#* ")
		if m := titleLabel.FindStringSubmatch(line); m != nil && len(m[1]) <= 300 && title == "" {
			title = strings.TrimSpace(m[1])
		}
		if m := companyLabel.FindStringSubmatch(line); m != nil && len(m[1]) <= 80 && company == "" {
			v := strings.TrimSpace(m[1])
			if !strings.EqualFold(v, "the role") && !strings.EqualFold(v, "the job") && !strings.EqualFold(v, "the team") && !strings.EqualFold(v, "the company") && !strings.EqualFold(v, "us") && !strings.EqualFold(v, "you") {
				company = v
			}
		}
	}
	if title == "" {
		for _, line := range lines {
			line = strings.Trim(strings.TrimSpace(line), "#* ")
			if line == "" || strings.EqualFold(line, "Kiosk mode") || strings.EqualFold(line, "About the job") {
				continue
			}
			if len(line) <= 150 && roleHeading.MatchString(line) && !strings.ContainsAny(roleAbbreviation.ReplaceAllString(line, ""), ".:!?") {
				title = line
			}
			break
		}
	}
	if title == "" {
		if m := proseRole.FindStringSubmatch(description); m != nil && roleHeading.MatchString(m[1]) {
			title = strings.TrimSpace(m[1])
		}
	}
	return company, title
}
