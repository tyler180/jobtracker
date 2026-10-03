package jobs

import "testing"

func TestApplicationDates(t *testing.T) {
	const today = "2026-10-03"
	a := Application{Company: "Acme", Title: "Engineer", Status: "applied"}
	a.DefaultDates(today)
	if a.AppliedDate != today || a.ResponseDate != "" || a.ScreeningDate != "" {
		t.Fatalf("Invented response or interview: %+v", a)
	}
	a.Status = "interview"
	a.InterviewStage = "initial screening"
	a.DefaultDates(today)
	if a.ResponseDate != today || a.ScreeningDate != today || a.Round1Date != "" {
		t.Fatalf("Unexpected interview defaults: %+v", a)
	}
	a.InterviewStage = "round 1"
	a.DefaultDates("2026-10-09")
	if a.Round1Date != "2026-10-09" || a.ScreeningDate != today || a.ResponseDate != today || a.AppliedDate != today {
		t.Fatalf("Dates overwritten: %+v", a)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"2026-02-30", "2026-13-01", "10/03/2026"} {
		a.Round2Date = invalid
		if err := a.Validate(); err == nil {
			t.Fatalf("Accepted invalid date %s", invalid)
		}
	}
}
