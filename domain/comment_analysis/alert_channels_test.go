package comment_analysis

import (
	"errors"
	"testing"
)

func readyRule(mutate func(*AlertRule)) AlertRule {
	r := AlertRule{
		WorkspaceID: "ws-1",
		AccountID:   "acc-1",
		Name:        "Alerta maximo",
		Metric:      AlertMetricCommentSeverity,
		Threshold:   80,
		Channel:     AlertChannelUnofficial,
		Recipient:   "558494409624",
		Enabled:     true,
	}
	if mutate != nil {
		mutate(&r)
	}
	return r
}

func statuses(list ...AlertChannelStatus) []AlertChannelStatus { return list }

// The bug this whole contract exists for, reported from production 2026-09-10.
//
// A workspace with zero connected numbers could arm an unofficial rule. It
// saved, it displayed "Regra ativa", and it could not send a single message.
// The failure only surfaced in LastError after the first firing, which is the
// worst possible moment: the operator has already stopped watching manually
// because they believe the alert has them covered.
func TestValidateSender_EnabledRuleOnAChannelWithNoSender(t *testing.T) {
	rule := readyRule(nil)
	err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelUnofficial,
		Available: false,
		Reason:    AlertChannelReasonNoSender,
	}))
	if !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("err = %v, want ErrChannelUnavailable", err)
	}
}

// Drafting stays possible. The rule that cannot fire is only a lie while it
// claims to be armed, so a DISABLED rule on an unusable channel is a perfectly
// reasonable thing to save before connecting the number.
func TestValidateSender_DisabledRuleMaySitOnAnUnusableChannel(t *testing.T) {
	rule := readyRule(func(r *AlertRule) { r.Enabled = false })
	if err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelUnofficial,
		Available: false,
		Reason:    AlertChannelReasonNoSender,
	})); err != nil {
		t.Fatalf("a disabled draft must be saveable, got %v", err)
	}
}

// One connected number is the common case, and the rule is allowed to name
// none: AlertRule.InstanceID has always documented empty as "whichever one
// this workspace has", and with exactly one there is no ambiguity to resolve.
func TestValidateSender_OneSenderNeedsNoChoice(t *testing.T) {
	rule := readyRule(nil)
	if err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelUnofficial,
		Available: true,
		Senders:   []AlertSender{{ID: "inst-1", Label: "Comercial"}},
	})); err != nil {
		t.Fatalf("one sender resolves itself, got %v", err)
	}
}

// With more than one, "whichever one this workspace has" stops being an
// answer. Leaving it to the send path would pick for the operator, silently,
// and possibly from the wrong number.
func TestValidateSender_SeveralSendersForceAChoice(t *testing.T) {
	rule := readyRule(nil)
	err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelUnofficial,
		Available: true,
		Senders: []AlertSender{
			{ID: "inst-1", Label: "Comercial"},
			{ID: "inst-2", Label: "Suporte"},
		},
	}))
	if !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("err = %v, want a demand to choose a sender", err)
	}
}

func TestValidateSender_SeveralSendersAcceptANamedOne(t *testing.T) {
	rule := readyRule(func(r *AlertRule) { r.InstanceID = "inst-2" })
	if err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelUnofficial,
		Available: true,
		Senders: []AlertSender{
			{ID: "inst-1", Label: "Comercial"},
			{ID: "inst-2", Label: "Suporte"},
		},
	})); err != nil {
		t.Fatalf("a named sender is the answer, got %v", err)
	}
}

// A number can be disconnected or banned after the rule was written. The rule
// still points at it, and it is now exactly as dead as having none at all.
func TestValidateSender_NamedSenderThatIsGone(t *testing.T) {
	rule := readyRule(func(r *AlertRule) { r.InstanceID = "inst-gone" })
	err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelUnofficial,
		Available: true,
		Senders:   []AlertSender{{ID: "inst-1", Label: "Comercial"}},
	}))
	if !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("err = %v, want ErrChannelUnavailable for a sender that is gone", err)
	}
}

// The official channel is checked the same way. It already failed LOUDLY
// (Validate requires a business phone and a template), so this adds the
// missing half: the phone it names must still exist.
func TestValidateSender_OfficialChannelIsCheckedToo(t *testing.T) {
	rule := readyRule(func(r *AlertRule) {
		r.Channel = AlertChannelOfficial
		r.BusinessPhoneID = "phone-gone"
		r.TemplateID = "tpl-1"
	})
	err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelOfficial,
		Available: true,
		Senders:   []AlertSender{{ID: "phone-1", Label: "+55 11 9999-0000"}},
	}))
	if !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("err = %v, want the official phone to be checked as well", err)
	}
}

// A channel the directory says nothing about is unavailable, not assumed fine.
// Silence from the thing that knows is the same answer as "no".
func TestValidateSender_UnknownChannelIsNotAssumedReady(t *testing.T) {
	rule := readyRule(nil)
	err := rule.ValidateSender(statuses(AlertChannelStatus{
		Channel:   AlertChannelOfficial,
		Available: true,
		Senders:   []AlertSender{{ID: "phone-1"}},
	}))
	if !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("err = %v, want an unlisted channel to be refused", err)
	}
}

// No directory at all means the deployment could not be asked, and this check
// stands down rather than blocking every rule in the product. The check is a
// guard against a silent lie, not a new dependency for saving anything.
func TestValidateSender_NoStatusesMeansNoOpinion(t *testing.T) {
	rule := readyRule(nil)
	if err := rule.ValidateSender(nil); err != nil {
		t.Fatalf("an unanswerable question must not block the save, got %v", err)
	}
}

// The reason travels so the UI can say WHY rather than greying a control out
// with no explanation, which is the thing that sends people to support.
func TestChannelStatusFor(t *testing.T) {
	list := statuses(
		AlertChannelStatus{Channel: AlertChannelOfficial, Available: true},
		AlertChannelStatus{Channel: AlertChannelUnofficial, Available: false, Reason: AlertChannelReasonNoSender},
	)
	got, ok := ChannelStatusFor(list, AlertChannelUnofficial)
	if !ok {
		t.Fatal("expected the unofficial status to be found")
	}
	if got.Available || got.Reason != AlertChannelReasonNoSender {
		t.Fatalf("unexpected status %+v", got)
	}
	if _, ok := ChannelStatusFor(list, AlertChannel("voice")); ok {
		t.Fatal("a channel that is not in the list must not be found")
	}
}
