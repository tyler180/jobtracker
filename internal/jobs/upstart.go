package jobs

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// Upstart renders the posting title and description in separate page elements.
// Only the description is archived, excluding application forms and site chrome.
func importUpstart(body []byte, j *Job) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return err
	}
	var description *html.Node
	count := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "h1" && j.Title == "" {
				var heading bytes.Buffer
				if html.Render(&heading, n) == nil {
					j.Title = PlainText(heading.String())
				}
			}
			for _, attr := range n.Attr {
				if attr.Key == "class" {
					for _, class := range strings.Fields(attr.Val) {
						if class == "job-description" {
							description = n
							count++
							break
						}
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if count != 1 {
		return errors.New("Upstart returned no unique job description; paste the description to save it")
	}
	var content bytes.Buffer
	for child := description.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&content, child); err != nil {
			return err
		}
	}
	j.DescriptionHTML = content.String()
	return nil
}
