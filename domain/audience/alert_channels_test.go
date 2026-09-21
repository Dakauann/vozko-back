package audience

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

func TestValidateSender_NoStatusesMeansNoOpinion(t *testing.T) {
	rule := readyRule(nil)
	if err := rule.ValidateSender(nil); err != nil {
		t.Fatalf("an unanswerable question must not block the save, got %v", err)
	}
}

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
