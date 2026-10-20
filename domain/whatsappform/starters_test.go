package whatsappform

import "testing"

func TestEveryStarterIsAValidFormThatCompiles(t *testing.T) {
	starters := Starters()
	if len(starters) != 7 {
		t.Fatalf("starters = %d, want the seven of the plan", len(starters))
	}
	seen := map[string]bool{}
	for _, s := range starters {
		t.Run(s.Key, func(t *testing.T) {
			if seen[s.Key] || s.Name == "" || s.Description == "" {
				t.Fatalf("starter %+v needs a unique key, a name and a description", s)
			}
			seen[s.Key] = true
			if problems := Validate(s.Document, nil); len(problems) > 0 {
				t.Fatalf("problems: %+v", problems)
			}
			if _, err := Compile(s.Document, nil); err != nil {
				t.Fatalf("compile: %v", err)
			}
		})
	}
}

func TestStarterLookupReturnsAnIndependentCopy(t *testing.T) {
	first, ok := StarterByKey("lead_capture")
	if !ok {
		t.Fatal("lead_capture is a starter")
	}
	first.Document.Screens[0].Title = "changed"
	second, _ := StarterByKey("lead_capture")
	if second.Document.Screens[0].Title == "changed" {
		t.Fatal("editing a starter copy must not change the catalog")
	}
	if _, ok := StarterByKey("nope"); ok {
		t.Fatal("unknown starters are not found")
	}
}

func TestBlankStarterIsTheSmallestValidForm(t *testing.T) {
	blank := BlankDocument()
	if problems := Validate(blank, nil); len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}
}
