package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var appleLocale = regexp.MustCompile(`^[a-z]{2}-[a-z]{2}$`)

// Apple embeds JSON in a JSON.parse string literal. Decode data without executing JavaScript.
func importApple(body []byte, t target, j *Job) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return err
	}
	var encoded string
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "script" {
			var script strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					script.WriteString(c.Data)
				}
			}
			source := strings.TrimSpace(script.String())
			const prefix = "window.__staticRouterHydrationData = JSON.parse("
			if strings.HasPrefix(source, prefix) {
				if encoded != "" {
					return errors.New("provider returned ambiguous Apple job data")
				}
				value := strings.TrimSpace(strings.TrimPrefix(source, prefix))
				value = strings.TrimSuffix(value, ";")
				if !strings.HasSuffix(value, ")") {
					return errors.New("provider returned invalid Apple job data")
				}
				if err := json.Unmarshal([]byte(strings.TrimSuffix(value, ")")), &encoded); err != nil {
					return errors.New("provider returned invalid Apple job data")
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
	var state struct {
		LoaderData struct {
			JobDetails struct {
				JobsData struct {
					JobNumber, PostingTitle, JobSummary, Description, Responsibilities                                                string
					MinimumQualifications, PreferredQualifications, KeyQualifications, EducationAndExperience, AdditionalRequirements string
					SelectedLocale                                                                                                    string
					Locations                                                                                                         []struct{ Name, City, StateProvince, CountryName string }
					PostingFooters                                                                                                    []struct {
						Localizations map[string][]struct{ Name, Content string }
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(encoded), &state); err != nil {
		return errors.New("provider returned invalid or missing Apple job data")
	}
	data := state.LoaderData.JobDetails.JobsData
	if data.JobNumber != t.id || strings.TrimSpace(data.PostingTitle) == "" || strings.TrimSpace(data.Description) == "" {
		return errors.New("provider returned no matching complete Apple posting; nothing was saved")
	}
	j.Title = strings.TrimSpace(data.PostingTitle)
	var locations []string
	for _, l := range data.Locations {
		city := l.City
		if strings.TrimSpace(city) == "" {
			city = l.Name
		}
		var parts []string
		for _, part := range []string{city, l.StateProvince, l.CountryName} {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			locations = append(locations, strings.Join(parts, ", "))
		}
	}
	j.Location = strings.Join(locations, "; ")
	var description strings.Builder
	section := func(title, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		description.WriteString("<h2>" + html.EscapeString(title) + "</h2><div>" + strings.ReplaceAll(content, "\n", "<br>") + "</div>")
	}
	section("Summary", data.JobSummary)
	section("Description", data.Description)
	section("Responsibilities", data.Responsibilities)
	section("Key Qualifications", data.KeyQualifications)
	section("Minimum Qualifications", data.MinimumQualifications)
	section("Preferred Qualifications", data.PreferredQualifications)
	section("Education & Experience", data.EducationAndExperience)
	section("Additional Requirements", data.AdditionalRequirements)
	for _, footer := range data.PostingFooters {
		for _, item := range footer.Localizations[data.SelectedLocale] {
			section(item.Name, item.Content)
		}
	}
	j.DescriptionHTML = description.String()
	return nil
}
