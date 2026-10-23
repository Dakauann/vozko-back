package emailsending

import (
	"testing"
	"time"
)

var pacingNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func TestEverySendThatIsNotACampaignSendIsAPrioritySend(t *testing.T) {
	cases := []struct {
		kind RunKind
		want SendLane
	}{
		{kind: RunCampaign, want: SendLaneCampaign},
		{kind: RunReply, want: SendLanePriority},
		{kind: RunTriggered, want: SendLanePriority},
		{kind: RunTest, want: SendLanePriority},
		{kind: RunKind("outreach"), want: SendLanePriority},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := SendLaneOf(tc.kind); got != tc.want {
				t.Fatalf("SendLaneOf(%s) = %s, want %s", tc.kind, got, tc.want)
			}
		})
	}
}

func TestTheSandboxQuotaIsUsedUntilTheAccountIsRead(t *testing.T) {
	q := SandboxSendQuota(pacingNow)
	if q.TokensPerSecond() != 1 || q.DailyQuota() != 200 || q.ProductionAccess || !q.ReadAt.Equal(pacingNow) || q.Read {
		t.Fatalf("sandbox quota = %+v", q)
	}
}

func TestTokensPerSecondRoundDownTheProviderRate(t *testing.T) {
	cases := []struct {
		rate float64
		want int64
	}{
		{rate: 1, want: 1},
		{rate: 14, want: 14},
		{rate: 14.9, want: 14},
		{rate: 0.5, want: 1},
		{rate: 0, want: 1},
		{rate: -1, want: 1},
	}
	for _, tc := range cases {
		q := SendQuota{MaxSendRate: tc.rate}
		if got := q.TokensPerSecond(); got != tc.want {
			t.Errorf("TokensPerSecond(%v) = %d, want %d", tc.rate, got, tc.want)
		}
	}
}

func TestFivePercentOfTheDailyQuotaStaysReservedForPrioritySends(t *testing.T) {
	cases := []struct {
		name  string
		daily float64
		want  int64
	}{
		{name: "sandbox", daily: 200, want: 10},
		{name: "production", daily: 50000, want: 2500},
		{name: "rounds up", daily: 101, want: 6},
		{name: "unlimited", daily: -1, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (SendQuota{Max24HourSend: tc.daily}).PriorityReserve(); got != tc.want {
				t.Fatalf("PriorityReserve = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDailyRoomKeepsTheReserveForPrioritySends(t *testing.T) {
	quota := SendQuota{MaxSendRate: 14, Max24HourSend: 200, SentLast24Hours: 180, ReadAt: pacingNow}
	cases := []struct {
		name  string
		quota SendQuota
		lane  SendLane
		ours  int64
		want  bool
	}{
		{name: "campaign below the reserve line", quota: quota, lane: SendLaneCampaign, ours: 9, want: true},
		{name: "campaign at the reserve line", quota: quota, lane: SendLaneCampaign, ours: 10, want: false},
		{name: "priority inside the reserve", quota: quota, lane: SendLanePriority, ours: 19, want: true},
		{name: "priority at the quota", quota: quota, lane: SendLanePriority, ours: 20, want: false},
		{name: "fractional provider count rounds up", quota: SendQuota{Max24HourSend: 200, SentLast24Hours: 189.2}, lane: SendLaneCampaign, ours: 0, want: false},
		{name: "unlimited quota", quota: SendQuota{Max24HourSend: -1, SentLast24Hours: 1e9}, lane: SendLaneCampaign, ours: 1e6, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.quota.DailyRoom(tc.lane, tc.ours); got != tc.want {
				t.Fatalf("DailyRoom = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAQuotaReadOlderThanFifteenMinutesIsStale(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{name: "just read", age: 0, want: true},
		{name: "at the limit", age: SendQuotaStaleAfter, want: true},
		{name: "past the limit", age: SendQuotaStaleAfter + time.Second, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := SendQuota{ReadAt: pacingNow.Add(-tc.age)}
			if got := q.Fresh(pacingNow); got != tc.want {
				t.Fatalf("Fresh = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheQuotaAlertFiresAtEightyPercentOfTheDailyQuota(t *testing.T) {
	cases := []struct {
		name string
		q    SendQuota
		ours int64
		bp   int64
		want bool
	}{
		{name: "quiet", q: SendQuota{Max24HourSend: 1000, SentLast24Hours: 100}, bp: 1000, want: false},
		{name: "just under", q: SendQuota{Max24HourSend: 1000, SentLast24Hours: 799}, bp: 7990, want: false},
		{name: "at eighty", q: SendQuota{Max24HourSend: 1000, SentLast24Hours: 790}, ours: 10, bp: 8000, want: true},
		{name: "unlimited", q: SendQuota{Max24HourSend: -1, SentLast24Hours: 1e9}, bp: 0, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.q.UsageBasisPoints(tc.ours); got != tc.bp {
				t.Fatalf("UsageBasisPoints = %d, want %d", got, tc.bp)
			}
			if got := tc.q.NearlyExhausted(tc.ours); got != tc.want {
				t.Fatalf("NearlyExhausted = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCampaignSendsGetWhatPrioritySendsLeave(t *testing.T) {
	cases := []struct {
		name   string
		rate   int64
		wanted int64
		want   int64
	}{
		{name: "no priority demand", rate: 14, wanted: 0, want: 14},
		{name: "some priority demand", rate: 14, wanted: 3, want: 11},
		{name: "priority takes the whole second", rate: 1, wanted: 1, want: 0},
		{name: "more demand than the rate", rate: 14, wanted: 20, want: 0},
		{name: "a negative reading counts as none", rate: 14, wanted: -2, want: 14},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CampaignTokens(tc.rate, tc.wanted); got != tc.want {
				t.Fatalf("CampaignTokens = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestFairShareSplitsTheCampaignBudgetBetweenWaitingWorkspaces(t *testing.T) {
	cases := []struct {
		name       string
		budget     int64
		workspaces []string
		second     int64
		want       map[string]int64
	}{
		{name: "alone takes everything", budget: 14, workspaces: []string{"a"}, want: map[string]int64{"a": 14}},
		{name: "even split", budget: 12, workspaces: []string{"a", "b", "c"}, want: map[string]int64{"a": 4, "b": 4, "c": 4}},
		{name: "remainder goes to the first in rotation", budget: 14, workspaces: []string{"a", "b", "c"}, second: 0, want: map[string]int64{"a": 5, "b": 5, "c": 4}},
		{name: "remainder rotates with the second", budget: 14, workspaces: []string{"a", "b", "c"}, second: 1, want: map[string]int64{"a": 4, "b": 5, "c": 5}},
		{name: "one token rotates between workspaces", budget: 1, workspaces: []string{"a", "b", "c"}, second: 2, want: map[string]int64{"a": 0, "b": 0, "c": 1}},
		{name: "order and duplicates of the listing do not matter", budget: 14, workspaces: []string{"c", "a", "b", "a"}, second: 0, want: map[string]int64{"a": 5, "b": 5, "c": 4}},
		{name: "no budget", budget: 0, workspaces: []string{"a", "b"}, want: map[string]int64{"a": 0, "b": 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var total int64
			for ws, want := range tc.want {
				got := FairShare(tc.budget, tc.workspaces, ws, tc.second)
				if got != want {
					t.Errorf("FairShare(%s) = %d, want %d", ws, got, want)
				}
				total += got
			}
			if tc.budget > 0 && total != tc.budget {
				t.Fatalf("shares add up to %d, want the whole budget %d", total, tc.budget)
			}
		})
	}
}

func TestAWorkspaceMissingFromTheListingStillGetsItsShare(t *testing.T) {
	if got := FairShare(10, []string{"a"}, "b", 0); got != 5 {
		t.Fatalf("FairShare = %d, want 5", got)
	}
}

func TestOneSecondHoldsEachTokenWindow(t *testing.T) {
	at := time.Date(2026, 10, 21, 12, 0, 7, 250_000_000, time.UTC)
	if got := SendWindow(at); got != at.Unix() {
		t.Fatalf("SendWindow = %d", got)
	}
	if got := UntilNextSendWindow(at); got != 750*time.Millisecond {
		t.Fatalf("UntilNextSendWindow = %s", got)
	}
	if got := UntilNextSendWindow(time.Date(2026, 10, 21, 12, 0, 8, 0, time.UTC)); got != time.Second {
		t.Fatalf("UntilNextSendWindow on the boundary = %s", got)
	}
}
