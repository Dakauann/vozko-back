package advertising

import (
	"context"
	"slices"

	ads "vozko/domain/advertising"
)

type ruleGateway interface {
	ListRules(ctx context.Context, token, metaAccountID string) ([]ads.AutomatedRule, error)
	CreateRule(ctx context.Context, token, metaAccountID, currency string, rule ads.AutomatedRule) (string, error)
	SetRuleStatus(ctx context.Context, token, ruleID string, status ads.RuleStatus) error
	DeleteRule(ctx context.Context, token, ruleID string) error
	RuleHistory(ctx context.Context, token, ruleID string) ([]ads.RuleRun, error)
}

type RulesUseCase struct {
	access  accountAccess
	gateway ruleGateway
	objects ads.ObjectRepository
}

func NewRulesUseCase(sync *SyncUseCase, gateway ruleGateway) *RulesUseCase {
	return &RulesUseCase{access: sync.access, gateway: gateway, objects: sync.objects}
}

var ruleLevels = map[ads.RuleEntity]ads.Level{ads.RuleCampaign: ads.LevelCampaign, ads.RuleAdSet: ads.LevelAdSet, ads.RuleAd: ads.LevelAd}

func (uc *RulesUseCase) List(ctx context.Context, workspaceID, accountID string) ([]ads.AutomatedRule, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.UseRead)
	if err != nil {
		return nil, err
	}
	rules, err := uc.gateway.ListRules(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	for i := range rules {
		rules[i].AdAccountID = account.ID
	}
	return rules, nil
}

func (uc *RulesUseCase) prepare(ctx context.Context, workspaceID string, rule *ads.AutomatedRule) (*ads.AdAccount, string, []*ads.Object, error) {
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, "", nil, err
	}
	account, token, err := uc.access.open(ctx, workspaceID, rule.AdAccountID, ads.UseWrite)
	if err != nil {
		return nil, "", nil, err
	}
	objects := make([]*ads.Object, 0, len(rule.ObjectIDs))
	for _, id := range rule.ObjectIDs {
		o, err := uc.objects.Find(ctx, workspaceID, id)
		if err != nil || o.AdAccountID != account.ID || o.Level != ruleLevels[rule.Entity] {
			return nil, "", nil, ads.FieldError("objectIds", "not_available")
		}
		objects = append(objects, o)
	}
	return account, token, objects, nil
}

type RuleCheck struct {
	Rule    ads.AutomatedRule
	Objects []*ads.Object
}

func (uc *RulesUseCase) Check(ctx context.Context, workspaceID string, rule ads.AutomatedRule) (*RuleCheck, error) {
	_, _, objects, err := uc.prepare(ctx, workspaceID, &rule)
	if err != nil {
		return nil, err
	}
	return &RuleCheck{Rule: rule, Objects: objects}, nil
}

func (uc *RulesUseCase) Create(ctx context.Context, workspaceID string, rule ads.AutomatedRule) (string, error) {
	account, token, _, err := uc.prepare(ctx, workspaceID, &rule)
	if err != nil {
		return "", err
	}
	id, err := uc.gateway.CreateRule(ctx, token, account.MetaAccountID, account.Currency, rule)
	if err != nil {
		return "", uc.access.failed(ctx, account, err)
	}
	return id, nil
}

func (uc *RulesUseCase) owned(ctx context.Context, workspaceID, accountID, ruleID string, use ads.AccountUse) (*ads.AdAccount, string, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, use)
	if err != nil {
		return nil, "", err
	}
	rules, err := uc.gateway.ListRules(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, "", uc.access.failed(ctx, account, err)
	}
	if !slices.ContainsFunc(rules, func(r ads.AutomatedRule) bool { return r.MetaID == ruleID }) {
		return nil, "", ads.ErrRuleNotFound
	}
	return account, token, nil
}

func (uc *RulesUseCase) SetEnabled(ctx context.Context, workspaceID, accountID, ruleID string, enabled bool) error {
	account, token, err := uc.owned(ctx, workspaceID, accountID, ruleID, ads.UseWrite)
	if err != nil {
		return err
	}
	status := ads.RuleDisabled
	if enabled {
		status = ads.RuleEnabled
	}
	return uc.access.failed(ctx, account, uc.gateway.SetRuleStatus(ctx, token, ruleID, status))
}

func (uc *RulesUseCase) Delete(ctx context.Context, workspaceID, accountID, ruleID string) error {
	account, token, err := uc.owned(ctx, workspaceID, accountID, ruleID, ads.UseWrite)
	if err != nil {
		return err
	}
	return uc.access.failed(ctx, account, uc.gateway.DeleteRule(ctx, token, ruleID))
}

func (uc *RulesUseCase) History(ctx context.Context, workspaceID, accountID, ruleID string) ([]ads.RuleRun, error) {
	account, token, err := uc.owned(ctx, workspaceID, accountID, ruleID, ads.UseRead)
	if err != nil {
		return nil, err
	}
	runs, err := uc.gateway.RuleHistory(ctx, token, ruleID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	return runs, nil
}
