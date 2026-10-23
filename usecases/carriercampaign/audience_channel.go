package carriercampaign_usecase

import (
	"context"

	audience "vozko/domain/campaignaudience"
	cc "vozko/domain/carriercampaign"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

type audienceChannel struct{ s *Service }

func (s *Service) AudienceChannel() campaignaudience_usecase.Channel {
	return audienceChannel{s: s}
}

func (s *Service) SetAudience(service *campaignaudience_usecase.Service) {
	s.audience = service
}

func (audienceChannel) Name() audience.Channel { return audience.ChannelCarrier }

func (c audienceChannel) Prepare(_ context.Context, a campaignaudience_usecase.Actor, params campaignaudience_usecase.Params) (campaignaudience_usecase.Prepared, error) {
	kind := cc.Kind(params.Kind)
	if !kind.Valid() {
		return campaignaudience_usecase.Prepared{}, audience.ErrKindRequired
	}
	return campaignaudience_usecase.Prepared{Open: func(_ context.Context, _ []audience.Bound, source audience.Source) (campaignaudience_usecase.Judge, error) {
		if source != audience.SourceLeads {
			return nil, audience.ErrSourceUnsupported
		}
		return &carrierJudge{s: c.s, workspaceID: a.WorkspaceID, kind: kind, seen: map[string]bool{}}, nil
	}}, nil
}

type carrierJudge struct {
	s           *Service
	workspaceID string
	kind        cc.Kind
	seen        map[string]bool
}

func (j *carrierJudge) Leads(ctx context.Context, ids []string) ([]audience.Candidate, error) {
	judged, err := j.s.judgeRecipients(ctx, j.workspaceID, j.kind, ids, j.seen)
	if err != nil {
		return nil, err
	}
	out := make([]audience.Candidate, 0, len(judged))
	for _, r := range judged {
		out = append(out, audience.Candidate{LeadID: r.leadID, Contact: r.phone, HasContact: r.phone != "", Refusal: string(r.reason)})
	}
	return out, nil
}

func (j *carrierJudge) Rows(context.Context, []campaignaudience_usecase.RowCandidate) ([]audience.Candidate, error) {
	return nil, audience.ErrSourceUnsupported
}

func (s *Service) countAudience(ctx context.Context, c cc.Campaign, a Actor) (*audience.Count, error) {
	if !c.HasAudience() {
		return nil, cc.ErrAudienceRequired
	}
	if s.audience == nil {
		return nil, audience.ErrUnavailable
	}
	sel := c.Audience.Inferred()
	spec := audience.Spec{Source: audience.SourceLeads, Selection: &sel}
	actor := campaignaudience_usecase.Actor{WorkspaceID: c.WorkspaceID, UserID: a.UserID, IsAdmin: a.IsAdmin}
	count, err := s.audience.Walk(ctx, actor, audience.ChannelCarrier, campaignaudience_usecase.Params{Kind: string(c.Kind)}, spec, "", nil)
	if err != nil {
		return nil, err
	}
	return &count, nil
}
