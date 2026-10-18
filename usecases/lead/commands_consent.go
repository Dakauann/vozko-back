package lead_usecase

import (
	"context"
	"errors"
	"slices"

	"vozko/domain/lead"
	"vozko/domain/workspace"
)

var operatorConsentSources = []lead.ConsentSource{lead.ConsentManual, lead.ConsentConversationRequest, lead.ConsentAPI}

type ConsentInput struct {
	Kind        lead.ConsentKind
	Source      lead.ConsentSource
	Purpose     string
	Evidence    lead.ConsentEvidence
	RequestedBy lead.OptOutSource
}

type ConsentResult struct {
	Lead    *lead.Lead
	Changed bool
	EventID string
}

func consentActions(optIn *bool) []workspace.Action {
	if optIn != nil && *optIn {
		return []workspace.Action{workspace.ActionConsent}
	}
	return nil
}

func consentKindActions(kind lead.ConsentKind) []workspace.Action {
	if kind == lead.ConsentGrant {
		return []workspace.Action{workspace.ActionUpdate, workspace.ActionConsent}
	}
	return []workspace.Action{workspace.ActionUpdate}
}

func (c *Commands) RecordConsent(ctx context.Context, a Actor, id string, in ConsentInput) (ConsentResult, error) {
	v, err := c.authorize(a, consentKindActions(in.Kind)...)
	if err != nil {
		return ConsentResult{}, err
	}
	if !slices.Contains(operatorConsentSources, in.Source) {
		return ConsentResult{}, lead.ErrConsentSourceForbidden
	}
	change := lead.ConsentChange{
		Kind: in.Kind, Source: in.Source, Actor: lead.ConsentActor{Type: lead.ConsentActorUser, ID: a.UserID},
		Purpose: in.Purpose, Evidence: in.Evidence, RequestedBy: in.RequestedBy,
	}
	sent := []string{lead.FieldOptedOut, lead.FieldWhatsAppOptIn}
	out, err := c.consent.Record(ctx, a.WorkspaceID, id, change)
	if errors.Is(err, errStillContended) {
		return ConsentResult{}, c.conflict(ctx, a, v, id, sent)
	}
	if err != nil {
		return ConsentResult{}, err
	}
	return ConsentResult{Lead: out.Lead.VisibleTo(v, sent), Changed: out.Changed, EventID: out.EventID}, nil
}

func (c *Commands) OptOut(ctx context.Context, a Actor, id string, source lead.OptOutSource) (*lead.Lead, error) {
	out, err := c.RecordConsent(ctx, a, id, ConsentInput{Kind: lead.ConsentWithdraw, Source: lead.ConsentManual, RequestedBy: source.OrDefault()})
	if err != nil {
		return nil, err
	}
	return out.Lead, nil
}
