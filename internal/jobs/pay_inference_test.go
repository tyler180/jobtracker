package jobs

import "testing"

func TestInferPay(t *testing.T) {
	for _, tc := range []struct{ text, min, max, kind string }{
		{"Salary (permanent) $130,000–$180,000 base + ~10% annual bonus", "130000", "180000", "salary"},
		{"Compensation: $140k-195k Base + Bonus + Benefits", "140000", "195000", "salary"},
		{"Learning budget $5k and database work", "", "", ""},
		{"Compensation: $80 base + tips", "", "", ""},
		{"Annual salary: $130k to $180K", "130000", "180000", "salary"},
		{"Hourly rate: $65.50 - $90.25 per hour", "65.5", "90.25", "hourly"},
		{"Pay $80/hr", "80", "80", "hourly"},
		{"Salary: $150,000 annually", "150000", "150000", "salary"},
		{"Salary: $130000 - $180000\nSalary: $130,000–$180,000", "130000", "180000", "salary"},
		{"Salary: $100,000 - $120,000\nSalary: $140,000 - $180,000", "", "", ""},
		{"Salary: $130000 - $180000\nContract $80/hr", "", "", ""},
		{"Learning budget $5000 and 10% bonus", "", "", ""},
		{"Competitive hourly rate", "", "", ""},
		{"Salary: CAD $130,000 - $180,000", "", "", ""},
		{"Salary: $180000 - $130000", "", "", ""},
	} {
		a := InferPay(tc.text)
		if a.PayMin != tc.min || a.PayMax != tc.max || a.PayType != tc.kind {
			t.Errorf("%s: got %+v", tc.text, a)
		}
	}
}

func TestInterviewTimes(t *testing.T) {
	for _, tc := range []struct {
		time, zone, date string
		valid            bool
	}{
		{"", "", "2026-10-06", true}, {"14:30", "America/Denver", "2026-10-06", true}, {"09:15", "America/New_York", "2026-10-06", true},
		{"25:00", "America/Denver", "2026-10-06", false}, {"2:30 PM", "America/Denver", "2026-10-06", false}, {"14:30", "", "2026-10-06", false}, {"14:30", "Invalid/Zone", "2026-10-06", false}, {"", "America/Denver", "2026-10-06", false},
		{"02:30", "America/Denver", "2027-03-14", false},
	} {
		a := Application{Company: "Acme", Title: "Engineer", Milestones: []InterviewDate{{Stage: "round 1", Date: tc.date, Time: tc.time, Timezone: tc.zone}}}
		if (a.Validate() == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, a.Validate())
		}
	}
	d := InterviewDate{Time: "14:30", Timezone: "America/Denver"}
	if d.TimeLabel() != "2:30 PM (America/Denver)" {
		t.Fatal(d.TimeLabel())
	}
}
