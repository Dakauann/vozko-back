package emailsending

import (
	"testing"
	"time"
)

func TestBillableChunks(t *testing.T) {
	cases := []struct {
		name  string
		bytes int64
		want  int64
	}{
		{name: "a short reply has no chunk", bytes: 30 << 10, want: 0},
		{name: "just under one chunk", bytes: 256<<10 - 1, want: 0},
		{name: "exactly one chunk", bytes: 256 << 10, want: 1},
		{name: "only complete chunks count, as AWS bills 768 KB as three", bytes: 768 << 10, want: 3},
		{name: "a maximum size message", bytes: 40 << 20, want: 160},
		{name: "a negative size is never billed", bytes: -1, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BillableChunks(tc.bytes); got != tc.want {
				t.Fatalf("BillableChunks(%d) = %d, want %d", tc.bytes, got, tc.want)
			}
		})
	}
}

func TestInboundVerdictRejected(t *testing.T) {
	cases := []struct {
		verdict InboundVerdict
		want    bool
	}{
		{verdict: InboundVerdict{Spam: VerdictPass, Virus: VerdictPass}, want: false},
		{verdict: InboundVerdict{Spam: VerdictGray, Virus: VerdictPass}, want: false},
		{verdict: InboundVerdict{Spam: VerdictFail, Virus: VerdictPass}, want: true},
		{verdict: InboundVerdict{Spam: VerdictPass, Virus: VerdictFail}, want: true},
		{verdict: InboundVerdict{Spam: VerdictProcessingFailed, Virus: VerdictProcessingFailed}, want: false},
		{verdict: InboundVerdict{}, want: false},
	}
	for _, tc := range cases {
		if got := tc.verdict.Rejected(); got != tc.want {
			t.Fatalf("%+v.Rejected() = %v, want %v", tc.verdict, got, tc.want)
		}
	}
}

func TestFlooding(t *testing.T) {
	if Flooding(InboundFloodThreshold - 1) {
		t.Fatal("below the threshold is normal traffic")
	}
	if !Flooding(InboundFloodThreshold) {
		t.Fatal("reaching the threshold inside the window is a flood")
	}
	if InboundFloodWindow != 10*time.Minute || InboundBlockDuration != 7*24*time.Hour {
		t.Fatalf("window %s, block %s", InboundFloodWindow, InboundBlockDuration)
	}
}

func TestInboundPaused(t *testing.T) {
	cases := []struct {
		balance int64
		want    bool
	}{
		{balance: 1, want: false},
		{balance: 0, want: false},
		{balance: -1, want: true},
	}
	for _, tc := range cases {
		if got := InboundPaused(tc.balance); got != tc.want {
			t.Fatalf("InboundPaused(%d) = %v, want %v", tc.balance, got, tc.want)
		}
	}
}

func TestInboundRecipientCapacity(t *testing.T) {
	if InboundConditionsPerRule != 100 || InboundRulesPerRuleSet != 200 {
		t.Fatalf("SES limits are %d conditions per rule and %d rules per set", InboundConditionsPerRule, InboundRulesPerRuleSet)
	}
	if got := InboundReplyDomainsPerRule(); got != 50 {
		t.Fatalf("reply domains per rule = %d, want 50", got)
	}
	if got := InboundReplyDomainCapacity(); got != 10_000 {
		t.Fatalf("reply domain capacity = %d, want 10000", got)
	}
}

func TestInboundRecipients(t *testing.T) {
	got := InboundRecipients("reply.loja.com.br")
	want := []string{"reply@reply.loja.com.br", "unsubscribe@reply.loja.com.br"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("InboundRecipients = %v, want %v", got, want)
	}
}

func TestSendingDomainReceivesReplies(t *testing.T) {
	cases := []struct {
		name   string
		domain SendingDomain
		want   bool
	}{
		{name: "an active own domain", domain: SendingDomain{Kind: DomainOwn}, want: true},
		{name: "a removed own domain lost its route", domain: SendingDomain{Kind: DomainOwn, Retired: true}, want: false},
		{name: "a probation domain retired by promotion keeps answering old replies", domain: SendingDomain{Kind: DomainSharedProbation, Retired: true}, want: true},
		{name: "an active trusted domain", domain: SendingDomain{Kind: DomainSharedTrusted}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.domain.ReceivesReplies(); got != tc.want {
				t.Fatalf("ReceivesReplies() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSendingDomainRepliesPaused(t *testing.T) {
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	if (SendingDomain{}).RepliesPaused() {
		t.Fatal("a domain without a pause stamp is receiving")
	}
	if !(SendingDomain{RepliesPausedAt: &at}).RepliesPaused() {
		t.Fatal("a stamped domain is paused")
	}
}

func TestExpiredSenderBlocks(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	past := SenderBlock{Name: "a", Until: now.Add(-time.Second)}
	exact := SenderBlock{Name: "b", Until: now}
	future := SenderBlock{Name: "c", Until: now.Add(time.Hour)}
	got := ExpiredSenderBlocks([]SenderBlock{past, future, exact}, now)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("ExpiredSenderBlocks = %+v, want the past and the exactly expired block", got)
	}
}

func TestSenderBlockToEvict(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	managed := []SenderBlock{
		{Name: "late", Until: now.Add(6 * 24 * time.Hour)},
		{Name: "soonest", Until: now.Add(time.Hour)},
		{Name: "middle", Until: now.Add(3 * 24 * time.Hour)},
	}
	cases := []struct {
		name     string
		managed  []SenderBlock
		inUse    int
		want     string
		evicting bool
	}{
		{name: "room left means no eviction", managed: managed, inUse: InboundIPFiltersPerAccount - 1},
		{name: "at the quota the soonest expiring block makes room", managed: managed, inUse: InboundIPFiltersPerAccount, want: "soonest", evicting: true},
		{name: "above the quota still evicts the soonest expiring block", managed: managed, inUse: InboundIPFiltersPerAccount + 3, want: "soonest", evicting: true},
		{name: "a quota full of filters that are not ours evicts nothing", managed: nil, inUse: InboundIPFiltersPerAccount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SenderBlockToEvict(tc.managed, tc.inUse)
			if ok != tc.evicting || got.Name != tc.want {
				t.Fatalf("SenderBlockToEvict = %+v, %v; want %q, %v", got, ok, tc.want, tc.evicting)
			}
		})
	}
	if InboundIPFiltersPerAccount != 100 {
		t.Fatalf("SES allows 100 IP address filters per account, got %d", InboundIPFiltersPerAccount)
	}
}
