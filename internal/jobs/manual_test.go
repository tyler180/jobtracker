package jobs

import "testing"

func TestManualMetadata(t *testing.T) {
	source := "https://careers.upstart.com/jobs/senior-data-platform-engineer-076371c4-e43b-4489-8132-a304724cc54f"
	for _, c := range []struct{ url, description, company, title string }{
		{source, "", "Upstart", "Senior Data Platform Engineer"},
		{"https://example.com/job", "# Staff Platform Engineer\n\nAbout Acme\nBuild services.", "Acme", "Staff Platform Engineer"},
		{"https://example.com/job", "Company: Acme\nJob title: Senior Engineer\nBuild services.", "Acme", "Senior Engineer"},
		{source, "About Upstart\nAs a Senior Data Platform Engineer joining this team, you will focus on:\nBuild things.", "Upstart", "Senior Data Platform Engineer"},
		{"https://example.com/job", "About the team\nOur engineers build services.", "", ""},
	} {
		got, err := ManualPreview(c.url, c.description)
		if err != nil || got.Company != c.company || got.Title != c.title {
			t.Fatalf("%q: company=%q title=%q err=%v", c.description, got.Company, got.Title, err)
		}
	}
	got, err := Manual(source, "My company", "My title", "Senior Engineer\nAbout Upstart\nBuild services.")
	if err != nil || got.Company != "My company" || got.Title != "My title" {
		t.Fatal("explicit metadata was overwritten")
	}
	if _, err := Manual(source, "", "", ""); err == nil {
		t.Fatal("saved empty description")
	}
}
