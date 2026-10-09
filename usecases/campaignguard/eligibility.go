package campaignguard

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/campaign"
)

type LeadFactsReader interface {
	LeadFacts(ctx context.Context, workspaceID string, leadIDs []string) (map[string]campaign.LeadFacts, error)
}

type Screener interface {
	Check(ctx context.Context, workspaceID, leadID, senderID string) (campaign.SkipReason, error)
	Screen(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (Screening, error)
}

type EntryGate interface {
	Check(ctx context.Context, workspaceID, leadID, senderID string) (campaign.SkipReason, error)
	Claim(ctx context.Context, workspaceID, leadID, senderID string) (func(), error)
}

type Eligibility struct {
	leads LeadFactsReader
	spam  *SpamGuard
}

func NewEligibility(leads LeadFactsReader, spam *SpamGuard) (*Eligibility, error) {
	if leads == nil || spam == nil {
		return nil, ErrUnavailable
	}
	return &Eligibility{leads: leads, spam: spam}, nil
}

func (e *Eligibility) Facts(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (map[string]campaign.EligibilityFacts, error) {
	facts, _, err := e.factsWithin(ctx, workspaceID, leadIDs, senderID)
	return facts, err
}

func (e *Eligibility) factsWithin(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (map[string]campaign.EligibilityFacts, int, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, 0, ErrWorkspaceRequired
	}
	days, err := e.spam.days(ctx, workspaceID)
	if err != nil {
		return nil, 0, err
	}
	ids := distinct(leadIDs)
	out := make(map[string]campaign.EligibilityFacts, len(ids))
	for _, chunk := range chunks(ids, FactsChunk) {
		leads, err := e.leads.LeadFacts(ctx, workspaceID, chunk)
		if err != nil {
			return nil, 0, fmt.Errorf("campaign guard: could not read the leads of this send: %w", err)
		}
		cooling, err := e.spam.inCooldownWithin(days, chunk, senderID)
		if err != nil {
			return nil, 0, err
		}
		for _, id := range chunk {
			out[id] = campaign.EligibilityFacts{Lead: leads[id], InCooldown: cooling[id]}
		}
	}
	return out, days, nil
}

type Screening struct {
	Skipped      map[string]campaign.SkipReason
	CooldownDays int
}

func (s Screening) Detail(missing []campaign.MissingVariable) campaign.SkipDetail {
	return campaign.SkipDetail{Missing: missing, CooldownDays: s.CooldownDays}
}

func (e *Eligibility) Screen(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (Screening, error) {
	facts, days, err := e.factsWithin(ctx, workspaceID, leadIDs, senderID)
	if err != nil {
		return Screening{}, err
	}
	skipped := map[string]campaign.SkipReason{}
	for _, id := range leadIDs {
		if ok, reason := campaign.Eligibility(facts[strings.TrimSpace(id)]); !ok {
			skipped[id] = reason
		}
	}
	return Screening{Skipped: skipped, CooldownDays: days}, nil
}

func (e *Eligibility) Check(ctx context.Context, workspaceID, leadID, senderID string) (campaign.SkipReason, error) {
	screening, err := e.Screen(ctx, workspaceID, []string{leadID}, senderID)
	if err != nil {
		return "", err
	}
	return screening.Skipped[leadID], nil
}

func (e *Eligibility) Claim(ctx context.Context, workspaceID, leadID, senderID string) (func(), error) {
	return e.spam.Claim(ctx, workspaceID, leadID, senderID)
}
