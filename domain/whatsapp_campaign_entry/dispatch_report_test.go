package whatsapp_campaign_entry

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/campaign"
)

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestDayWindowCoversWholeLocalDaysWithAnExclusiveEnd(t *testing.T) {
	loc := saoPaulo(t)

	w, err := NewDayWindow("2026-09-01", "2026-09-03", loc)
	if err != nil {
		t.Fatalf("NewDayWindow() error = %v", err)
	}

	if want := time.Date(2026, 9, 1, 0, 0, 0, 0, loc); !w.From.Equal(want) {
		t.Fatalf("From = %v, want %v", w.From, want)
	}
	if want := time.Date(2026, 9, 4, 0, 0, 0, 0, loc); !w.To.Equal(want) {
		t.Fatalf("To = %v, want %v so the last day is counted whole", w.To, want)
	}
	if want := []string{"2026-09-01", "2026-09-02", "2026-09-03"}; !reflect.DeepEqual(w.Days(), want) {
		t.Fatalf("Days() = %v, want %v", w.Days(), want)
	}
}

func TestDayWindowRejectsWhatItCannotAnswer(t *testing.T) {
	loc := saoPaulo(t)
	cases := []struct {
		name     string
		from, to string
		loc      *time.Location
		want     error
	}{
		{"a malformed day", "2026-9-1", "2026-09-03", loc, ErrReportWindowInvalid},
		{"an end before the start", "2026-09-03", "2026-09-01", loc, ErrReportWindowInvalid},
		{"no timezone to cut days in", "2026-09-01", "2026-09-03", nil, ErrReportWindowInvalid},
		{"more days than a chart can show", "2026-01-01", "2026-06-01", loc, ErrReportWindowTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewDayWindow(tc.from, tc.to, tc.loc); !errors.Is(err, tc.want) {
				t.Fatalf("NewDayWindow() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDenseDaysFillsSilentDaysWithZeroAndDropsDaysOutsideTheWindow(t *testing.T) {
	w, err := NewDayWindow("2026-09-01", "2026-09-03", saoPaulo(t))
	if err != nil {
		t.Fatal(err)
	}

	got := DenseDays(w, []DayCount{
		{Day: "2026-09-02", Sent: 5, Delivered: 4, Read: 3, Replied: 1},
		{Day: "2026-08-31", Sent: 9},
	})

	want := []DayCount{
		{Day: "2026-09-01"},
		{Day: "2026-09-02", Sent: 5, Delivered: 4, Read: 3, Replied: 1},
		{Day: "2026-09-03"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DenseDays() = %+v, want %+v", got, want)
	}
}

func TestStatusesProvingFollowTheMilestoneRule(t *testing.T) {
	cases := []struct {
		milestone campaign.Milestone
		want      []SendStatus
	}{
		{campaign.MilestoneSent, []SendStatus{SendStatusSent, SendStatusDelivered, SendStatusRead}},
		{campaign.MilestoneDelivered, []SendStatus{SendStatusDelivered, SendStatusRead}},
		{campaign.MilestoneRead, []SendStatus{SendStatusRead}},
		{campaign.MilestoneFailed, []SendStatus{SendStatusFailed}},
	}
	for _, tc := range cases {
		t.Run(string(tc.milestone), func(t *testing.T) {
			if got := StatusesProving(tc.milestone); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("StatusesProving(%s) = %v, want %v", tc.milestone, got, tc.want)
			}
		})
	}
}

func TestDayWindowAcceptsExactlyTheMaximumNumberOfDays(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	last := from.AddDate(0, 0, MaxReportDays-1)

	w, err := NewDayWindow(from.Format("2006-01-02"), last.Format("2006-01-02"), time.UTC)
	if err != nil {
		t.Fatalf("NewDayWindow() error = %v", err)
	}
	if got := len(w.Days()); got != MaxReportDays {
		t.Fatalf("len(Days()) = %d, want %d", got, MaxReportDays)
	}
	if _, err := NewDayWindow(from.Format("2006-01-02"), last.AddDate(0, 0, 1).Format("2006-01-02"), time.UTC); !errors.Is(err, ErrReportWindowTooLong) {
		t.Fatalf("one day past the maximum: error = %v, want %v", err, ErrReportWindowTooLong)
	}
}

func TestDayWindowFingerprintSeparatesRangesAndTimezones(t *testing.T) {
	a, _ := NewDayWindow("2026-09-01", "2026-09-02", time.UTC)
	b, _ := NewDayWindow("2026-09-01", "2026-09-03", time.UTC)
	c, _ := NewDayWindow("2026-09-01", "2026-09-02", saoPaulo(t))

	if a.Fingerprint() == b.Fingerprint() || a.Fingerprint() == c.Fingerprint() {
		t.Fatalf("fingerprints collide: %q %q %q", a.Fingerprint(), b.Fingerprint(), c.Fingerprint())
	}
}

func TestCampaignScopeNeedsACampaign(t *testing.T) {
	if _, err := CampaignScope(""); !errors.Is(err, ErrReportScopeInvalid) {
		t.Fatalf("CampaignScope(\"\") error = %v, want %v", err, ErrReportScopeInvalid)
	}
	scope, err := CampaignScope("c1")
	if err != nil || !scope.IsCampaign() || scope.CampaignID != "c1" {
		t.Fatalf("CampaignScope(c1) = %+v, %v", scope, err)
	}
}

func TestWorkspaceScopeIsBoundedByWorkspaceAndPeriod(t *testing.T) {
	window, _ := NewDayWindow("2026-09-01", "2026-09-30", time.UTC)

	if _, err := WorkspaceScope("", nil, "organic", window); !errors.Is(err, ErrReportScopeInvalid) {
		t.Fatalf("no workspace: error = %v, want %v", err, ErrReportScopeInvalid)
	}
	if _, err := WorkspaceScope("ws1", nil, "organic", DayWindow{}); !errors.Is(err, ErrReportScopeInvalid) {
		t.Fatalf("no period: error = %v, want %v", err, ErrReportScopeInvalid)
	}
	scope, err := WorkspaceScope("ws1", []string{"d2", "d1"}, "organic", window)
	if err != nil || scope.IsCampaign() {
		t.Fatalf("WorkspaceScope() = %+v, %v", scope, err)
	}
}

func TestScopeFingerprintsNeverCollideAcrossWhatTheyCover(t *testing.T) {
	september, _ := NewDayWindow("2026-09-01", "2026-09-30", time.UTC)
	august, _ := NewDayWindow("2026-08-01", "2026-08-31", time.UTC)
	campaign, _ := CampaignScope("c1")
	whole, _ := WorkspaceScope("ws1", nil, "organic", september)
	oneDept, _ := WorkspaceScope("ws1", []string{"d1"}, "organic", september)
	otherOrder, _ := WorkspaceScope("ws1", []string{"d2", "d1"}, "organic", september)
	sameSet, _ := WorkspaceScope("ws1", []string{"d1", "d2"}, "organic", september)
	earlier, _ := WorkspaceScope("ws1", nil, "organic", august)

	seen := map[string]bool{}
	for _, s := range []ReportScope{campaign, whole, oneDept, otherOrder, earlier} {
		if seen[s.Fingerprint()] {
			t.Fatalf("fingerprint %q reused for a different scope", s.Fingerprint())
		}
		seen[s.Fingerprint()] = true
	}
	if otherOrder.Fingerprint() != sameSet.Fingerprint() {
		t.Fatalf("the same departments in another order must share a cache entry: %q vs %q", otherOrder.Fingerprint(), sameSet.Fingerprint())
	}
}
