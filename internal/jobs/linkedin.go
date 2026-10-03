package jobs

import (
	"bytes"
	"errors"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var linkedinID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var linkedinSlugID = regexp.MustCompile(`^(?:[a-zA-Z0-9]+-)+([1-9][0-9]{0,19})$`)

func linkedinJobID(slug string) string {
	if linkedinID.MatchString(slug) {
		return slug
	}
	if match := linkedinSlugID.FindStringSubmatch(slug); match != nil {
		return match[1]
	}
	return ""
}

func linkedinHasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, value := range strings.Fields(attr.Val) {
				if value == class {
					return true
				}
			}
		}
	}
	return false
}

// importLinkedIn archives only the full guest-page "About the job" markup.
// It does not follow links, execute scripts, or use a LinkedIn account.
func importLinkedIn(body []byte, j *Job) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return err
	}
	var titles, companies, descriptions []*html.Node
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inDescription bool) {
		if linkedinHasClass(n, "top-card-layout__title") {
			titles = append(titles, n)
		}
		if linkedinHasClass(n, "topcard__org-name-link") {
			companies = append(companies, n)
		}
		if linkedinHasClass(n, "topcard__flavor--bullet") && j.Location == "" {
			j.Location = linkedinNodeText(n)
		}
		inDescription = inDescription || linkedinHasClass(n, "description__text")
		if inDescription && linkedinHasClass(n, "show-more-less-html__markup") {
			descriptions = append(descriptions, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inDescription)
		}
	}
	walk(root, false)
	if len(titles) != 1 || len(companies) != 1 || len(descriptions) != 1 {
		return errors.New("LinkedIn did not return a complete public posting; paste its About the job section to save it")
	}
	j.Title, j.Company = linkedinNodeText(titles[0]), linkedinNodeText(companies[0])
	if j.Company == "" {
		return errors.New("LinkedIn returned no company name")
	}
	var content bytes.Buffer
	for child := descriptions[0].FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&content, child); err != nil {
			return err
		}
	}
	j.DescriptionHTML = content.String()
	return nil
}

func linkedinNodeText(n *html.Node) string {
	var content bytes.Buffer
	if html.Render(&content, n) != nil {
		return ""
	}
	return PlainText(content.String())
}
