package audience_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/audience"
)

type stubDirectory struct {
	statuses []ca.AlertChannelStatus
	err      error
	calls    int
}

func (s *stubDirectory) ChannelStatus(context.Context, string) ([]ca.AlertChannelStatus, error) {
	s.calls++
	return s.statuses, s.err
}

func armedRule() ca.AlertRule {
	return ca.AlertRule{
		WorkspaceID: "ws-1",
		AccountID:   "acc-1",
		Name:        "Alerta maximo",
		Metric:      ca.AlertMetricCommentSeverity,
		Threshold:   80,
		Channel:     ca.AlertChannelUnofficial,
		Recipient:   "558494409624",
		Enabled:     true,
	}
}

// The production case: no connected number, rule armed anyway. The save is
// refused here rather than at the first firing, which is where the operator
// used to find out.
func TestCreate_RefusesAnArmedRuleWithNoSender(t *testing.T) {
	rules := &fakeAlertRules{}
	dir := &stubDirectory{statuses: []ca.AlertChannelStatus{
		{Channel: ca.AlertChannelUnofficial, Available: false, Reason: ca.AlertChannelReasonNoSender},
	}}
	uc := NewManageAlertRulesUseCase(rules, fixedClock{time.Now()}).WithSenderDirectory(dir)

	if _, err := uc.Create(context.Background(), armedRule()); !errors.Is(err, ca.ErrChannelUnavailable) {
		t.Fatalf("err = %v, want ErrChannelUnavailable", err)
	}
	if rules.created != nil {
		t.Fatal("nothing may reach the repository once the channel is refused")
	}
}

func TestCreate_AllowsAnArmedRuleWithAConnectedSender(t *testing.T) {
	rules := &fakeAlertRules{}
	dir := &stubDirectory{statuses: []ca.AlertChannelStatus{
		{
			Channel:   ca.AlertChannelUnofficial,
			Available: true,
			Senders:   []ca.AlertSender{{ID: "inst-1", Label: "Comercial"}},
		},
	}}
	uc := NewManageAlertRulesUseCase(rules, fixedClock{time.Now()}).WithSenderDirectory(dir)

	if _, err := uc.Create(context.Background(), armedRule()); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if rules.created == nil {
		t.Fatal("the rule should have been created")
	}
}

// Enabling an existing rule is the other door into the same lie, and it used
// to be wide open: the rule was already saved, so flipping the switch never
// re-asked whether it could send.
func TestUpdate_RefusesEnablingARuleWhoseChannelWentAway(t *testing.T) {
	existing := armedRule()
	existing.ID = "rule-1"
	existing.Enabled = false
	rules := &fakeAlertRules{rules: []*ca.AlertRule{&existing}}
	dir := &stubDirectory{statuses: []ca.AlertChannelStatus{
		{Channel: ca.AlertChannelUnofficial, Available: false, Reason: ca.AlertChannelReasonNoSender},
	}}
	uc := NewManageAlertRulesUseCase(rules, fixedClock{time.Now()}).WithSenderDirectory(dir)

	patch := armedRule()
	patch.Enabled = true
	if _, err := uc.Update(context.Background(), "ws-1", "rule-1", patch); !errors.Is(err, ca.ErrChannelUnavailable) {
		t.Fatalf("err = %v, want ErrChannelUnavailable", err)
	}
	if rules.updated != nil {
		t.Fatal("nothing may reach the repository once the channel is refused")
	}
}

// Turning a rule OFF must always work, whatever the channel is doing. Refusing
// it would trap an operator with an alert they cannot disable, which is a
// worse bug than the one being fixed.
func TestUpdate_DisablingIsAlwaysAllowed(t *testing.T) {
	existing := armedRule()
	existing.ID = "rule-1"
	rules := &fakeAlertRules{rules: []*ca.AlertRule{&existing}}
	dir := &stubDirectory{statuses: []ca.AlertChannelStatus{
		{Channel: ca.AlertChannelUnofficial, Available: false, Reason: ca.AlertChannelReasonNoSender},
	}}
	uc := NewManageAlertRulesUseCase(rules, fixedClock{time.Now()}).WithSenderDirectory(dir)

	patch := armedRule()
	patch.Enabled = false
	if _, err := uc.Update(context.Background(), "ws-1", "rule-1", patch); err != nil {
		t.Fatalf("disabling must always be possible, got %v", err)
	}
	if rules.updated == nil {
		t.Fatal("the rule should have been updated")
	}
}

// No directory wired means the deployment cannot answer. Saving keeps working
// exactly as before rather than every rule in the product becoming unsaveable.
func TestCreate_WithoutADirectoryBehavesAsBefore(t *testing.T) {
	rules := &fakeAlertRules{}
	uc := NewManageAlertRulesUseCase(rules, fixedClock{time.Now()})

	if _, err := uc.Create(context.Background(), armedRule()); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if rules.created == nil {
		t.Fatal("the rule should have been created")
	}
}

// A directory that errors is an outage in the thing being asked, not a verdict
// on the rule. Blocking every save because the instance table was briefly
// unreachable trades a silent bug for a loud one.
func TestCreate_ADirectoryOutageDoesNotBlockTheSave(t *testing.T) {
	rules := &fakeAlertRules{}
	dir := &stubDirectory{err: errors.New("db is down")}
	uc := NewManageAlertRulesUseCase(rules, fixedClock{time.Now()}).WithSenderDirectory(dir)

	if _, err := uc.Create(context.Background(), armedRule()); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if rules.created == nil {
		t.Fatal("the rule should have been created")
	}
}

// The picker reads the same source the validator does, so what the form offers
// and what the save accepts cannot drift apart. That drift IS the bug.
func TestChannelsUseCase_ReportsWhatTheValidatorWillEnforce(t *testing.T) {
	dir := &stubDirectory{statuses: []ca.AlertChannelStatus{
		{Channel: ca.AlertChannelOfficial, Available: true, Senders: []ca.AlertSender{{ID: "p1"}}},
		{Channel: ca.AlertChannelUnofficial, Available: false, Reason: ca.AlertChannelReasonNoSender},
	}}
	uc := NewGetAlertChannelsUseCase(dir)

	got, err := uc.Execute(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both channels reported, got %d", len(got))
	}
	if dir.calls != 1 {
		t.Fatalf("expected one directory read, got %d", dir.calls)
	}
}

// Without a directory the picker falls back to the full vocabulary rather than
// offering nothing, which would make the panel unusable in a deployment that
// never wired one.
func TestChannelsUseCase_WithoutADirectoryReportsEverythingAvailable(t *testing.T) {
	uc := NewGetAlertChannelsUseCase(nil)

	got, err := uc.Execute(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the full vocabulary, got %d", len(got))
	}
	for _, s := range got {
		if !s.Available {
			t.Fatalf("%s should be assumed available with no directory", s.Channel)
		}
	}
}
