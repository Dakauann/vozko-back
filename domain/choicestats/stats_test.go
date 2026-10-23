package choicestats

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSummarizeTurnsCountersIntoRatesInBasisPoints(t *testing.T) {
	rows := []CountRow{
		{OptionID: OptionTotal, Shown: 1742, Clicks: 1394},
		{OptionID: "a", Shown: 1742, Clicks: 244},
		{OptionID: "b", Shown: 1700, Clicks: 1150},
		{OptionID: OptionFreeText, Clicks: 87},
		{OptionID: "gone", Shown: 10, Clicks: 3},
	}
	got := Summarize(rows, []OfferedOption{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	want := PromptStats{
		Sends: 1742, ClickedPrompts: 1394, ClickRate: 8002,
		FreeText: Rate{Count: 87, Rate: 499},
		Options: []OptionStats{
			{OptionID: "a", Shown: 1742, Clicks: 244, CTR: 1401},
			{OptionID: "b", Shown: 1700, Clicks: 1150, CTR: 6765},
			{OptionID: "c", Shown: 0, Clicks: 0, CTR: 0},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSummarizeWithoutAnOrderListsEveryCountedOptionByID(t *testing.T) {
	rows := []CountRow{
		{OptionID: "template_button:1", Shown: 4, Clicks: 1},
		{OptionID: OptionTotal, Shown: 4, Clicks: 1},
		{OptionID: "template_button:0", Shown: 4},
	}
	got := Summarize(rows, nil)
	ids := []string{}
	for _, o := range got.Options {
		ids = append(ids, o.OptionID)
	}
	if !reflect.DeepEqual(ids, []string{"template_button:0", "template_button:1"}) {
		t.Fatalf("options = %v", ids)
	}
	if got.Options == nil || got.FreeText.Count != 0 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestSummarizeCarriesTheTrackedFlagOfAnOfferedOption(t *testing.T) {
	tracked, untracked := true, false
	got := Summarize(nil, []OfferedOption{{ID: "template_button:0", Tracked: &tracked}, {ID: "template_url:1", Tracked: &untracked}})
	if got.Options[0].Tracked == nil || !*got.Options[0].Tracked || got.Options[1].Tracked == nil || *got.Options[1].Tracked {
		t.Fatalf("tracked flags = %+v", got.Options)
	}
	if got.Sends != 0 || got.ClickRate != 0 {
		t.Fatalf("an empty summary = %+v", got)
	}
}

func TestNodeSummariesOmitNodesWithoutSendsOrOffTheCanvas(t *testing.T) {
	rows := []CountRow{
		{NodeID: "ask", OptionID: OptionTotal, Shown: 10, Clicks: 5},
		{NodeID: "ask", OptionID: "sim", Shown: 10, Clicks: 5},
		{NodeID: "deleted", OptionID: OptionTotal, Shown: 3},
		{NodeID: "quiet", OptionID: OptionFreeText, Clicks: 0},
	}
	canvas := []CanvasNode{
		{NodeID: "ask", Options: []OfferedOption{{ID: "sim"}, {ID: "nao"}}},
		{NodeID: "quiet", Options: []OfferedOption{{ID: "x"}}},
		{NodeID: "tpl"},
	}
	got := SummarizeNodes(rows, canvas)
	if len(got) != 1 || got[0].NodeID != "ask" || len(got[0].Options) != 2 || got[0].ClickRate != 5000 {
		t.Fatalf("nodes = %+v", got)
	}
}

func TestPeriodsAreWholeWorkspaceDaysWithinTheCounterRetention(t *testing.T) {
	saoPaulo := time.FixedZone("BRT", -3*3600)
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		from, to string
		want     error
		first    string
		last     string
	}{
		{"a chip period", "2026-09-23", "2026-10-21", nil, "2026-09-23", "2026-10-21"},
		{"the full retention", "2025-09-17", "2026-10-21", nil, "2025-09-17", "2026-10-21"},
		{"past the retention", "2025-09-16", "2026-10-21", ErrInvalidPeriod, "", ""},
		{"missing to", "2026-09-23", "", ErrInvalidPeriod, "", ""},
		{"reversed", "2026-10-21", "2026-09-23", ErrInvalidPeriod, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPeriod(tc.from, tc.to, saoPaulo)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err == nil && (p.FirstDay() != tc.first || p.LastDay() != tc.last || p.Timezone() != "BRT") {
				t.Fatalf("period %s..%s %s", p.FirstDay(), p.LastDay(), p.Timezone())
			}
		})
	}

	lifetime, err := OptionalPeriod("", "", time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), now, saoPaulo)
	if err != nil || lifetime.FirstDay() != "2026-10-01" || lifetime.LastDay() != "2026-10-21" {
		t.Fatalf("lifetime = %s..%s, %v (today in Sao Paulo is the 21st)", lifetime.FirstDay(), lifetime.LastDay(), err)
	}
	old, err := OptionalPeriod("", "", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), now, saoPaulo)
	if err != nil || old.FirstDay() != "2025-09-17" {
		t.Fatalf("an old source starts at the retention edge, got %s, %v", old.FirstDay(), err)
	}
	if len(old.window.Days()) != MaxPeriodDays {
		t.Fatalf("lifetime covers %d days, want %d", len(old.window.Days()), MaxPeriodDays)
	}
	if _, err := OptionalPeriod("2026-10-01", "", now, now, saoPaulo); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("half a range = %v", err)
	}
	if _, err := NewPeriod("2026-10-01", "2026-10-02", nil); !errors.Is(err, ErrZoneUnknown) {
		t.Fatalf("no zone = %v", err)
	}
	if !lifetime.FirstDate().Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !lifetime.LastDate().Equal(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("stored dates = %v %v", lifetime.FirstDate(), lifetime.LastDate())
	}
}
