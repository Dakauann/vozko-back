package carriermessaging

import (
	"errors"
	"testing"
	"time"
)

func TestReferenceIsThirtyTwoAlphanumerics(t *testing.T) {
	got, err := ReferenceFor("0b7d2a3e-5f4c-4e8b-9a1d-2c3b4a5d6e7f")
	if err != nil || got != "0b7d2a3e5f4c4e8b9a1d2c3b4a5d6e7f" {
		t.Fatalf("ReferenceFor = %q, %v", got, err)
	}
	for _, bad := range []string{"", "short", "0b7d2a3e-5f4c-4e8b-9a1d-2c3b4a5d6e7f-extra", "0b7d2a3e_5f4c-4e8b-9a1d-2c3b4a5d6e7f"} {
		if _, err := ReferenceFor(bad); !errors.Is(err, ErrReferenceInvalid) {
			t.Fatalf("ReferenceFor(%q) = %v, want invalid", bad, err)
		}
	}
}

func TestStatusOnlyMovesForward(t *testing.T) {
	cases := []struct {
		from, to Status
		moves    bool
	}{
		{StatusPending, StatusAccepted, true},
		{StatusAccepted, StatusDelivered, true},
		{StatusDelivered, StatusRead, true},
		{StatusPending, StatusRead, true},
		{StatusRead, StatusDelivered, false},
		{StatusDelivered, StatusAccepted, false},
		{StatusAccepted, StatusFailed, true},
		{StatusPending, StatusRejected, true},
		{StatusAccepted, StatusExpired, true},
		{StatusDelivered, StatusFailed, false},
		{StatusRead, StatusExpired, false},
		{StatusFailed, StatusDelivered, true},
		{StatusFailed, StatusRejected, false},
		{StatusExpired, StatusDelivered, true},
		{StatusRead, StatusRead, false},
	}
	for _, tc := range cases {
		if got := tc.from.Supersedes(tc.to); got != tc.moves {
			t.Fatalf("%s -> %s moves = %v, want %v", tc.from, tc.to, got, tc.moves)
		}
	}
}

func TestAdvanceStampsTheFirstTimeOfEachMilestone(t *testing.T) {
	m := Message{Status: StatusPending}
	m, moved := m.Advance(StatusAccepted, "", t0)
	if !moved || m.AcceptedAt == nil {
		t.Fatalf("accepted = %+v", m)
	}
	m, _ = m.Advance(StatusRead, "", t0.Add(time.Minute))
	if m.Status != StatusRead || m.DeliveredAt == nil || m.ReadAt == nil || !m.DeliveredAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("read without a delivery report must stamp delivered too: %+v", m)
	}
	m, moved = m.Advance(StatusDelivered, "", t0.Add(2*time.Minute))
	if moved || m.Status != StatusRead {
		t.Fatalf("late delivered report moved the message: %+v", m)
	}
}

func TestAdvanceRecordsTheFailureCode(t *testing.T) {
	m := Message{Status: StatusAccepted}
	m, moved := m.Advance(StatusFailed, "42", t0)
	if !moved || m.FailedAt == nil || m.ErrorCode != "42" || !m.Status.Terminal() || m.Status.Landed() {
		t.Fatalf("failed = %+v", m)
	}
}

func TestLateDeliveryAfterExpiryStillCounts(t *testing.T) {
	m := Message{Status: StatusAccepted}
	m, _ = m.Advance(StatusExpired, "", t0)
	m, moved := m.Advance(StatusDelivered, "", t0.Add(time.Minute))
	if !moved || !m.Status.Landed() {
		t.Fatalf("delivery after expiry = %+v", m)
	}
}

func TestExpiredOnlyAfterTheValidityWithoutLanding(t *testing.T) {
	expires := t0.Add(30 * time.Minute)
	m := Message{Status: StatusAccepted, ExpiresAt: &expires}
	if m.Overdue(t0.Add(29 * time.Minute)) {
		t.Fatal("not overdue before validity")
	}
	if !m.Overdue(t0.Add(30 * time.Minute)) {
		t.Fatal("overdue at validity")
	}
	m.Status = StatusDelivered
	if m.Overdue(t0.Add(time.Hour)) {
		t.Fatal("a delivered message is never overdue")
	}
}

func TestFallbackChain(t *testing.T) {
	both := FallbackPlan{WhatsApp: true, SMS: true}
	smsOnly := FallbackPlan{SMS: true}
	cases := []struct {
		name    string
		step    Step
		status  Status
		plan    FallbackPlan
		want    Step
		advance bool
	}{
		{"rcs failed goes to whatsapp", StepRCS, StatusFailed, both, StepWhatsApp, true},
		{"rcs rejected goes to whatsapp", StepRCS, StatusRejected, both, StepWhatsApp, true},
		{"rcs expired goes to whatsapp", StepRCS, StatusExpired, both, StepWhatsApp, true},
		{"rcs failed without whatsapp goes to sms", StepRCS, StatusFailed, smsOnly, StepSMS, true},
		{"whatsapp failed goes to sms", StepWhatsApp, StatusFailed, both, StepSMS, true},
		{"sms is the last step", StepSMS, StatusFailed, both, "", false},
		{"delivered never falls back", StepRCS, StatusDelivered, both, "", false},
		{"accepted waits", StepRCS, StatusAccepted, both, "", false},
		{"no plan, no fallback", StepRCS, StatusFailed, FallbackPlan{}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.plan.Next(tc.step, tc.status)
			if ok != tc.advance || got != tc.want {
				t.Fatalf("Next = %s, %v; want %s, %v", got, ok, tc.want, tc.advance)
			}
		})
	}
}

func TestValidityShortensWhenAFallbackWaits(t *testing.T) {
	if got := (FallbackPlan{}).Validity(); got != DefaultValidity {
		t.Fatalf("Validity without fallback = %v", got)
	}
	if got := (FallbackPlan{SMS: true}).Validity(); got != FallbackValidity {
		t.Fatalf("Validity with fallback = %v", got)
	}
}

func TestFallbackSpecDecidesThePlan(t *testing.T) {
	spec := FallbackSpec{WhatsApp: &WhatsAppFallback{BusinessPhoneID: "p", TemplateID: "t"}, SMSRouteID: "r", SMSText: " "}
	if got := spec.Plan(); !got.WhatsApp || got.SMS {
		t.Fatalf("Plan = %+v, a blank SMS text is no SMS step", got)
	}
	if got := (FallbackSpec{SMSText: "Seu pedido saiu"}).Plan(); got.SMS {
		t.Fatalf("Plan = %+v, an SMS step needs its route", got)
	}
	if got := (FallbackSpec{SMSRouteID: "r", SMSText: "Seu pedido saiu"}).Plan(); got.WhatsApp || !got.SMS {
		t.Fatalf("Plan = %+v", got)
	}
}
