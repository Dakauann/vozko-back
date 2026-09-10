package comment_analysis_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	ca "vozko/domain/comment_analysis"
)

// Managing the alert rules, and firing one on demand to prove it works.
//
// The tenancy check is the same shape ManageCommentRulesUseCase uses next door:
// every write resolves the rule inside the caller's workspace first, so an id
// from elsewhere is not found rather than editable.

// ManageAlertRulesUseCase is the CRUD behind the settings panel.
type ManageAlertRulesUseCase struct {
	rules ca.AlertRuleRepository
	clock ca.Clock
}

func NewManageAlertRulesUseCase(rules ca.AlertRuleRepository, clock ca.Clock) *ManageAlertRulesUseCase {
	return &ManageAlertRulesUseCase{rules: rules, clock: clock}
}

func (uc *ManageAlertRulesUseCase) List(ctx context.Context, workspaceID string, source ca.Source, accountID string) ([]*ca.AlertRule, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ca.ErrInvalidFilter)
	}
	rows, err := uc.rules.ListByAccount(ctx, workspaceID, source, strings.TrimSpace(accountID))
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*ca.AlertRule{}
	}
	return rows, nil
}

func (uc *ManageAlertRulesUseCase) Create(ctx context.Context, rule ca.AlertRule) (*ca.AlertRule, error) {
	// The id is never the caller's to choose, and neither is the history: a
	// rule that arrived claiming it had already fired today would silence
	// itself, and one claiming it never had would skip its own cooldown.
	rule.ID = ""
	rule.LastFiredAt, rule.FiredToday, rule.FiredDay, rule.LastError = nil, 0, "", ""

	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := uc.rules.Create(ctx, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (uc *ManageAlertRulesUseCase) Update(ctx context.Context, workspaceID, id string, patch ca.AlertRule) (*ca.AlertRule, error) {
	current, err := uc.rules.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}

	// The account a rule watches is immutable, like the account on a comment
	// rule: moving it would silently repoint an alert somebody trusts.
	next := *current
	next.Name = patch.Name
	next.Enabled = patch.Enabled
	next.Metric = patch.Metric
	next.Threshold = patch.Threshold
	next.WindowMinutes = patch.WindowMinutes
	next.Channel = patch.Channel
	next.Recipient = patch.Recipient
	next.BusinessPhoneID = patch.BusinessPhoneID
	next.TemplateID = patch.TemplateID
	next.InstanceID = patch.InstanceID
	next.Brief = patch.Brief
	next.CooldownMinutes = patch.CooldownMinutes
	next.MaxPerDay = patch.MaxPerDay

	next.Normalize()
	if err := next.Validate(); err != nil {
		return nil, err
	}
	if err := uc.rules.Update(ctx, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (uc *ManageAlertRulesUseCase) Delete(ctx context.Context, workspaceID, id string) error {
	return uc.rules.Delete(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
}

// TestAlertRuleUseCase sends one alert on demand.
//
// It exists because the alternative way to find out whether a rule works is to
// wait for a bad day. It deliberately does NOT claim, so testing does not
// consume the cooldown or the daily cap that the real firing depends on, and it
// carries an idempotency key of its own so a double click does not send twice.
type TestAlertRuleUseCase struct {
	rules      ca.AlertRuleRepository
	dispatcher ca.AlertDispatcher
	clock      ca.Clock
}

func NewTestAlertRuleUseCase(rules ca.AlertRuleRepository, dispatcher ca.AlertDispatcher, clock ca.Clock) *TestAlertRuleUseCase {
	return &TestAlertRuleUseCase{rules: rules, dispatcher: dispatcher, clock: clock}
}

func (uc *TestAlertRuleUseCase) Execute(ctx context.Context, workspaceID, id, actorUserID string) error {
	if uc.dispatcher == nil {
		return fmt.Errorf("%w: no channel is configured to send alerts", ca.ErrInvalidFilter)
	}
	rule, err := uc.rules.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	// A rule that would be refused at firing time must be refused here too,
	// otherwise a test passes and the real alert never arrives.
	if err := rule.Validate(); err != nil {
		return err
	}

	now := uc.now()
	alert := ca.NewAlert(*rule, ca.AlertObservation{
		Metric: rule.Metric,
		Value:  rule.Threshold,
	}, now)

	// The person pressing Test is the author of THIS message, even if somebody
	// else armed the rule.
	actor := strings.TrimSpace(actorUserID)
	if actor == "" {
		actor = rule.CreatedByUserID
	}

	return uc.dispatcher.Dispatch(ctx, ca.AlertDelivery{
		WorkspaceID:     rule.WorkspaceID,
		Channel:         rule.Channel,
		Recipient:       rule.Recipient,
		BusinessPhoneID: rule.BusinessPhoneID,
		TemplateID:      rule.TemplateID,
		TemplateParams:  alert.TemplateParams(),
		Facts:           alert.Facts(),
		InstanceID:      rule.InstanceID,
		Text:            testPrefix + alert.Message(),
		// Its own key, so a test and a real firing at the same instant are two
		// different sends rather than one swallowing the other.
		IdempotencyKey: "test-" + alert.IdempotencyKey(),
		ActorUserID:    actor,
	})
}

// testPrefix marks the message so the recipient is not misled into acting on an
// incident that is not happening.
const testPrefix = "[TESTE] "

func (uc *TestAlertRuleUseCase) now() time.Time {
	if uc.clock != nil {
		return uc.clock.Now()
	}
	return time.Now().UTC()
}
