package template

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
)

// The classification table is the billing decision in one place. Each row is a
// way a send can end and the money consequence of reading it wrongly.
func TestClassifySendOutcome(t *testing.T) {
	cases := []struct {
		name string
		out  *conversation.SendTextMessageOutput
		err  error
		want SendOutcome
	}{
		{
			name: "message id returned",
			out:  &conversation.SendTextMessageOutput{MessageID: "wamid.1", ResponseStatus: 200},
			want: OutcomeAccepted,
		},
		{
			name: "2xx with no message id is still accepted",
			out:  &conversation.SendTextMessageOutput{ResponseStatus: 200},
			want: OutcomeAcceptedNoID,
		},
		{
			// The expensive one. A 2xx with an error means WE broke, not them: the
			// provider already took the message. Refunding here credits something
			// the customer has already received.
			name: "2xx alongside an error is our failure, not a rejection",
			out:  &conversation.SendTextMessageOutput{ResponseStatus: 200},
			err:  errors.New("json: cannot unmarshal"),
			want: OutcomeAcceptedNoID,
		},
		{
			name: "typed unknown outcome is accepted, never refunded",
			out:  &conversation.SendTextMessageOutput{ResponseStatus: 200},
			err:  errors.Join(conversation.ErrSendOutcomeUnknown, errors.New("bad json")),
			want: OutcomeAcceptedNoID,
		},
		{
			name: "4xx is a considered refusal, nothing was sent",
			out:  &conversation.SendTextMessageOutput{ResponseStatus: 400},
			err:  errors.New("bad request"),
			want: OutcomeRejected,
		},
		{
			name: "5xx tells us nothing either way",
			out:  &conversation.SendTextMessageOutput{ResponseStatus: 503},
			err:  errors.New("upstream down"),
			want: OutcomeUnknown,
		},
		{
			name: "no response at all is unknown, not rejected",
			out:  nil,
			err:  errors.New("dial tcp: connection refused"),
			want: OutcomeUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifySendOutcome(tc.out, tc.err)
			if got != tc.want {
				t.Fatalf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

// Only an outright refusal returns money. Everything else either delivered or
// might have.
func TestOnlyRejectedRefunds(t *testing.T) {
	for _, o := range []SendOutcome{OutcomeAccepted, OutcomeAcceptedNoID, OutcomeUnknown} {
		if o.ShouldRefund() {
			t.Fatalf("%q must not refund: it may have been delivered", o)
		}
	}
	if !OutcomeRejected.ShouldRefund() {
		t.Fatal("a rejected send must be refunded, nothing was delivered")
	}
}

// The state machine is what makes "the money gate is crossed once" a database
// property rather than a matter of careful coding.
func TestSendAttemptTransitions(t *testing.T) {
	legal := [][2]SendAttemptStatus{
		{SendAttemptPending, SendAttemptCharged},
		// The sweep's job: money left and no send was ever confirmed.
		{SendAttemptCharged, SendAttemptRefunded},
		// Delivery-failure settlement. Meta bills on delivery, so an accepted
		// send that later fails delivery must be refundable.
		{SendAttemptSent, SendAttemptRefunded},
		{SendAttemptCharged, SendAttemptSent},
		{SendAttemptCharged, SendAttemptRejected},
		{SendAttemptCharged, SendAttemptUnknown},
		{SendAttemptRejected, SendAttemptRefunded},
		{SendAttemptUnknown, SendAttemptRefunded},
	}
	for _, edge := range legal {
		if !edge[0].CanTransitionTo(edge[1]) {
			t.Fatalf("%s -> %s must be allowed", edge[0], edge[1])
		}
	}

	illegal := [][2]SendAttemptStatus{
		// Nothing returns to pending: that would reopen the money gate.
		{SendAttemptCharged, SendAttemptPending},
		{SendAttemptSent, SendAttemptPending},
		{SendAttemptRefunded, SendAttemptCharged},
		// Charging twice is the failure this whole design exists to prevent.
		{SendAttemptCharged, SendAttemptCharged},
	}
	for _, edge := range illegal {
		if edge[0].CanTransitionTo(edge[1]) {
			t.Fatalf("%s -> %s must be refused", edge[0], edge[1])
		}
	}
}

func TestPredecessorsOfChargedIsPendingOnly(t *testing.T) {
	got := PredecessorsOf(SendAttemptCharged)
	if len(got) != 1 || got[0] != SendAttemptPending {
		t.Fatalf("only a pending attempt may be charged, got %v", got)
	}
}

// The prefix is what keeps an ad-hoc charge from ever being mistaken for a
// campaign's in department reporting.
func TestChargeReferenceIsPrefixed(t *testing.T) {
	if ref := ChargeReferenceID("abc"); ref != "waba:abc" {
		t.Fatalf("charge reference = %q", ref)
	}
	if ref := RefundReferenceID("abc"); ref != "refund:waba:abc" {
		t.Fatalf("refund reference = %q", ref)
	}
}

func TestNeedsReconciliation(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)

	cases := []struct {
		name    string
		attempt SendAttempt
		want    bool
	}{
		{"charged and stale: money is held for nothing", SendAttempt{Status: SendAttemptCharged, UpdatedAt: old}, true},
		{"unknown and stale", SendAttempt{Status: SendAttemptUnknown, UpdatedAt: old}, true},
		{"charged but fresh: the send may still be in flight", SendAttempt{Status: SendAttemptCharged, UpdatedAt: now}, false},
		{"sent is settled", SendAttempt{Status: SendAttemptSent, UpdatedAt: old}, false},
		{"already refunded", SendAttempt{Status: SendAttemptRefunded, UpdatedAt: old}, false},
		{"pending never charged, nothing to return", SendAttempt{Status: SendAttemptPending, UpdatedAt: old}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.attempt.NeedsReconciliation(now, time.Hour); got != tc.want {
				t.Fatalf("NeedsReconciliation = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseProviderErrorPrefersMetaMessage(t *testing.T) {
	out := &conversation.SendTextMessageOutput{
		ResponsePayload: []byte(`{"error":{"code":132015,"message":"Template is paused"}}`),
	}
	code, msg := ParseProviderError(out)
	if code != 132015 || msg != "Template is paused" {
		t.Fatalf("code=%d msg=%q", code, msg)
	}

	if code, msg := ParseProviderError(nil); code != 0 || msg != "" {
		t.Fatalf("no payload should yield nothing, got %d %q", code, msg)
	}
}
