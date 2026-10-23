package emailhygiene

import (
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

func at(t time.Time) *time.Time { return &t }

func TestInactiveSunsetsOnlyAfterSixMonthsAndFiveCampaigns(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	sevenMonths := now.AddDate(0, -7, 0)
	fiveMonths := now.AddDate(0, -5, 0)
	exactlySix := now.AddDate(0, -6, 0)
	cases := []struct {
		name     string
		activity Activity
		want     bool
	}{
		{name: "never mailed is never inactive", activity: Activity{}, want: false},
		{name: "seven months and five campaigns without engagement is inactive", activity: Activity{Known: true, SunsetSince: at(sevenMonths), CampaignsSinceEngaged: 5}, want: true},
		{name: "seven months but only four campaigns is too little mail to judge", activity: Activity{Known: true, SunsetSince: at(sevenMonths), CampaignsSinceEngaged: 4}, want: false},
		{name: "five months is inside the window", activity: Activity{Known: true, SunsetSince: at(fiveMonths), CampaignsSinceEngaged: 20}, want: false},
		{name: "exactly six months is still inside the window", activity: Activity{Known: true, SunsetSince: at(exactlySix), CampaignsSinceEngaged: 20}, want: false},
		{name: "a row without a reference time is not inactive", activity: Activity{Known: true, CampaignsSinceEngaged: 9}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.activity.Inactive(now); got != tc.want {
				t.Fatalf("Inactive = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEverDeliveredDecidesTheFreshShare(t *testing.T) {
	if (Activity{}).EverDelivered() {
		t.Fatal("no activity means never delivered")
	}
	if (Activity{Known: true, SoftStreak: 2}).EverDelivered() {
		t.Fatal("only bounces means never delivered")
	}
	if !(Activity{Known: true, FirstDeliveredAt: at(time.Now())}).EverDelivered() {
		t.Fatal("a delivery means the address was mailed before")
	}
}

func TestEngagesCountsClicksAndHumanOpensButNotMachineOpens(t *testing.T) {
	cases := []struct {
		name string
		ev   es.DeliveryEvent
		want bool
	}{
		{name: "a human click engages", ev: es.DeliveryEvent{Kind: es.EventClick, UserAgent: "Mozilla/5.0 (iPhone)"}, want: true},
		{name: "a bot click does not engage", ev: es.DeliveryEvent{Kind: es.EventClick, IsBot: true}, want: false},
		{name: "a human open engages", ev: es.DeliveryEvent{Kind: es.EventOpen, UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"}, want: true},
		{name: "an apple privacy prefetch does not engage", ev: es.DeliveryEvent{Kind: es.EventOpen, UserAgent: "Mozilla/5.0"}, want: false},
		{name: "an apple privacy prefetch with spaces does not engage", ev: es.DeliveryEvent{Kind: es.EventOpen, UserAgent: "  Mozilla/5.0 "}, want: false},
		{name: "a bot open does not engage", ev: es.DeliveryEvent{Kind: es.EventOpen, IsBot: true, UserAgent: "Mozilla/5.0 (Windows)"}, want: false},
		{name: "a delivery is not engagement", ev: es.DeliveryEvent{Kind: es.EventDelivery}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Engages(tc.ev); got != tc.want {
				t.Fatalf("Engages = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSoftBounceStreakCountsOnlyMailboxFailures(t *testing.T) {
	cases := []struct {
		name   string
		bounce *es.Bounce
		want   bool
	}{
		{name: "mailbox full counts", bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "MailboxFull"}, want: true},
		{name: "a general transient failure counts", bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "General"}, want: true},
		{name: "an undetermined bounce counts", bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "Undetermined"}, want: true},
		{name: "a soft bounce without subtype counts", bounce: &es.Bounce{Kind: es.BounceSoft}, want: true},
		{name: "a message too large is about the message", bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "MessageTooLarge"}, want: false},
		{name: "rejected content is about the message", bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "ContentRejected"}, want: false},
		{name: "a rejected attachment is about the message", bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "AttachmentRejected"}, want: false},
		{name: "a hard bounce is not part of the soft streak", bounce: &es.Bounce{Kind: es.BounceHard, SubType: "General"}, want: false},
		{name: "a provider list hit is not part of the soft streak", bounce: &es.Bounce{Kind: es.BounceSuppressed, SubType: "Suppressed"}, want: false},
		{name: "no bounce detail is not counted", bounce: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountsTowardSoftStreak(tc.bounce); got != tc.want {
				t.Fatalf("CountsTowardSoftStreak = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSoftStreakSuppressesAfterFiveSpanningTwoWeeks(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		streak int
		last   time.Time
		want   bool
	}{
		{name: "five over fourteen days suppresses", streak: 5, last: start.Add(14 * 24 * time.Hour), want: true},
		{name: "six over twenty days suppresses", streak: 6, last: start.Add(20 * 24 * time.Hour), want: true},
		{name: "four is not enough", streak: 4, last: start.Add(30 * 24 * time.Hour), want: false},
		{name: "five inside one week is a full mailbox for a week, not a dead one", streak: 5, last: start.Add(6 * 24 * time.Hour), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SoftStreakSuppresses(tc.streak, start, tc.last); got != tc.want {
				t.Fatalf("SoftStreakSuppresses = %v, want %v", got, tc.want)
			}
		})
	}
}
