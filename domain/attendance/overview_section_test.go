package attendance

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestParseSectionAcceptsEverySection(t *testing.T) {
	for _, section := range Sections() {
		got, ok := ParseSection(string(section))
		if !ok || got != section {
			t.Fatalf("ParseSection(%q) = %q, %v, want %q, true", section, got, ok, section)
		}
	}
}

func TestParseSectionRejectsUnknownNames(t *testing.T) {
	for _, raw := range []string{"", "overview", "queue", "SUMMARY", " summary"} {
		if _, ok := ParseSection(raw); ok {
			t.Fatalf("ParseSection(%q) ok = true, want false", raw)
		}
	}
}

func fingerprintFilter() OverviewFilter {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 24, 23, 59, 59, 0, time.UTC)
	return OverviewFilter{
		DateFrom:     &from,
		DateTo:       &to,
		DepartmentID: "d1",
		MemberID:     "m1",
		CampaignID:   "c1",
		CampaignType: "whatsapp",
		Channel:      "whatsapp",
		IncludeAI:    true,
		TrendBuckets: 13,
		RankMetric:   "resolved",
	}
}

func TestFingerprintIsStableForEqualFilters(t *testing.T) {
	a := fingerprintFilter()
	b := fingerprintFilter()
	for _, section := range Sections() {
		if a.Fingerprint(section) != b.Fingerprint(section) {
			t.Fatalf("Fingerprint(%s) differs for equal filters", section)
		}
	}
}

func TestFingerprintSeparatesSections(t *testing.T) {
	filter := fingerprintFilter()
	seen := map[string]Section{}
	for _, section := range Sections() {
		key := filter.Fingerprint(section)
		if other, dup := seen[key]; dup {
			t.Fatalf("Fingerprint(%s) == Fingerprint(%s)", section, other)
		}
		seen[key] = section
	}
}

func TestFingerprintChangesWithEveryScopingField(t *testing.T) {
	later := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	mutations := map[string]func(*OverviewFilter){
		"date_from":     func(f *OverviewFilter) { f.DateFrom = &later },
		"date_to":       func(f *OverviewFilter) { f.DateTo = nil },
		"department_id": func(f *OverviewFilter) { f.DepartmentID = "d2" },
		"member_id":     func(f *OverviewFilter) { f.MemberID = "" },
		"campaign_id":   func(f *OverviewFilter) { f.CampaignID = "c2" },
		"campaign_type": func(f *OverviewFilter) { f.CampaignType = "" },
		"channel":       func(f *OverviewFilter) { f.Channel = "instagram" },
	}
	base := fingerprintFilter()
	for name, mutate := range mutations {
		changed := fingerprintFilter()
		mutate(&changed)
		for _, section := range Sections() {
			if base.Fingerprint(section) == changed.Fingerprint(section) {
				t.Fatalf("Fingerprint(%s) ignored a change to %s", section, name)
			}
		}
	}
}

func TestFingerprintIgnoresFieldsTheSectionDoesNotRead(t *testing.T) {
	cases := []struct {
		field   string
		mutate  func(*OverviewFilter)
		readers []Section
	}{
		{field: "rank_metric", mutate: func(f *OverviewFilter) { f.RankMetric = "volume" }, readers: []Section{SectionTeam}},
		{field: "include_ai", mutate: func(f *OverviewFilter) { f.IncludeAI = false }, readers: []Section{SectionTeam}},
		{field: "trend_buckets", mutate: func(f *OverviewFilter) { f.TrendBuckets = 6 }, readers: []Section{SectionTrend}},
		{field: "quality", mutate: func(f *OverviewFilter) { f.Quality = QualityPolicy{Enabled: true} }},
	}
	base := fingerprintFilter()
	for _, tc := range cases {
		changed := fingerprintFilter()
		tc.mutate(&changed)
		for _, section := range Sections() {
			same := base.Fingerprint(section) == changed.Fingerprint(section)
			reads := slices.Contains(tc.readers, section)
			if reads && same {
				t.Fatalf("Fingerprint(%s) ignored %s, which it reads", section, tc.field)
			}
			if !reads && !same {
				t.Fatalf("Fingerprint(%s) changed with %s, which it never reads", section, tc.field)
			}
		}
	}
}

func TestFingerprintComparesDatesAsInstants(t *testing.T) {
	utc := fingerprintFilter()
	shifted := fingerprintFilter()
	from := utc.DateFrom.In(time.FixedZone("BRT", -3*3600))
	shifted.DateFrom = &from
	if utc.Fingerprint(SectionSummary) != shifted.Fingerprint(SectionSummary) {
		t.Fatalf("Fingerprint() treated the same instant in two zones as different")
	}
}

func TestOverviewKeepsItsWireShape(t *testing.T) {
	raw, err := json.Marshal(Overview{})
	if err != nil {
		t.Fatalf("json.Marshal(Overview{}) error = %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	want := []string{
		"filter", "kpis", "hourly", "status_distribution", "by_department", "by_member",
		"frt", "ai", "queue", "occupancy", "live", "channel_mix", "messaging", "reopen",
		"finished_by_source", "stages", "period", "projections", "standing", "trend",
		"revenue", "backlog_xray", "quality", "team_ranking", "rework", "generated_at",
		"definitions",
	}
	got := make([]string, 0, len(decoded))
	for key := range decoded {
		got = append(got, key)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("Overview JSON keys = %v, want %v", got, want)
	}
}

func TestSectionResponsesCarryOnlyTheirOwnKeys(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{name: "trend", value: TrendSection{}, want: []string{"trend"}},
		{name: "stages", value: StagesSection{}, want: []string{"stages"}},
		{name: "backlog", value: BacklogSection{}, want: []string{"backlog_xray"}},
		{name: "rework", value: ReworkSection{}, want: []string{"rework"}},
		{name: "team", value: TeamSection{}, want: []string{"by_department", "by_member", "team_ranking"}},
		{name: "live", value: LiveSection{}, want: []string{"live", "occupancy", "queue"}},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatalf("json.Marshal(%s) error = %v", tc.name, err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(%s) error = %v", tc.name, err)
		}
		got := make([]string, 0, len(decoded))
		for key := range decoded {
			got = append(got, key)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s JSON keys = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLiveIsASectionOfItsOwn(t *testing.T) {
	got, ok := ParseSection("live")
	if !ok || got != SectionLive {
		t.Fatalf("ParseSection(live) = %q, %v, want %q, true", got, ok, SectionLive)
	}
}
