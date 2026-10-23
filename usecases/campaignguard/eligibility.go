package campaignguard

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/campaign"
	lcs "vozko/domain/lead_campaign_send"
	"vozko/domain/selection"
)

type ScreeningFactsReader interface {
	ScreeningFacts(ctx context.Context, q campaign.ScreeningQuery) (map[string]campaign.ScreeningFacts, error)
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
	facts ScreeningFactsReader
	spam  *SpamGuard
}

func NewEligibility(facts ScreeningFactsReader, spam *SpamGuard) (*Eligibility, error) {
	if facts == nil || spam == nil {
		return nil, ErrUnavailable
	}
	return &Eligibility{facts: facts, spam: spam}, nil
}

func (e *Eligibility) Facts(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (map[string]campaign.EligibilityFacts, error) {
	facts, _, err := e.factsWithin(ctx, campaign.ScreeningQuery{WorkspaceID: workspaceID, LeadIDs: leadIDs, SenderID: senderID})
	return facts, err
}

func (e *Eligibility) factsWithin(ctx context.Context, q campaign.ScreeningQuery) (map[string]campaign.EligibilityFacts, int, error) {
	if strings.TrimSpace(q.WorkspaceID) == "" {
		return nil, 0, ErrWorkspaceRequired
	}
	days, err := e.spam.days(ctx, q.WorkspaceID)
	if err != nil {
		return nil, 0, err
	}
	if days <= 0 {
		q.SenderID = ""
	}
	q.SenderID = strings.TrimSpace(q.SenderID)
	ids := distinct(q.LeadIDs)
	out := make(map[string]campaign.EligibilityFacts, len(ids))
	now := e.spam.now()
	for _, chunk := range chunks(ids, selection.Chunk) {
		q.LeadIDs = chunk
		facts, err := e.facts.ScreeningFacts(ctx, q)
		if err != nil {
			return nil, 0, fmt.Errorf("campaign guard: could not read the leads of this send: %w", err)
		}
		for _, id := range chunk {
			f := facts[id]
			out[id] = campaign.EligibilityFacts{Lead: f.Lead, InCooldown: lcs.WithinSpamWindow(f.LastSentAt, days, now), InRunningCampaign: f.InRunningCampaign}
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
	return e.screen(ctx, campaign.ScreeningQuery{WorkspaceID: workspaceID, LeadIDs: leadIDs, SenderID: senderID})
}

func (e *Eligibility) ScreenWithRunning(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (Screening, error) {
	return e.screen(ctx, campaign.ScreeningQuery{WorkspaceID: workspaceID, LeadIDs: leadIDs, SenderID: senderID, Running: true})
}

func (e *Eligibility) screen(ctx context.Context, q campaign.ScreeningQuery) (Screening, error) {
	leadIDs := q.LeadIDs
	facts, days, err := e.factsWithin(ctx, q)
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
