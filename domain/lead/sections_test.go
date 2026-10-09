package lead

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
)

func TestSectionFingerprint_SeparatesEverythingThatChangesTheAnswer(t *testing.T) {
	day := time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC)
	base := SectionQuery{WorkspaceID: "ws", Today: day, Filter: leadFilter(crmfilter.FieldBlocked, crmfilter.OpIsTrue)}
	manager := Viewer{ReadsLeads: true, ReadsAddresses: true, Fields: customfield.Viewer{ReadsSensitive: true}}
	operator := Viewer{ReadsLeads: true}

	fp := func(q SectionQuery, s Section, v Viewer, areas string) string {
		t.Helper()
		key, err := q.Key(s, v, areas).Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	key := fp(base, SectionSummary, manager, "")
	if key != fp(base, SectionSummary, manager, "") {
		t.Fatal("the same question must give the same key")
	}
	variants := map[string]string{
		"another section": fp(base, SectionFacets, manager, ""),
		"another tier":    fp(base, SectionSummary, operator, ""),
		"reshaped areas":  fp(base, SectionSummary, manager, "stamps"),
	}
	nextDay := base
	nextDay.Today = day.Add(2 * time.Hour)
	variants["another day"] = fp(nextDay, SectionSummary, manager, "")
	otherFilter := base
	otherFilter.Filter = leadFilter(crmfilter.FieldBlocked, crmfilter.OpIsFalse)
	variants["another filter"] = fp(otherFilter, SectionSummary, manager, "")
	classified := base
	classified.ClassificationKey = "classificacao"
	variants["a visible classification"] = fp(classified, SectionSummary, manager, "")
	for name, other := range variants {
		if other == key {
			t.Fatalf("%s must change the key", name)
		}
	}

	sameDayLater := base
	sameDayLater.Today = day.Add(-3 * time.Hour)
	if fp(sameDayLater, SectionSummary, manager, "") != key {
		t.Fatal("two moments of one workspace day share a key")
	}
}

func TestClassificationKeyFor_OnlyWhenTheViewerMayReadIt(t *testing.T) {
	defs := []*customfield.Definition{
		{Key: "interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect},
		{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Role: customfield.RoleClassification, Sensitive: true},
	}
	if got := ClassificationKeyFor(Viewer{ReadsLeads: true, Definitions: defs, Fields: customfield.Viewer{ReadsSensitive: true}}); got != "classificacao" {
		t.Fatalf("ClassificationKeyFor(sensitive reader) = %q", got)
	}
	if got := ClassificationKeyFor(Viewer{ReadsLeads: true, Definitions: defs}); got != "" {
		t.Fatalf("a sensitive classification is hidden from a viewer without the permission, got %q", got)
	}
	if got := ClassificationKeyFor(Viewer{ReadsLeads: true, Definitions: defs[:1]}); got != "" {
		t.Fatalf("no classification field means no facet, got %q", got)
	}
}

func TestSectionKeyFingerprint_SeparatesEveryPartAndKeepsAStableKey(t *testing.T) {
	day := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	f := leadFilter(crmfilter.FieldBlocked, crmfilter.OpIsTrue)
	base := SectionKey{Section: "layer", Filter: f, Today: day, Tier: "s:true", Areas: "areas-a", Parts: []string{"14/1:2/3:4", "classificacao"}}
	key, err := base.Fingerprint()
	if err != nil || len(key) != 64 {
		t.Fatalf("Fingerprint() = %q, %v", key, err)
	}
	if again, _ := base.Fingerprint(); again != key {
		t.Fatal("the same question keeps its fingerprint")
	}
	change := map[string]func(k *SectionKey){
		"section":  func(k *SectionKey) { k.Section = "summary" },
		"filter":   func(k *SectionKey) { k.Filter = crmfilter.Filter{} },
		"day":      func(k *SectionKey) { k.Today = day.AddDate(0, 0, 1) },
		"tier":     func(k *SectionKey) { k.Tier = "s:false" },
		"areas":    func(k *SectionKey) { k.Areas = "areas-b" },
		"tiles":    func(k *SectionKey) { k.Parts = []string{"14/1:2/3:5", "classificacao"} },
		"color by": func(k *SectionKey) { k.Parts = []string{"14/1:2/3:4", ""} },
		"one part": func(k *SectionKey) { k.Parts = []string{"14/1:2/3:4"} },
	}
	seen := map[string]string{key: "base"}
	for name, apply := range change {
		k := base
		apply(&k)
		got, err := k.Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		if previous, dup := seen[got]; dup {
			t.Fatalf("changing the %s kept the fingerprint of %s", name, previous)
		}
		seen[got] = name
	}
}

func TestTopOwners_KeepsTheLargestBreaksTiesByIdAndSaysWhenItCut(t *testing.T) {
	owners := func(n int) []OwnerCount {
		out := make([]OwnerCount, 0, n)
		for i := range n {
			out = append(out, OwnerCount{Owner: fmt.Sprintf("owner-%03d", i), Count: int64(i % 7)})
		}
		return out
	}
	cases := []struct {
		name          string
		in            []OwnerCount
		wantLen       int
		wantTruncated bool
	}{
		{"none", nil, 0, false},
		{"under the cap", owners(3), 3, false},
		{"exactly the cap", owners(MaxFacetOwners), MaxFacetOwners, false},
		{"one over the cap", owners(MaxFacetOwners + 1), MaxFacetOwners, true},
		{"far over the cap", owners(MaxFacetOwners * 3), MaxFacetOwners, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			top, truncated := TopOwners(tc.in)
			if len(top) != tc.wantLen || truncated != tc.wantTruncated {
				t.Fatalf("TopOwners() kept %d, truncated %v; want %d, %v", len(top), truncated, tc.wantLen, tc.wantTruncated)
			}
			for i := 1; i < len(top); i++ {
				if top[i-1].Count < top[i].Count || (top[i-1].Count == top[i].Count && top[i-1].Owner > top[i].Owner) {
					t.Fatalf("out of order at %d: %+v then %+v", i, top[i-1], top[i])
				}
			}
			if top == nil {
				t.Fatal("TopOwners() must answer an empty list, never nil")
			}
		})
	}
}

func TestFacetsSection_AlwaysSendsOwnersTruncated(t *testing.T) {
	body, err := json.Marshal(FacetsSection{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"ownersTruncated":false`) {
		t.Fatalf("facets section = %s, want ownersTruncated present", body)
	}
}
