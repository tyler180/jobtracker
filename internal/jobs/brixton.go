package jobs

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// Brixton's JSON-LD embeds unescaped HTML quotes; use the visible posting instead.
func importBrixton(body []byte, t target, j *Job) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return err
	}
	var details []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if linkedinHasClass(n, "job-details-inner") {
			details = append(details, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(details) != 1 {
		return errors.New("Brixton did not return a complete public posting; paste the job description")
	}
	fields := map[string]string{}
	for c := details[0].FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "div" {
			continue
		}
		key, value, ok := strings.Cut(linkedinNodeText(c), ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	id := fields["Job ID"]
	if id == "" || !strings.HasSuffix(t.id, "-"+id) || fields["Job Title"] == "" {
		return errors.New("Brixton returned missing or mismatched job details")
	}
	var description *html.Node
	for n := details[0].NextSibling; n != nil; n = n.NextSibling {
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) == "" {
			continue
		}
		if n.Type == html.ElementNode && n.Data == "span" {
			description = n
		}
		break
	}
	if description == nil {
		return errors.New("Brixton returned no job description")
	}
	var content bytes.Buffer
	for c := description.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&content, c); err != nil {
			return err
		}
	}
	j.Title = fields["Job Title"]
	j.Location = strings.Join(nonempty(fields["City"], fields["State"]), ", ")
	j.DescriptionHTML = content.String()
	return nil
}

func nonempty(values ...string) []string {
	var result []string
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
