package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// importStructuredPosting reads inline JobPosting data only. Schema context URLs
// and other links in the document are never fetched.
func importStructuredPosting(body []byte, t target, j *Job) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return err
	}
	var candidates []map[string]json.RawMessage
	var collect func(json.RawMessage, int) error
	collect = func(raw json.RawMessage, depth int) error {
		if depth > 32 {
			return errors.New("job data is too deeply nested")
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			return errors.New("empty structured job data")
		}
		if raw[0] == '[' {
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				return err
			}
			for _, item := range items {
				if err := collect(item, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		var node map[string]json.RawMessage
		if err := json.Unmarshal(raw, &node); err != nil {
			return err
		}
		var kind string
		var kinds []string
		if json.Unmarshal(node["@type"], &kind) == nil {
			kinds = []string{kind}
		} else {
			_ = json.Unmarshal(node["@type"], &kinds)
		}
		for _, kind := range kinds {
			if kind == "JobPosting" {
				candidates = append(candidates, node)
				break
			}
		}
		if graph, ok := node["@graph"]; ok {
			return collect(graph, depth+1)
		}
		return nil
	}
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "script" {
			for _, attr := range n.Attr {
				if attr.Key == "type" && strings.EqualFold(strings.TrimSpace(attr.Val), "application/ld+json") {
					var data strings.Builder
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.TextNode {
							data.WriteString(c.Data)
						}
					}
					if err := collect(json.RawMessage(data.String()), 0); err != nil {
						return errors.New("provider returned invalid structured job data")
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return err
	}
	var match map[string]json.RawMessage
	for _, candidate := range candidates {
		var rawURL string
		if raw, exists := candidate["url"]; exists && json.Unmarshal(raw, &rawURL) != nil {
			continue
		}
		if strings.TrimSpace(rawURL) == "" {
			// Some public pages omit the optional URL field. Only a single
			// JobPosting can identify the requested page in that case.
			if t.provider != "generic" || len(candidates) != 1 {
				continue
			}
		} else {
			base, _ := url.Parse(t.canonical)
			reference, err := url.Parse(rawURL)
			if err != nil {
				continue
			}
			posting, err := parse(base.ResolveReference(reference).String())
			if err != nil || posting.provider != t.provider || posting.id != t.id {
				continue
			}
		}
		if match != nil {
			return errors.New("provider returned ambiguous job data; nothing was saved")
		}
		match = candidate
	}
	if match == nil {
		return errors.New("provider returned no matching job data; paste the description to save it")
	}
	if json.Unmarshal(match["title"], &j.Title) != nil || json.Unmarshal(match["description"], &j.DescriptionHTML) != nil {
		return errors.New("provider returned invalid job title or description")
	}
	var organization struct{ Name string }
	if raw, ok := match["hiringOrganization"]; ok {
		if err := json.Unmarshal(raw, &organization); err != nil {
			return errors.New("provider returned invalid hiring organization")
		}
		if name := strings.TrimSpace(organization.Name); name != "" {
			j.Company = name
		}
	}
	location, err := structuredLocations(match["jobLocation"])
	if err != nil {
		return err
	}
	j.Title = strings.TrimSpace(j.Title)
	j.Location = location
	return nil
}

func structuredLocations(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var places []json.RawMessage
	if bytes.TrimSpace(raw)[0] == '[' {
		if err := json.Unmarshal(raw, &places); err != nil {
			return "", errors.New("provider returned invalid job locations")
		}
	} else {
		places = []json.RawMessage{raw}
	}
	var locations []string
	for _, place := range places {
		var location struct {
			Address struct {
				AddressLocality, AddressRegion string
				AddressCountry                 json.RawMessage
			}
		}
		if err := json.Unmarshal(place, &location); err != nil {
			return "", errors.New("provider returned invalid job location")
		}
		a := location.Address
		var country string
		if len(a.AddressCountry) != 0 && json.Unmarshal(a.AddressCountry, &country) != nil {
			var object struct{ Name string }
			if err := json.Unmarshal(a.AddressCountry, &object); err != nil {
				return "", errors.New("provider returned invalid location country")
			}
			country = object.Name
		}
		var parts []string
		for _, part := range []string{a.AddressLocality, a.AddressRegion, country} {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		if text := strings.Join(parts, ", "); text != "" {
			locations = append(locations, text)
		}
	}
	return strings.Join(locations, "; "), nil
}
