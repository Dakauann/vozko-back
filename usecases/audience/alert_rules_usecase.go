package audience_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	ca "vozko/domain/audience"
)

type ManageAlertRulesUseCase struct {
	rules   ca.AlertRuleRepository
	clock   ca.Clock
	senders ca.AlertSenderDirectory
}

func NewManageAlertRulesUseCase(rules ca.AlertRuleRepository, clock ca.Clock) *ManageAlertRulesUseCase {
	return &ManageAlertRulesUseCase{rules: rules, clock: clock}
}

func (uc *ManageAlertRulesUseCase) WithSenderDirectory(d ca.AlertSenderDirectory) *ManageAlertRulesUseCase {
	uc.senders = d
	return uc
}

func (uc *ManageAlertRulesUseCase) checkSender(ctx context.Context, rule ca.AlertRule) error {
	if uc.senders == nil {
		return nil
	}
	statuses, err := uc.senders.ChannelStatus(ctx, rule.WorkspaceID)
	if err != nil {
		return nil
	}
	return rule.ValidateSender(statuses)
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
	rule.ID = ""
	rule.LastFiredAt, rule.FiredToday, rule.FiredDay, rule.LastError = nil, 0, "", ""

	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := uc.checkSender(ctx, rule); err != nil {
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

	next := *current
	next.Name = patch.Name
	next.Enabled = patch.Enabled
	next.Metric = patch.Metric
	next.Threshold = patch.Threshold
	next.WindowMinutes = patch.WindowMinutes
	next.MinMessages = patch.MinMessages
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
	if err := uc.checkSender(ctx, next); err != nil {
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
	if err := rule.Validate(); err != nil {
		return err
	}

	now := uc.now()
	alert := ca.NewAlert(*rule, ca.AlertObservation{
		Metric: rule.Metric,
		Value:  rule.Threshold,
	}, now)

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
		Text:            testPrefix + alert.Message() + testFooter(*rule),
		IdempotencyKey:  "test-" + alert.IdempotencyKey(),
		ActorUserID:     actor,
	})
}

const testPrefix = "[TESTE] "

func testFooter(rule ca.AlertRule) string {
	footer := "\n\nEste é um teste: o número acima é o limite configurado, não uma medição, e não há conversa por trás dele."
	if rule.Brief {
		footer += " A leitura da IA só é gerada em um disparo real."
	}
	return footer
}

func (uc *TestAlertRuleUseCase) now() time.Time {
	if uc.clock != nil {
		return uc.clock.Now()
	}
	return time.Now().UTC()
}
