package emailconsent

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func TestNewSubscription(t *testing.T) {
	cases := []struct {
		name    string
		consent Consent
		wantErr error
		wantIP  string
	}{
		{name: "form with ip", consent: Consent{WorkspaceID: "ws", Address: " Ana@Example.com ", Source: SourceForm, IP: "203.0.113.9", At: now}, wantIP: "203.0.113.9"},
		{name: "ipv6 is canonical", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceAPI, IP: "2001:DB8::0:1", At: now}, wantIP: "2001:db8::1"},
		{name: "import without ip", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now}},
		{name: "operator without ip", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceOperator, At: now}},
		{name: "form needs ip", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceForm, At: now}, wantErr: ErrConsentIPRequired},
		{name: "bad ip", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, IP: "999.1.1.1", At: now}, wantErr: ErrConsentIPInvalid},
		{name: "unknown source", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: "scraped", At: now}, wantErr: ErrSourceInvalid},
		{name: "no workspace", consent: Consent{Address: "ana@example.com", Source: SourceImport, At: now}, wantErr: ErrWorkspaceRequired},
		{name: "bad address", consent: Consent{WorkspaceID: "ws", Address: "nope", Source: SourceImport, At: now}, wantErr: ErrAddressInvalid},
		{name: "no date", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport}, wantErr: ErrConsentDateRequired},
		{name: "future date", consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now.Add(48 * time.Hour)}, wantErr: ErrConsentDateInFuture},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := NewSubscription(tc.consent, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if sub.Address != "ana@example.com" || sub.Status != StatusSubscribed || sub.ConsentIP != tc.wantIP || !sub.ConsentedAt.Equal(tc.consent.At) {
				t.Fatalf("subscription = %+v", sub)
			}
		})
	}
}

func TestRenew(t *testing.T) {
	base, err := NewSubscription(Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now.Add(-time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	gone, _, _ := base.Unsubscribe(UnsubscribeLink, now.Add(-time.Minute))
	cases := []struct {
		name        string
		current     Subscription
		consent     Consent
		wantChanged bool
		wantStatus  Status
		wantSource  Source
	}{
		{name: "subscribed stays subscribed", current: base, consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now}, wantStatus: StatusSubscribed, wantSource: SourceImport},
		{name: "subscribed gains a lead link", current: base, consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", LeadID: "lead-1", Source: SourceImport, At: now}, wantChanged: true, wantStatus: StatusSubscribed, wantSource: SourceImport},
		{name: "import never revives an opt-out", current: gone, consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now}, wantStatus: StatusUnsubscribed, wantSource: SourceImport},
		{name: "a form consent revives it", current: gone, consent: Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceForm, IP: "203.0.113.9", At: now}, wantChanged: true, wantStatus: StatusSubscribed, wantSource: SourceForm},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed, err := tc.current.Renew(tc.consent, now)
			if err != nil {
				t.Fatal(err)
			}
			if changed != tc.wantChanged || got.Status != tc.wantStatus || got.Source != tc.wantSource {
				t.Fatalf("Renew() = %+v changed=%v", got, changed)
			}
			if got.Status == StatusSubscribed && (got.UnsubscribedAt != nil || got.UnsubscribeSource != "") {
				t.Fatalf("a renewed subscription keeps opt-out traces: %+v", got)
			}
		})
	}
}

func TestRenewRefusesAnotherAddress(t *testing.T) {
	base, _ := NewSubscription(Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now}, now)
	if _, _, err := base.Renew(Consent{WorkspaceID: "ws", Address: "bia@example.com", Source: SourceImport, At: now}, now); !errors.Is(err, ErrAddressMismatch) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := base.Renew(Consent{WorkspaceID: "other", Address: "ana@example.com", Source: SourceImport, At: now}, now); !errors.Is(err, ErrAddressMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	base, _ := NewSubscription(Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now}, now)
	first, changed, err := base.Unsubscribe(UnsubscribeOneClick, now)
	if err != nil || !changed || first.Status != StatusUnsubscribed || first.UnsubscribeSource != UnsubscribeOneClick || first.UnsubscribedAt == nil {
		t.Fatalf("first = %+v changed=%v err=%v", first, changed, err)
	}
	second, changed, err := first.Unsubscribe(UnsubscribeComplaint, now.Add(time.Hour))
	if err != nil || changed || second.UnsubscribeSource != UnsubscribeOneClick || !second.UnsubscribedAt.Equal(now) {
		t.Fatalf("second = %+v changed=%v err=%v", second, changed, err)
	}
	if _, _, err := base.Unsubscribe("shrug", now); !errors.Is(err, ErrUnsubscribeSourceInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewUnsubscribedRecordsAnOptOutWithoutConsent(t *testing.T) {
	sub, err := NewUnsubscribed("ws", "Ana@Example.com", "lead-1", UnsubscribeMailto, now)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != StatusUnsubscribed || sub.Address != "ana@example.com" || sub.UnsubscribeSource != UnsubscribeMailto || sub.Source != "" {
		t.Fatalf("sub = %+v", sub)
	}
	if MarketingEligibility(&sub, LeadFacts{}) != SkipUnsubscribed {
		t.Fatal("an opt-out record must refuse marketing")
	}
	if _, err := NewUnsubscribed("ws", "ana@example.com", "", "bogus", now); !errors.Is(err, ErrUnsubscribeSourceInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarketingEligibility(t *testing.T) {
	subscribed, _ := NewSubscription(Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: SourceImport, At: now}, now)
	unsubscribed, _, _ := subscribed.Unsubscribe(UnsubscribeLink, now)
	cases := []struct {
		name  string
		sub   *Subscription
		facts LeadFacts
		want  SkipReason
	}{
		{name: "eligible", sub: &subscribed, want: SkipNone},
		{name: "no subscription record", sub: nil, want: SkipNone},
		{name: "no record and globally opted out", sub: nil, facts: LeadFacts{OptedOut: true}, want: SkipOptedOut},
		{name: "no record and blocked", sub: nil, facts: LeadFacts{Blocked: true}, want: SkipBlocked},
		{name: "unsubscribed", sub: &unsubscribed, facts: LeadFacts{OptedOut: true}, want: SkipUnsubscribed},
		{name: "global opt-out", sub: &subscribed, facts: LeadFacts{OptedOut: true, Blocked: true}, want: SkipOptedOut},
		{name: "blocked", sub: &subscribed, facts: LeadFacts{Blocked: true}, want: SkipBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MarketingEligibility(tc.sub, tc.facts); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSourcesAndUnsubscribeSourcesAreClosedSets(t *testing.T) {
	for _, s := range []Source{SourceImport, SourceForm, SourceOperator, SourceAPI} {
		if !s.Valid() {
			t.Errorf("%q must be valid", s)
		}
	}
	for _, s := range []UnsubscribeSource{UnsubscribeOneClick, UnsubscribeLink, UnsubscribeMailto, UnsubscribeComplaint, UnsubscribeOperator} {
		if !s.Valid() {
			t.Errorf("%q must be valid", s)
		}
	}
	if Source("").Valid() || UnsubscribeSource("").Valid() {
		t.Fatal("empty values are not sources")
	}
}
