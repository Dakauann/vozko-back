package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/callsession"
	"vozko/domain/lead"
	"vozko/domain/sip_trunk"
	"vozko/domain/workspace"
)

var errDialTargetsIncomplete = errors.New("lead dial targets: missing dependency")

type DialLeads interface {
	LoadForDial(ctx context.Context, workspaceID, leadID string) (*lead.Lead, error)
	FindIdentities(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error)
}

type DialTargetDeps struct {
	Leads       DialLeads
	Permissions Permissions
	Planner     sip_trunk.CallPlanner
	Lines       sip_trunk.CallLines
}

type DialTargets struct {
	deps DialTargetDeps
}

var _ callsession.LeadDialTargets = (*DialTargets)(nil)

func NewDialTargets(deps DialTargetDeps) (*DialTargets, error) {
	missing := map[string]bool{
		"leads":       deps.Leads == nil,
		"permissions": deps.Permissions == nil,
		"planner":     deps.Planner == nil,
		"lines":       deps.Lines == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errDialTargetsIncomplete, name)
		}
	}
	return &DialTargets{deps: deps}, nil
}

func (d *DialTargets) CheckLead(ctx context.Context, dial callsession.LeadDial) error {
	l, err := d.dialable(ctx, dial.WorkspaceID, dial.LeadID)
	if errors.Is(err, lead.ErrLeadNotFound) {
		return &lead.DialRefusal{Reason: lead.DialRefusedNotFound}
	}
	if err != nil {
		return err
	}
	identities, err := d.identities(ctx, dial.WorkspaceID, []string{dial.Number})
	if err != nil {
		return err
	}
	return lead.CheckLeadDial(l, dial.Number, identities, lead.DialContext{Purpose: dial.Purpose})
}

func (d *DialTargets) IdentityLead(ctx context.Context, workspaceID, number string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return "", lead.ErrLeadWorkspaceRequired
	}
	identities, err := d.identities(ctx, workspaceID, []string{number})
	if err != nil {
		return "", err
	}
	if len(identities) == 0 {
		return "", nil
	}
	holder, err := lead.CheckNumberDial(number, identities, lead.DialContext{Purpose: lead.DialDirect})
	if err != nil || holder == nil {
		return "", err
	}
	return holder.ID, nil
}

func (d *DialTargets) identities(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error) {
	identities, err := d.deps.Leads.FindIdentities(ctx, workspaceID, numbers)
	if err != nil {
		return nil, fmt.Errorf("leads whose WhatsApp is the dialed number: %w", err)
	}
	return identities, nil
}

func (d *DialTargets) dialable(ctx context.Context, workspaceID, leadID string) (*lead.Lead, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	l, err := d.deps.Leads.LoadForDial(ctx, workspaceID, strings.TrimSpace(leadID))
	if err != nil {
		return nil, err
	}
	if l == nil || l.WorkspaceID != workspaceID {
		return nil, lead.ErrLeadNotFound
	}
	return l, nil
}

type TrunkRefusal string

const (
	TrunkRefusedNotPermitted TrunkRefusal = "unauthorized"
	TrunkRefusedNoneDialable TrunkRefusal = "no_dialable_trunk"
)

type LeadDialPlan struct {
	LeadID string
	lead.DialPlan
	Trunks       []sip_trunk.TrunkChoice
	TrunkRefusal TrunkRefusal
}

func (d *DialTargets) Plan(ctx context.Context, actor Actor, leadID string) (*LeadDialPlan, error) {
	if strings.TrimSpace(actor.WorkspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if !(viewers{permissions: d.deps.Permissions}).allowed(actor, workspace.ActionRead) {
		return nil, lead.ErrLeadForbidden
	}
	l, err := d.dialable(ctx, actor.WorkspaceID, leadID)
	if err != nil {
		return nil, err
	}
	numbers := l.DialNumbers()
	dialed := make([]string, 0, len(numbers))
	for _, number := range numbers {
		dialed = append(dialed, number.Number)
	}
	identities, err := d.identities(ctx, actor.WorkspaceID, dialed)
	if err != nil {
		return nil, err
	}
	plan := &LeadDialPlan{LeadID: l.ID, DialPlan: l.PlanDial(identities, lead.DialContext{Purpose: lead.DialDirect})}
	for plan.Callable != "" {
		lines, err := d.NumberLines(ctx, actor, plan.Callable)
		if errors.Is(err, sip_trunk.ErrInvalidPhoneNumber) {
			plan.Refuse(plan.Callable, lead.DialRefusedInvalidNumber)
			continue
		}
		if lines == nil {
			return plan, err
		}
		plan.Trunks, plan.TrunkRefusal = lines.Trunks, lines.TrunkRefusal
		return plan, err
	}
	return plan, nil
}

func (d *DialTargets) Lines(ctx context.Context, actor Actor) (*LeadDialPlan, error) {
	if strings.TrimSpace(actor.WorkspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	lines, err := d.deps.Lines.Lines(ctx, sip_trunk.CallPlanInput{WorkspaceID: actor.WorkspaceID, UserID: actor.UserID, IsAdmin: actor.IsAdmin})
	plan := &LeadDialPlan{}
	return plan, plan.offer(&sip_trunk.CallPlan{Trunks: lines}, err)
}

func (d *DialTargets) NumberLines(ctx context.Context, actor Actor, number string) (*LeadDialPlan, error) {
	if strings.TrimSpace(actor.WorkspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if strings.TrimSpace(number) == "" {
		return nil, sip_trunk.ErrInvalidPhoneNumber
	}
	trunks, err := d.deps.Planner.Plan(ctx, sip_trunk.CallPlanInput{
		WorkspaceID: actor.WorkspaceID, UserID: actor.UserID, IsAdmin: actor.IsAdmin, PhoneNumber: number,
	})
	if errors.Is(err, sip_trunk.ErrInvalidPhoneNumber) {
		return nil, err
	}
	plan := &LeadDialPlan{}
	return plan, plan.offer(trunks, err)
}

func (p *LeadDialPlan) offer(trunks *sip_trunk.CallPlan, err error) error {
	switch {
	case errors.Is(err, sip_trunk.ErrCallNotPermitted):
		p.TrunkRefusal = TrunkRefusedNotPermitted
	case errors.Is(err, sip_trunk.ErrNoDialableTrunk):
		p.TrunkRefusal = TrunkRefusedNoneDialable
	case err != nil:
		return fmt.Errorf("lines that can call lead %s: %w", p.LeadID, err)
	case trunks == nil:
		return fmt.Errorf("lines that can call lead %s: no plan", p.LeadID)
	default:
		p.Trunks = trunks.Trunks
	}
	return nil
}
