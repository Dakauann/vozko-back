package leadsend_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

type PrepareRequest struct {
	Actor       Actor
	Departments *wd.DepartmentFilter
	Action      leadaction.Action
	Params      leadaction.SendParams
	SnapshotID  string
	Base        string
}

type target struct {
	leadID    string
	variables []string
	skip      campaign.SkipReason
	missing   []campaign.MissingVariable
}

func (s *Service) Existing(ctx context.Context, a Actor, departments *wd.DepartmentFilter, action leadaction.Action, base string) (*campaign.SendReview, error) {
	if !action.Sends() {
		return nil, fmt.Errorf("%w: %q", leadaction.ErrUnknownAction, action)
	}
	found, err := s.deps.Store.KeyedParts(ctx, action.Channel(), a.WorkspaceID, base)
	if err != nil {
		return nil, err
	}
	ids, complete := campaign.CompleteParts(base, found)
	if !complete {
		return nil, nil
	}
	review, _, err := s.review(ctx, a, departments, action.Channel(), ids)
	return review, err
}

func (s *Service) Prepare(ctx context.Context, req PrepareRequest) (*campaign.SendReview, error) {
	a := req.Actor
	began := s.now()
	if strings.TrimSpace(req.Base) == "" || strings.TrimSpace(req.SnapshotID) == "" {
		return nil, leadaction.ErrSnapshotLost
	}
	plan, err := s.plan(ctx, a, req.Action, req.Params)
	if err != nil {
		return nil, err
	}
	ctx, err = s.creation(ctx, a, plan.params)
	if err != nil {
		return nil, err
	}
	targets, err := s.targets(ctx, a.WorkspaceID, req.SnapshotID, plan)
	if err != nil {
		return nil, err
	}
	sizes, err := campaign.SplitSelection(len(targets), plan.params.Split)
	if err != nil {
		return nil, err
	}
	keys := campaign.PartKeys(req.Base, len(sizes))
	ids := make([]string, 0, len(sizes))
	offset := 0
	for i, size := range sizes {
		id, err := s.createPart(ctx, a, plan, partName(plan.params.Name, i+1, len(sizes)), keys[i], targets[offset:offset+size])
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		offset += size
	}
	s.dropSnapshot(ctx, a.WorkspaceID, req.SnapshotID)
	review, _, err := s.review(ctx, a, req.Departments, plan.channel, ids)
	if err != nil {
		return nil, err
	}
	s.countPrepared(req.Action, review, began)
	RunLog(req.Base, a.WorkspaceID, req.Action).Info("lead send: prepared", "actor_id", a.UserID, "campaign_ids", ids,
		"entries", review.Entries, "eligible", review.Eligible)
	return review, nil
}

func partName(name string, part, parts int) string {
	if parts <= 1 {
		return name
	}
	return fmt.Sprintf("%s (%d/%d)", name, part, parts)
}

func (s *Service) dropSnapshot(ctx context.Context, workspaceID, snapshotID string) {
	if err := s.deps.Snapshots.DropSnapshot(context.WithoutCancel(ctx), workspaceID, snapshotID); err != nil {
		slog.Warn("lead send: the frozen selection stays until the sweep", "run_id", snapshotID, "workspace_id", workspaceID, "error", err)
	}
}

func (s *Service) targets(ctx context.Context, workspaceID, snapshotID string, plan *sendPlan) ([]target, error) {
	var out []target
	after := ""
	for {
		ids, err := s.deps.Snapshots.Snapshot(ctx, workspaceID, snapshotID, after, LeadChunk)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return out, nil
		}
		chunk, err := s.chunkTargets(ctx, workspaceID, ids, plan)
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if len(ids) < LeadChunk {
			return out, nil
		}
		after = ids[len(ids)-1]
	}
}

func (s *Service) chunkTargets(ctx context.Context, workspaceID string, ids []string, plan *sendPlan) ([]target, error) {
	var running map[string]bool
	var subjects map[string]campaign.BindingSubject
	err := s.gated(ctx, func(ctx context.Context) error {
		var err error
		if running, err = s.deps.Store.InRunningCampaigns(ctx, workspaceID, ids); err != nil {
			return err
		}
		subjects, err = s.subjects(ctx, workspaceID, ids, plan.bindings)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]target, 0, len(ids))
	for _, id := range ids {
		values, err := plan.bindings.Resolve(subjects[id])
		missing := errors.Is(err, campaign.ErrMissingVariable)
		if err != nil && !missing {
			return nil, err
		}
		slots, _ := campaign.MissingVariablesOf(err)
		out = append(out, target{leadID: id, variables: values, skip: campaign.CampaignSkip(running[id], missing, false), missing: slots})
	}
	return out, nil
}

func (s *Service) subjects(ctx context.Context, workspaceID string, ids []string, bindings campaign.BindingPlan) (map[string]campaign.BindingSubject, error) {
	subjects := make(map[string]campaign.BindingSubject, len(ids))
	if !bindings.NeedsLead() {
		return subjects, nil
	}
	found, err := s.deps.Leads.FindByIDs(workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("leads of the send: %w", err)
	}
	leads := make([]*lead.Lead, 0, len(found))
	for _, l := range found {
		if l != nil && l.WorkspaceID == workspaceID {
			leads = append(leads, l)
		}
	}
	if bindings.NeedsAddresses() {
		if err := s.deps.Contacts.AttachContactDetails(ctx, workspaceID, leads); err != nil {
			return nil, fmt.Errorf("addresses of the send: %w", err)
		}
	}
	names := map[string]string{}
	if bindings.NeedsOwnerNames() {
		var owners []string
		for _, l := range leads {
			if l.Owner != "" {
				owners = append(owners, l.Owner)
			}
		}
		if len(owners) > 0 {
			names = s.deps.Names.Names(owners...)
		}
	}
	for _, l := range leads {
		subjects[l.ID] = campaign.BindingSubject{Lead: l, OwnerName: names[l.Owner]}
	}
	return subjects, nil
}

func (s *Service) createPart(ctx context.Context, a Actor, plan *sendPlan, name, key string, targets []target) (string, error) {
	p := plan.params
	if plan.channel == campaign.ChannelOfficial {
		inputs := make([]wc.PhoneInput, 0, len(targets))
		for _, t := range targets {
			inputs = append(inputs, wc.PhoneInput{LeadID: t.leadID, Variables: t.variables, Skip: t.skip, Missing: t.missing})
		}
		created, err := s.deps.Official.Create.Execute(ctx, &wc.Campaign{
			WorkspaceID: a.WorkspaceID, Name: name, Type: wc.CampaignTypeStandard, TemplateID: p.TemplateID, BusinessPhoneID: p.BusinessPhoneID,
			Status: wc.CampaignStatusStopped, Source: campaign.SourceLeadSelection, IdempotencyKey: key, PhoneInputs: inputs,
		})
		if err != nil {
			return "", err
		}
		return created.ID, nil
	}
	inputs := make([]uwc.TargetInput, 0, len(targets))
	for _, t := range targets {
		inputs = append(inputs, uwc.TargetInput{LeadID: t.leadID, Variables: t.variables, Skip: t.skip, Missing: t.missing})
	}
	created, err := s.deps.Unofficial.Create.Execute(ctx, &uwc.Campaign{
		WorkspaceID: a.WorkspaceID, InstanceID: p.InstanceID, CreatedByID: a.UserID, Name: name, Message: *p.Message,
		SendDelayMinMS: p.SendDelayMinMS, SendDelayMaxMS: p.SendDelayMaxMS, DailyCap: p.DailyCap,
		Status: campaign.StatusStopped, Source: campaign.SourceLeadSelection, IdempotencyKey: key, Targets: inputs,
	}, plan.scope)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}
