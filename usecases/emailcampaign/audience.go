package emailcampaign_usecase

import (
	"context"
	"unicode/utf8"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/selection"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

const (
	resolvePage    = 5000
	maxStoredBytes = 254
)

type plannedRecipient struct {
	leadID  string
	address emailaddress.Address
	skip    emailcampaign.SkipReason
}

type audiencePlan struct {
	summary    emailcampaign.AudienceSummary
	recipients []plannedRecipient
}

func (s *Service) planAudience(ctx context.Context, scope selection.Scope, c emailcampaign.Campaign) (audiencePlan, error) {
	plan := audiencePlan{summary: emailcampaign.AudienceSummary{Skipped: map[emailcampaign.SkipReason]int{}}}
	spec, ok := c.AudienceSpec()
	if !ok {
		return plan, nil
	}
	if s.audience == nil {
		return audiencePlan{}, audience.ErrUnavailable
	}
	owner := ""
	if spec.Source == audience.SourceList {
		owner = c.AudienceOwner()
	}
	doc := c.RenderDocument()
	actor := campaignaudience_usecase.Actor{WorkspaceID: c.WorkspaceID, UserID: scope.ActorID, IsAdmin: scope.IsAdmin}
	params := campaignaudience_usecase.Params{Subject: c.MergeSubjects(), Document: &doc}
	neverMailed := 0
	count, err := s.audience.Walk(ctx, actor, audience.ChannelEmail, params, spec, owner, func(cand audience.Candidate, d audience.Decision) error {
		r := plannedRecipient{leadID: cand.LeadID, address: emailaddress.Address(cand.Contact)}
		if d.Outcome != audience.OutcomeSend {
			r.skip = emailcampaign.SkipReason(d.Reason)
		} else if cand.FirstContact {
			neverMailed++
		}
		plan.recipients = append(plan.recipients, r)
		return nil
	})
	if err != nil {
		return audiencePlan{}, err
	}
	plan.summary = emailcampaign.SummaryOf(count)
	plan.summary.NeverMailed = neverMailed
	return plan, nil
}

func clip(raw string) string {
	if len(raw) <= maxStoredBytes {
		return raw
	}
	cut := raw[:maxStoredBytes]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func consentSkip(subs map[emailaddress.Address]emailconsent.Subscription, address emailaddress.Address, facts emailconsent.LeadFacts) emailcampaign.SkipReason {
	var sub *emailconsent.Subscription
	if found, ok := subs[address]; ok {
		sub = &found
	}
	return emailcampaign.SkipFromConsent(emailconsent.MarketingEligibility(sub, facts))
}

func (p audiencePlan) materialize(c emailcampaign.Campaign) []emailcampaign.Recipient {
	out := make([]emailcampaign.Recipient, 0, len(p.recipients))
	for _, r := range p.recipients {
		rc := emailcampaign.Recipient{WorkspaceID: c.WorkspaceID, CampaignID: c.ID, LeadID: r.leadID, Address: r.address}
		switch {
		case r.skip != emailcampaign.SkipNone:
			rc.Status, rc.SkipReason = emailcampaign.RecipientSkipped, r.skip
		default:
			rc.Status, rc.Variant = emailcampaign.RecipientPending, emailcampaign.VariantA
			if c.ABTest != nil {
				rc.Variant = c.ABTest.Assign(r.address)
			}
		}
		out = append(out, rc)
	}
	return out
}
