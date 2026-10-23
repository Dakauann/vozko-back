package emailsending

import (
	"errors"
	"testing"
	"time"
)

func TestADomainSetupIsInProgressFailedOrDone(t *testing.T) {
	cases := []struct {
		name   string
		domain SendingDomain
		want   SetupProgress
	}{
		{"pending without a failure", SendingDomain{Status: DomainPending}, SetupProgress{State: SetupInProgress}},
		{"pending with a provider failure", SendingDomain{Status: DomainPending, SetupFailure: SetupFailureProvider, FailureReason: "identity: boom"}, SetupProgress{State: SetupFailed, Failure: SetupFailureProvider}},
		{"pending with the reply capacity full", SendingDomain{Status: DomainPending, SetupFailure: SetupFailureReplyCapacity}, SetupProgress{State: SetupFailed, Failure: SetupFailureReplyCapacity}},
		{"verifying", SendingDomain{Status: DomainVerifying}, SetupProgress{State: SetupDone}},
		{"verified with an old failure note", SendingDomain{Status: DomainVerified, SetupFailure: SetupFailureProvider}, SetupProgress{State: SetupDone}},
		{"failed verification", SendingDomain{Status: DomainFailed, FailureReason: "dkim_failed"}, SetupProgress{State: SetupDone}},
		{"retired while pending", SendingDomain{Status: DomainPending, Retired: true}, SetupProgress{State: SetupDone}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.domain.SetupProgress(); got != tc.want {
				t.Fatalf("progress = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTheWorkspaceSetupFollowsItsLiveDomains(t *testing.T) {
	failed := SendingDomain{Status: DomainPending, SetupFailure: SetupFailureReplyCapacity}
	cases := []struct {
		name    string
		domains []SendingDomain
		want    SetupProgress
	}{
		{"no domains", nil, SetupProgress{State: SetupDone}},
		{"every domain set up", []SendingDomain{{Status: DomainVerified}, {Status: DomainVerifying}}, SetupProgress{State: SetupDone}},
		{"one failed", []SendingDomain{{Status: DomainVerified}, failed}, SetupProgress{State: SetupFailed, Failure: SetupFailureReplyCapacity}},
		{"one still in progress wins over a failure", []SendingDomain{failed, {Status: DomainPending}}, SetupProgress{State: SetupInProgress}},
		{"a retired pending domain is ignored", []SendingDomain{{Status: DomainVerified}, {Status: DomainPending, Retired: true}}, SetupProgress{State: SetupDone}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SetupProgressOf(tc.domains); got != tc.want {
				t.Fatalf("progress = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRestartingSetupClearsTheFailureAndReturnsToPending(t *testing.T) {
	d := SendingDomain{Status: DomainFailed, SetupFailure: SetupFailureProvider, FailureReason: "dns: down"}
	if !d.RestartSetup() {
		t.Fatal("a failed domain changes when its setup restarts")
	}
	if d.Status != DomainPending || d.SetupFailure != "" || d.FailureReason != "" || d.SetupProgress().State != SetupInProgress {
		t.Fatalf("domain = %+v", d)
	}
	if d.RestartSetup() {
		t.Fatal("restarting a setup already in progress changes nothing")
	}
}

func TestAFailedSetupKeepsAVerifiedDomainVerified(t *testing.T) {
	pending := SendingDomain{Status: DomainVerifying}
	pending.FailSetup(SetupFailureProvider, "identity: boom")
	if pending.Status != DomainPending || pending.SetupFailure != SetupFailureProvider || pending.FailureReason != "identity: boom" {
		t.Fatalf("domain = %+v", pending)
	}
	verified := SendingDomain{Status: DomainVerified}
	verified.FailSetup(SetupFailureProvider, "inbound: boom")
	if verified.Status != DomainVerified || verified.SetupProgress().State != SetupDone {
		t.Fatalf("domain = %+v", verified)
	}
}

func TestACompletedSetupStartsTheVerificationOnce(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	layout := DomainLayout{Kind: DomainOwn, Domain: "loja.com.br", MailFromDomain: "bounce.loja.com.br", ReplyDomain: "reply.loja.com.br"}
	records := []DNSRecord{{Purpose: RecordDKIM, Type: "CNAME", Name: "k._domainkey.loja.com.br", Value: "k.dkim"}}
	d := SendingDomain{Status: DomainPending, SetupFailure: SetupFailureProvider, FailureReason: "identity: boom"}
	d.CompleteSetup(records, layout, now)
	if d.Status != DomainVerifying || d.VerifyingSince == nil || !d.VerifyingSince.Equal(now) || d.SetupFailure != "" || d.FailureReason != "" ||
		d.MailFromDomain != layout.MailFromDomain || d.ReplyDomain != layout.ReplyDomain || len(d.Records) != 1 {
		t.Fatalf("domain = %+v", d)
	}
	later := now.Add(time.Hour)
	d.CompleteSetup(records, layout, later)
	if !d.VerifyingSince.Equal(now) {
		t.Fatalf("a repeated setup keeps the verification clock: %v", d.VerifyingSince)
	}
	verified := SendingDomain{Status: DomainVerified}
	verified.CompleteSetup(records, layout, now)
	if verified.Status != DomainVerified || verified.VerifyingSince != nil {
		t.Fatalf("verified = %+v", verified)
	}
}

func TestSetupFailuresAreKnownValuesOnly(t *testing.T) {
	cases := map[SetupFailure]bool{
		"":                        true,
		SetupFailureProvider:      true,
		SetupFailureReplyCapacity: true,
		"timeout":                 false,
	}
	for failure, want := range cases {
		if got := failure.Valid(); got != want {
			t.Errorf("%q valid = %v, want %v", failure, got, want)
		}
	}
}

func TestTheReplyCapacityIsItsOwnSetupFailure(t *testing.T) {
	if got := SetupFailureFor(errors.Join(errors.New("rule set"), ErrInboundCapacityReached)); got != SetupFailureReplyCapacity {
		t.Fatalf("failure = %q", got)
	}
	if got := SetupFailureFor(errors.New("throttled")); got != SetupFailureProvider {
		t.Fatalf("failure = %q", got)
	}
}

func TestReleasingADomainRetiresItUntilTheProviderIsCleaned(t *testing.T) {
	d := SendingDomain{Status: DomainVerifying}
	d.Release()
	if !d.Retired || d.ReleaseState != ReleasePending || d.AwaitsSetup() || !d.AwaitsRelease() {
		t.Fatalf("domain = %+v", d)
	}
	d.Released()
	if d.ReleaseState != ReleaseDone || d.AwaitsRelease() || !d.Retired {
		t.Fatalf("domain = %+v", d)
	}
	d.Reinstate()
	if d.Retired || d.ReleaseState != ReleaseNone || d.Status != DomainPending || d.SetupProgress().State != SetupInProgress {
		t.Fatalf("reinstated = %+v", d)
	}
	for state, want := range map[ReleaseState]bool{ReleaseNone: true, ReleasePending: true, ReleaseDone: true, "gone": false} {
		if state.Valid() != want {
			t.Errorf("%q valid = %v", state, !want)
		}
	}
}
