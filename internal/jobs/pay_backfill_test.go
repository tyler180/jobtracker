package jobs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackfillPay(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, description string
		application       Application
		want              Application
	}{
		{"salary", "Base salary $130,000 - $180,000", Application{InterviewNotes: "Keep notes", Status: "applied"}, Application{PayMin: "130000", PayMax: "180000", PayType: "salary", InterviewNotes: "Keep notes", Status: "applied"}},
		{"hourly", "$65 - $90 per hour", Application{}, Application{PayMin: "65", PayMax: "90", PayType: "hourly"}},
		{"existing", "Salary $130000 - $180000", Application{PayMin: "150000", PayType: "salary"}, Application{PayMin: "150000", PayType: "salary"}},
		{"maximum only", "Salary $130000 - $180000", Application{PayMax: "160000"}, Application{PayMax: "160000"}},
		{"conflicting type", "Salary $130000 - $180000", Application{PayType: "hourly"}, Application{PayType: "hourly"}},
		{"ambiguous", "Salary $130000 - $180000\nSalary $100000 - $120000", Application{}, Application{}},
		{"absent", "We offer competitive compensation", Application{}, Application{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each case gets its own archive so the one-time migration runs independently.
			store, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			original, _, err := store.Save(Job{Title: "Engineer", Company: "Acme", DescriptionText: tc.description, DescriptionHTML: "<p>archived</p>", Application: tc.application})
			if err != nil {
				t.Fatal(err)
			}
			count, err := store.BackfillPay()
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 0
			if !reflect.DeepEqual(tc.application, tc.want) {
				wantCount = 1
			}
			if count != wantCount {
				t.Fatalf("updated %d, want %d", count, wantCount)
			}
			saved, err := store.Get(original.ID)
			if err != nil {
				t.Fatal(err)
			}
			expected := original
			expected.Application = tc.want
			if !reflect.DeepEqual(saved, expected) {
				t.Fatalf("saved entry changed unexpectedly: %#v", saved)
			}
			// A deliberate clear after completion must survive future restarts.
			saved.Application = tc.application
			if err := store.write(saved); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if count, err := reopened.BackfillPay(); err != nil || count != 0 {
				t.Fatalf("repeat: %d %v", count, err)
			}
			after, err := reopened.Get(saved.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, saved) {
				t.Fatal("repeat changed entry")
			}
		})
	}
	// Invalid archives must not be marked complete; correcting them allows retry.
	bad := filepath.Join(store.dir, "broken.json")
	if err := os.WriteFile(bad, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackfillPay(); err == nil {
		t.Fatal("expected corrupt archive error")
	}
	if _, err := os.Stat(filepath.Join(store.dir, ".pay-backfill-v1")); !os.IsNotExist(err) {
		t.Fatal("failed migration marked complete")
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackfillPay(); err != nil {
		t.Fatal(err)
	}
}
