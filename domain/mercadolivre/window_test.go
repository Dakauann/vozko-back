package mercadolivre

import (
	"errors"
	"testing"
	"time"
)

var documentedBlockSubstatuses = []string{
	"blocked_by_time",
	"blocked_by_buyer",
	"blocked_by_mediation",
	"blocked_by_mediation_fbm",
	"blocked_by_fulfillment",
	"blocked_by_payment",
	"blocked_by_conversation_initiated_by_seller",
	"blocked_by_conversation_use_message_api",
	"blocked_by_conversation_initiated_by_seller_limited",
	"blocked_by_cancelled_order",
	"blocked_by_cancelled_order_by_fraud",
	"blocked_by_cancelled_order_hidden",
	"blocked_by_conversation_expired",
	"blocked_by_refund",
	"blocked_by_claim_change_open",
	"blocked_by_claim_change_closed",
	"blocked_by_deactivated_account",
	"blocked_by_restrictions",
	"blocked_by_message_pending_review",
	"blocked_by_return_to_buyer_fulfillment",
	"blocked_by_ai_assistant",
	"blocked_by_ai_assistant_expired",
	"blocked_by_ai_assistant_contact_closed",
	"block_by_ai_assistant_initiated_by_seller_expired",
	"blocked_by_resale",
	"blocked_by_proximity_groceries",
}

func ptrBool(b bool) *bool { return &b }

func TestEveryDocumentedSubstatusClosesTheWindowWithItsOwnDetail(t *testing.T) {
	for _, s := range documentedBlockSubstatuses {
		for _, status := range []string{ThreadStatusBlocked, ThreadStatusActive} {
			open, detail, err := WindowFromStatus(status, s)
			if err != nil || open || detail != s {
				t.Errorf("WindowFromStatus(%q, %q) = %v, %q, %v", status, s, open, detail, err)
			}
		}
	}
}

func TestTheSubstatusTableIsExactlyTheDocumentedList(t *testing.T) {
	got := Substatuses()
	if len(got) != len(documentedBlockSubstatuses) {
		t.Fatalf("table has %d substatuses, the documentation lists %d", len(got), len(documentedBlockSubstatuses))
	}
	want := map[string]bool{}
	for _, s := range documentedBlockSubstatuses {
		want[s] = true
	}
	for _, s := range got {
		if !want[string(s)] {
			t.Errorf("%q is in the table but not in the documented list", s)
		}
	}
}

func TestWindowFromStatusRefusesWhatItDoesNotKnow(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		substatus string
		open      bool
		detail    string
		err       error
	}{
		{"active without a block", ThreadStatusActive, "", true, "", nil},
		{"blocked without a reason", ThreadStatusBlocked, "", false, "", ErrUnknownSubstatus},
		{"blocked with a new reason", ThreadStatusBlocked, "blocked_by_something_new", false, "", ErrUnknownSubstatus},
		{"active with a new reason", ThreadStatusActive, "blocked_by_something_new", false, "", ErrUnknownSubstatus},
		{"reason spelled in capitals", ThreadStatusBlocked, "BLOCKED_BY_TIME", false, "", ErrUnknownSubstatus},
		{"started instead of initiated", ThreadStatusBlocked, "blocked_by_conversation_started_by_seller", false, "", ErrUnknownSubstatus},
		{"missing status", "", "", false, "", ErrUnknownThreadStatus},
		{"status in capitals", "ACTIVE", "", false, "", ErrUnknownThreadStatus},
		{"unknown status", "paused", "", false, "", ErrUnknownThreadStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			open, detail, err := WindowFromStatus(tc.status, tc.substatus)
			if open != tc.open || detail != tc.detail {
				t.Fatalf("WindowFromStatus = %v, %q", open, detail)
			}
			if tc.err == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestBudgetFor(t *testing.T) {
	if _, err := BudgetFor(nil); !errors.Is(err, ErrMediationUnknown) {
		t.Fatalf("BudgetFor(nil) err = %v", err)
	}
	if got, err := BudgetFor(ptrBool(true)); err != nil || got != 1 {
		t.Fatalf("BudgetFor(true) = %d, %v", got, err)
	}
	if got, err := BudgetFor(ptrBool(false)); err != nil || got != 0 {
		t.Fatalf("BudgetFor(false) = %d, %v", got, err)
	}
}

func TestSendTarget(t *testing.T) {
	cases := []struct {
		name     string
		mediated *bool
		buyer    string
		site     string
		want     string
		err      error
	}{
		{"agent mediated thread goes to the agent", ptrBool(true), "123456", SiteMLB, "3037675074", nil},
		{"agent mediated thread needs no buyer", ptrBool(true), "", SiteMLB, "3037675074", nil},
		{"direct thread goes to the buyer", ptrBool(false), "123456", SiteMLB, "123456", nil},
		{"unknown mediation refuses", nil, "123456", SiteMLB, "", ErrMediationUnknown},
		{"direct thread without buyer refuses", ptrBool(false), "", SiteMLB, "", ErrBuyerUnknown},
		{"direct thread with a malformed buyer refuses", ptrBool(false), "12a", SiteMLB, "", ErrBuyerUnknown},
		{"buyer equal to the seller refuses", ptrBool(false), "415458330", SiteMLB, "", ErrBuyerUnknown},
		{"site without an agent refuses", ptrBool(true), "123456", "MLA", "", ErrSiteNotSupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conv := &Conversation{AgentMediated: tc.mediated, BuyerMLUserID: tc.buyer}
			got, err := SendTarget(conv, tc.site, "415458330")
			if got != tc.want {
				t.Fatalf("SendTarget = %q, want %q", got, tc.want)
			}
			if tc.err == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
	if _, err := SendTarget(nil, SiteMLB, "415458330"); !errors.Is(err, ErrMediationUnknown) {
		t.Fatalf("nil conversation err = %v", err)
	}
}

func TestAgentUserID(t *testing.T) {
	if id, ok := AgentUserID(SiteMLB); !ok || id != "3037675074" {
		t.Fatalf("AgentUserID(MLB) = %q, %v", id, ok)
	}
	if _, ok := AgentUserID("MLA"); ok {
		t.Fatal("only MLB has an agent in v1")
	}
	if !IsAgentUserID(SiteMLB, "3037675074") || IsAgentUserID(SiteMLB, "123") || IsAgentUserID("MLA", "3037675074") {
		t.Fatal("IsAgentUserID mismatch")
	}
}

func TestMediationFromPath(t *testing.T) {
	cases := []struct {
		path string
		want *bool
	}{
		{"/packs/2000000089077943/sellers/415458330/conversations/resale-seller", ptrBool(true)},
		{"/packs/2000000089077943/sellers/415458330/conversations/post_sale", ptrBool(true)},
		{"/packs/22175467/sellers/32086568493", ptrBool(false)},
		{"/packs/2000000089077943/seller/415458330", ptrBool(false)},
		{"", nil},
		{"/weird", nil},
		{"/packs/1/sellers/2/conversations/", nil},
		{"/packs/1/sellers/2/conversations/a/b", nil},
		{"/packs/x/sellers/2", nil},
	}
	for _, tc := range cases {
		got := MediationFromPath(tc.path)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("MediationFromPath(%q) = %v, want unknown", tc.path, *got)
		case tc.want != nil && (got == nil || *got != *tc.want):
			t.Errorf("MediationFromPath(%q) = %v, want %v", tc.path, got, *tc.want)
		}
	}
}

func TestNewThreadState(t *testing.T) {
	read := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		status    string
		substatus string
		mediated  *bool
		open      bool
		budget    int
		detail    string
		err       error
	}{
		{"open agent thread", ThreadStatusActive, "", ptrBool(true), true, 1, "", nil},
		{"open direct thread", ThreadStatusActive, "", ptrBool(false), true, 0, "", nil},
		{"open thread with unknown mediation stays closed", ThreadStatusActive, "", nil, false, 0, "", ErrMediationUnknown},
		{"blocked thread", ThreadStatusBlocked, "blocked_by_time", ptrBool(false), false, 0, "blocked_by_time", nil},
		{"blocked agent thread keeps its budget", ThreadStatusBlocked, "blocked_by_ai_assistant", ptrBool(true), false, 1, "blocked_by_ai_assistant", nil},
		{"unknown substatus", ThreadStatusBlocked, "blocked_by_new", ptrBool(false), false, 0, "", ErrUnknownSubstatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewThreadState(tc.status, tc.substatus, tc.mediated, read)
			if s.WindowOpen != tc.open || s.OutboundBudget != tc.budget || s.Detail != tc.detail {
				t.Fatalf("state = %+v", s)
			}
			if s.Status != tc.status || s.Substatus != tc.substatus || !s.ReadAt.Equal(read) {
				t.Fatalf("raw values lost: %+v", s)
			}
			if tc.err == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestConversationStatusFreshness(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		read *time.Time
		want bool
	}{
		{"never read", nil, true},
		{"read just now", ptrTime(now), false},
		{"read inside six hours", ptrTime(now.Add(-6*time.Hour + time.Second)), false},
		{"read six hours ago", ptrTime(now.Add(-6 * time.Hour)), true},
	}
	for _, tc := range cases {
		c := &Conversation{MLStatusReadAt: tc.read}
		if got := c.StatusIsStale(now); got != tc.want {
			t.Errorf("%s: StatusIsStale = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPackSummaryFreshness(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		summary   *PackSummary
		refreshed *time.Time
		want      bool
	}{
		{"no summary", nil, ptrTime(now), true},
		{"never refreshed", &PackSummary{}, nil, true},
		{"fresh", &PackSummary{}, ptrTime(now.Add(-time.Hour)), false},
		{"a day old", &PackSummary{}, ptrTime(now.Add(-24 * time.Hour)), true},
	}
	for _, tc := range cases {
		c := &Conversation{PackSummary: tc.summary, PackSummaryRefreshedAt: tc.refreshed}
		if got := c.PackSummaryIsStale(now); got != tc.want {
			t.Errorf("%s: PackSummaryIsStale = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestThreadResourceRoundTrips(t *testing.T) {
	pack, seller, err := ParseThreadResource(ThreadResource("1977056109", "378136913"))
	if err != nil || pack != "1977056109" || seller != "378136913" {
		t.Fatalf("round trip = %q, %q, %v", pack, seller, err)
	}
}

func TestPackKeyFor(t *testing.T) {
	cases := []struct {
		pack, order, want string
		err               error
	}{
		{"2000006181551917", "2000009047722568", "2000006181551917", nil},
		{"", "2000009047722568", "2000009047722568", nil},
		{"", "", "", ErrPackUnknown},
		{"20x", "2000009047722568", "", ErrPackUnknown},
		{"", "abc", "", ErrPackUnknown},
	}
	for _, tc := range cases {
		got, err := PackKeyFor(tc.pack, tc.order)
		if got != tc.want || (tc.err == nil) != (err == nil) || (tc.err != nil && !errors.Is(err, tc.err)) {
			t.Errorf("PackKeyFor(%q, %q) = %q, %v", tc.pack, tc.order, got, err)
		}
	}
}

func TestParseThreadResource(t *testing.T) {
	cases := []struct {
		resource     string
		pack, seller string
		ok           bool
	}{
		{"/packs/1977056109/sellers/378136913", "1977056109", "378136913", true},
		{"/packs/1234/sellers/2345", "1234", "2345", true},
		{"/packs/1234/seller/2345", "1234", "2345", true},
		{"/packs/1234", "", "", false},
		{"/orders/1234/sellers/2345", "", "", false},
		{"/packs/12a/sellers/2345", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		pack, seller, err := ParseThreadResource(tc.resource)
		if (err == nil) != tc.ok || pack != tc.pack || seller != tc.seller {
			t.Errorf("ParseThreadResource(%q) = %q, %q, %v", tc.resource, pack, seller, err)
		}
		if err != nil && !errors.Is(err, ErrInvalidResource) {
			t.Errorf("ParseThreadResource(%q) err = %v, want ErrInvalidResource", tc.resource, err)
		}
	}
}
