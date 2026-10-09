package leadsend_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/customfield"
	"vozko/domain/leadaction"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	template_usecase "vozko/usecases/whatsapp/template"
)

type sendPlan struct {
	channel  campaign.Channel
	params   leadaction.SendParams
	bindings campaign.BindingPlan
	template *template.Template
	instance *uw.Instance
	scope    uw.DepartmentScope
}

func (s *Service) plan(ctx context.Context, a Actor, action leadaction.Action, p leadaction.SendParams) (*sendPlan, error) {
	if !action.Sends() {
		return nil, fmt.Errorf("%w: %q", leadaction.ErrUnknownAction, action)
	}
	params := leadaction.Params{Send: &p}.Normalized()
	if err := params.Validate(action); err != nil {
		return nil, err
	}
	plan := &sendPlan{channel: action.Channel(), params: *params.Send}
	slots, err := s.target(ctx, a, plan)
	if err != nil {
		return nil, err
	}
	defs, err := s.deps.Definitions.ListByObject(a.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", a.WorkspaceID, err)
	}
	plan.bindings, err = campaign.PlanBindings(plan.params.Bindings, slots, defs)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *Service) target(ctx context.Context, a Actor, plan *sendPlan) (int, error) {
	if plan.channel == campaign.ChannelOfficial {
		tmpl, err := s.officialTemplate(a.WorkspaceID, plan.params.TemplateID)
		if err != nil {
			return 0, err
		}
		_, header := tmpl.GetBodyAndHeaderParameterNames()
		if err := campaign.RefuseSelectionTemplate(len(header), tmpl.IsNamedParameterFormat()); err != nil {
			return 0, err
		}
		plan.template = tmpl
		return tmpl.ParameterCount(), nil
	}
	scope, err := s.unofficialScope(a)
	if err != nil {
		return 0, err
	}
	if err := plan.params.Message.Validate(); err != nil {
		return 0, err
	}
	instance, err := s.deps.Unofficial.Instances.Usable(ctx, a.WorkspaceID, scope, plan.params.InstanceID)
	if err != nil {
		return 0, err
	}
	plan.instance, plan.scope = instance, scope
	return plan.params.Message.ParameterCount(), nil
}

func (s *Service) officialTemplate(workspaceID, templateID string) (*template.Template, error) {
	tmpl, err := s.deps.Templates.Get(workspaceID, templateID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", wc.ErrCampaignTemplateNotFound, err)
	}
	if tmpl == nil {
		return nil, wc.ErrCampaignTemplateNotFound
	}
	if err := tmpl.EnsureSendable(); err != nil {
		return nil, err
	}
	return tmpl, nil
}

func (s *Service) creation(ctx context.Context, a Actor, p leadaction.SendParams) (context.Context, error) {
	scope, ok := wd.GetCreationScope(ctx)
	if !ok || strings.TrimSpace(scope.UserID) == "" || scope.UserID != a.UserID {
		return nil, campaign.ErrCreationScopeMissing
	}
	if id := strings.TrimSpace(p.DepartmentID); id != "" {
		scope.RequestedDepartmentID = id
	}
	ctx = wd.WithCreationScope(ctx, scope)
	departmentID, err := s.deps.CreationDepartments.Resolve(ctx, a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if departmentID != "" {
		return ctx, nil
	}
	departments, err := s.deps.Departments.ListDepartments(a.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("departments of workspace %s: %w", a.WorkspaceID, err)
	}
	if len(departments) > 0 {
		return nil, campaign.ErrDepartmentRequired
	}
	return ctx, nil
}

func (s *Service) capRemaining(workspaceID string) (*int64, error) {
	usage, err := s.deps.Caps.MonthlySendCapUsage(workspaceID, s.now())
	if err != nil {
		return nil, fmt.Errorf("monthly send cap of workspace %s: %w", workspaceID, err)
	}
	if usage == nil {
		return nil, nil
	}
	remaining := usage.Remaining()
	return &remaining, nil
}

func (s *Service) priceOfficial(q *campaign.SendQuote, workspaceID string, tmpl *template.Template) error {
	cost, err := template_usecase.QuoteSend(s.deps.Costs, s.deps.Balances, workspaceID, tmpl, int64(q.Count))
	if err != nil {
		return err
	}
	remaining, err := s.capRemaining(workspaceID)
	if err != nil {
		return err
	}
	q.Category, q.UnitPriceMicros, q.CostMicros = cost.Category, cost.UnitPriceMicros, cost.CostMicros
	q.BalanceMicros, q.Currency, q.Affordable = cost.BalanceMicros, cost.Currency, cost.Affordable
	q.Judge(campaign.SendBudget{Eligible: q.Count, Priced: true, UnitPriceMicros: cost.UnitPriceMicros, BalanceMicros: cost.BalanceMicros, CapRemaining: remaining})
	return nil
}

func (s *Service) priceUnofficial(q *campaign.SendQuote, instance *uw.Instance, campaignDailyCap int) {
	draft := uwc.Campaign{DailyCap: campaignDailyCap}
	q.DailyCap = draft.EffectiveDailyCap(instance.EffectiveDailyCap(s.now()))
	q.EstimatedDays = campaign.EstimatedDays(q.Count, q.DailyCap)
	q.Affordable = true
	q.Judge(campaign.SendBudget{Eligible: q.Count})
}

func (s *Service) Quote(ctx context.Context, a Actor, action leadaction.Action, p leadaction.SendParams, selected int) (*campaign.SendQuote, error) {
	plan, err := s.plan(ctx, a, action, p)
	if err != nil {
		return nil, err
	}
	if _, err := s.creation(ctx, a, plan.params); err != nil {
		return nil, err
	}
	q := &campaign.SendQuote{}
	if err := q.Size(selected, plan.params.Split); err != nil {
		return nil, err
	}
	if plan.channel == campaign.ChannelOfficial {
		if err := s.priceOfficial(q, a.WorkspaceID, plan.template); err != nil {
			return nil, err
		}
		return q, nil
	}
	s.priceUnofficial(q, plan.instance, plan.params.DailyCap)
	return q, nil
}
