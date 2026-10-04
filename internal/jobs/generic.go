package jobs

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var hostnameLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// genericTarget accepts DNS names only; NewClient still validates every resolved
// address at connection time. No URL or link supplied by the page is fetched.
func genericTarget(u *url.URL) (target, error) {
	host := strings.ToLower(u.Hostname())
	labels := strings.Split(host, ".")
	if len(u.String()) > 4096 || len(host) > 253 || len(labels) < 2 || net.ParseIP(host) != nil {
		return target{}, ErrURL
	}
	for _, label := range labels {
		if !hostnameLabel.MatchString(label) {
			return target{}, ErrURL
		}
	}
	u.Host = host
	u.Fragment, u.RawFragment = "", ""
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return target{}, ErrURL
	}
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "gclid" || lower == "fbclid" || lower == "msclkid" || lower == "gh_src" {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	u.ForceQuery = false
	canonical := u.String()
	return target{"generic", "", canonical, canonical, canonical}, nil
}
