package emailcampaign

import (
	"testing"
	"time"

	"vozko/domain/emailconsent"
	"vozko/domain/emailsending"
)

func TestSkipReasonsAreAClosedSetAndCoverConsent(t *testing.T) {
	for _, r := range AllSkipReasons() {
		if !r.Valid() {
			t.Errorf("%q must be valid", r)
		}
	}
	for _, consent := range []emailconsent.SkipReason{emailconsent.SkipUnsubscribed, emailconsent.SkipOptedOut, emailconsent.SkipBlocked} {
		if !SkipFromConsent(consent).Valid() {
			t.Errorf("consent reason %q has no campaign skip reason", consent)
		}
	}
	if SkipFromConsent(emailconsent.SkipNone) != SkipNone {
		t.Fatal("an eligible address is not skipped")
	}
	if SkipReason("over_cap").Valid() {
		t.Fatal("the daily cap is a retry, never a skip")
	}
	for _, retired := range []SkipReason{"not_subscribed", "undeliverable"} {
		if retired.Valid() {
			t.Errorf("%q is not a skip reason any more", retired)
		}
	}
}

func TestBasisPoints(t *testing.T) {
	cases := []struct {
		n, d int64
		want int
	}{
		{0, 0, 0}, {5, 0, 0}, {1, 3, 3333}, {2, 3, 6667}, {1, 1, 10000}, {3, 1000, 30},
	}
	for _, tc := range cases {
		if got := BasisPoints(tc.n, tc.d); got != tc.want {
			t.Errorf("BasisPoints(%d, %d) = %d, want %d", tc.n, tc.d, got, tc.want)
		}
	}
}

func TestNewMetrics(t *testing.T) {
	m := NewMetrics(Tally{
		Recipients: 1000, Pending: 100, Sent: 800, Failed: 10,
		Skipped:   map[SkipReason]int64{SkipNoEmail: 50, SkipUnsubscribed: 40},
		Delivered: 760, Opens: 380, Clicks: 95, HardBounces: 30, SoftBounces: 10,
		Complaints: 2, Unsubscribes: 8,
	})
	want := map[string][2]int{
		"delivery":    {m.DeliveryRate, 9500},
		"open":        {m.OpenRate, 5000},
		"click":       {m.ClickRate, 1250},
		"ctor":        {m.ClickToOpenRate, 2500},
		"bounce":      {m.BounceRate, 500},
		"complaint":   {m.ComplaintRate, 26},
		"unsubscribe": {m.UnsubscribeRate, 105},
	}
	for name, pair := range want {
		if pair[0] != pair[1] {
			t.Errorf("%s rate = %d, want %d", name, pair[0], pair[1])
		}
	}
	if m.SkippedTotal != 90 {
		t.Fatalf("skipped total = %d", m.SkippedTotal)
	}
}

func TestMailboxFamily(t *testing.T) {
	cases := map[string]string{
		"gmail.com": "gmail", "googlemail.com": "gmail", "GMAIL.COM": "gmail",
		"hotmail.com": "microsoft", "outlook.com": "microsoft", "live.com": "microsoft", "msn.com": "microsoft",
		"hotmail.com.br": "microsoft", "outlook.com.br": "microsoft",
		"yahoo.com": "yahoo", "yahoo.com.br": "yahoo", "ymail.com": "yahoo",
		"icloud.com": "apple", "me.com": "apple", "mac.com": "apple",
		"uol.com.br": "uol", "bol.com.br": "uol",
		"empresa.com.br": "other", "": "other",
	}
	for domain, want := range cases {
		if got := MailboxFamily(domain); got != want {
			t.Errorf("MailboxFamily(%q) = %q, want %q", domain, got, want)
		}
	}
}

func TestGroupDomains(t *testing.T) {
	got := GroupDomains([]DomainCount{
		{Domain: "gmail.com", Sent: 10, Delivered: 9, Opens: 4, Bounces: 1},
		{Domain: "googlemail.com", Sent: 5, Delivered: 5, Opens: 1},
		{Domain: "acme.com", Sent: 3, Delivered: 2, Bounces: 1},
		{Domain: "hotmail.com", Sent: 20, Delivered: 19, Opens: 10, Bounces: 1},
		{Domain: "foo.org", Sent: 1, Delivered: 1},
	})
	want := []DomainStat{
		{Family: "microsoft", Sent: 20, Delivered: 19, Opens: 10, Bounces: 1, OpenRate: 5263, BounceRate: 500},
		{Family: "gmail", Sent: 15, Delivered: 14, Opens: 5, Bounces: 1, OpenRate: 3571, BounceRate: 667},
		{Family: "other", Sent: 4, Delivered: 3, Opens: 0, Bounces: 1, OpenRate: 0, BounceRate: 2500},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTimelineBucket(t *testing.T) {
	start := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if TimelineBucketFor(start, start.Add(47*time.Hour)) != BucketHour {
		t.Fatal("short campaigns are bucketed by hour")
	}
	if TimelineBucketFor(start, start.Add(49*time.Hour)) != BucketDay {
		t.Fatal("long campaigns are bucketed by day")
	}
}

func TestReputation(t *testing.T) {
	cases := []struct {
		name           string
		tally          Tally
		bounce         int
		bounceLevel    emailsending.ReputationLevel
		complaint      int
		complaintLevel emailsending.ReputationLevel
	}{
		{name: "healthy", tally: Tally{Sent: 10000, Delivered: 9900, HardBounces: 20, SoftBounces: 20, Complaints: 2}, bounce: 40, bounceLevel: emailsending.ReputationOK, complaint: 2, complaintLevel: emailsending.ReputationOK},
		{name: "warning", tally: Tally{Sent: 10000, Delivered: 9900, HardBounces: 100, SoftBounces: 120, Complaints: 20}, bounce: 220, bounceLevel: emailsending.ReputationWarning, complaint: 20, complaintLevel: emailsending.ReputationWarning},
		{name: "over", tally: Tally{Sent: 10000, Delivered: 9000, HardBounces: 300, SoftBounces: 250, Complaints: 40}, bounce: 550, bounceLevel: emailsending.ReputationOver, complaint: 44, complaintLevel: emailsending.ReputationOver},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReputation(tc.tally)
			if r.BounceRate != tc.bounce || r.BounceLevel != tc.bounceLevel || r.BounceWarning != 200 || r.BounceLimit != 500 || r.BounceOverLimit != (tc.bounceLevel == emailsending.ReputationOver) {
				t.Fatalf("bounce = %+v", r)
			}
			if r.ComplaintRate != tc.complaint || r.ComplaintLevel != tc.complaintLevel || r.ComplaintWarning != 10 || r.ComplaintLimit != 30 || r.ComplaintOverLimit != (tc.complaintLevel == emailsending.ReputationOver) {
				t.Fatalf("complaint = %+v", r)
			}
		})
	}
}
