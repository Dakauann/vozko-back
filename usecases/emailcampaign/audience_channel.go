package emailcampaign_usecase

import (
	"context"
	"strings"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/lead"
	"vozko/domain/selection"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

type audienceChannel struct{ s *Service }

func (s *Service) AudienceChannel() campaignaudience_usecase.Channel {
	return audienceChannel{s: s}
}

func (s *Service) SetAudience(service *campaignaudience_usecase.Service) {
	s.audience = service
}

func (audienceChannel) Name() audience.Channel { return audience.ChannelEmail }

func (c audienceChannel) Prepare(_ context.Context, a campaignaudience_usecase.Actor, params campaignaudience_usecase.Params) (campaignaudience_usecase.Prepared, error) {
	doc := emaildesign.Document{}
	if params.Document != nil {
		doc = *params.Document
	}
	tags := emaildesign.MergeTags(params.Subject, doc)
	slots := make([]audience.Slot, 0, len(tags))
	for _, tag := range tags {
		slots = append(slots, audience.Slot{Key: tag.Name, Fallback: tag.Fallback})
	}
	return campaignaudience_usecase.Prepared{Slots: slots, Open: func(_ context.Context, bound []audience.Bound, _ audience.Source) (campaignaudience_usecase.Judge, error) {
		return &emailJudge{s: c.s, workspaceID: a.WorkspaceID, bound: bound, seen: map[emailaddress.Address]bool{}}, nil
	}}, nil
}

type emailJudge struct {
	s           *Service
	workspaceID string
	bound       []audience.Bound
	seen        map[emailaddress.Address]bool
}

type judged struct {
	candidate audience.Candidate
	address   emailaddress.Address
	consent   emailconsent.LeadFacts
	screen    bool
}

func (j *emailJudge) Leads(ctx context.Context, ids []string) ([]audience.Candidate, error) {
	facts, err := j.s.deps.LeadFacts.EmailFacts(ctx, j.workspaceID, ids)
	if err != nil {
		return nil, err
	}
	var batch []judged
	for _, id := range ids {
		f, ok := facts[id]
		if !ok {
			continue
		}
		batch = append(batch, j.judgeOne(id, f.Email, f))
	}
	return j.screen(ctx, batch)
}

func (j *emailJudge) Rows(ctx context.Context, rows []campaignaudience_usecase.RowCandidate) ([]audience.Candidate, error) {
	var known []string
	for _, r := range rows {
		if r.LeadID != "" {
			known = append(known, r.LeadID)
		}
	}
	facts := map[string]lead.EmailFacts{}
	if len(known) > 0 {
		found, err := j.s.deps.LeadFacts.EmailFacts(ctx, j.workspaceID, known)
		if err != nil {
			return nil, err
		}
		facts = found
	}
	batch := make([]judged, 0, len(rows))
	for _, r := range rows {
		f, ok := facts[r.LeadID]
		if !ok {
			f = lead.EmailFacts{ID: r.LeadID}
		}
		if strings.TrimSpace(r.Row.Name) != "" {
			f.Name = r.Row.Name
		}
		batch = append(batch, j.judgeOne(r.LeadID, r.Row.Contact, f))
	}
	return j.screen(ctx, batch)
}

func (j *emailJudge) judgeOne(leadID, raw string, f lead.EmailFacts) judged {
	raw = strings.TrimSpace(raw)
	c := audience.Candidate{LeadID: leadID, Name: f.Name}
	if raw == "" {
		c.Refusal = string(emailcampaign.SkipNoEmail)
		return judged{candidate: c}
	}
	c.HasContact = true
	address, err := emailaddress.Normalize(raw)
	if err != nil {
		c.Contact, c.Refusal = clip(raw), string(emailcampaign.SkipInvalidAddress)
		return judged{candidate: c}
	}
	c.Contact = address.String()
	c.Values = j.values(personalization(f, address))
	if j.seen[address] {
		c.Refusal = string(emailcampaign.SkipDuplicate)
		return judged{candidate: c}
	}
	j.seen[address] = true
	return judged{candidate: c, address: address, consent: emailconsent.LeadFacts{OptedOut: f.OptedOut, Blocked: f.Blocked}, screen: true}
}

func (j *emailJudge) values(p emaildesign.Personalization) []string {
	values := make([]string, 0, len(j.bound))
	for _, b := range j.bound {
		values = append(values, p.Value(b.Key))
	}
	return values
}

func (j *emailJudge) screen(ctx context.Context, batch []judged) ([]audience.Candidate, error) {
	out := make([]audience.Candidate, len(batch))
	var pending []int
	for i, item := range batch {
		out[i] = item.candidate
		if item.screen {
			pending = append(pending, i)
		}
	}
	for start := 0; start < len(pending); start += selection.Chunk {
		chunk := pending[start:min(start+selection.Chunk, len(pending))]
		addresses := make([]emailaddress.Address, len(chunk))
		for k, i := range chunk {
			addresses[k] = batch[i].address
		}
		subs, err := j.s.deps.Subscriptions.Find(ctx, j.workspaceID, addresses)
		if err != nil {
			return nil, err
		}
		standing, err := j.s.deps.Hygiene.Standing(ctx, j.workspaceID, addresses)
		if err != nil {
			return nil, err
		}
		if err := j.s.deps.Hygiene.RequestStale(ctx, standing); err != nil {
			return nil, err
		}
		now := j.s.now()
		for _, i := range chunk {
			address := batch[i].address
			skip := consentSkip(subs, address, batch[i].consent)
			if skip == emailcampaign.SkipNone {
				skip = emailcampaign.HygieneSkip(j.s.deps.Hygiene.Facts(address), standing[address], now)
			}
			out[i].Refusal = string(skip)
			out[i].FirstContact = !standing[address].Activity.EverDelivered()
		}
	}
	return out, nil
}
