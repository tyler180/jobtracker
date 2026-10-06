package jobs

import "testing"

func TestPayRange(t *testing.T) {
	for _, tc := range []struct {
		min, max, kind string
		valid          bool
	}{
		{"130000", "180000", "salary", true}, {"65.50", "90", "hourly", true}, {"", "90", "hourly", true}, {"65", "", "hourly", true}, {"", "", "", true},
		{"180000", "130000", "salary", false}, {"-1", "90", "hourly", false}, {"0", "90", "hourly", false}, {"NaN", "90", "hourly", false}, {"65.123", "90", "hourly", false}, {"65", "90", "", false}, {"65", "90", "weekly", false},
	} {
		a := Application{Company: "Acme", Title: "Engineer", PayMin: tc.min, PayMax: tc.max, PayType: tc.kind}
		if (a.Validate() == nil) != tc.valid {
			t.Fatalf("%+v", tc)
		}
	}
	store, _ := Open(t.TempDir())
	j, _, _ := store.Save(Job{Company: "Acme", Title: "Engineer", DescriptionText: "original"})
	a := Application{Company: "Acme", Title: "Engineer", PayMin: "65.50", PayMax: "90", PayType: "hourly"}
	if _, err := store.UpdateApplication(j.ID, a); err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(store.dir)
	saved, _ := reopened.Get(j.ID)
	if saved.Application.PayLabel() != "$65.50 – $90 / hour" || saved.DescriptionText != "original" {
		t.Fatal(saved)
	}
}
