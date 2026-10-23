package carriercampaign_usecase

import (
	"context"
	"fmt"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/customfield"
	"vozko/domain/lead"
)

type bindingSet struct {
	variant  campaign.BindingPlan
	waBody   campaign.BindingPlan
	waHeader campaign.BindingPlan
	sms      campaign.BindingPlan
	hasWA    bool
	hasSMS   bool
}

func plansFor(c cc.Campaign, variant int, defs []*customfield.Definition) (bindingSet, error) {
	if variant < 0 || variant >= len(c.Variants) {
		return bindingSet{}, ErrVariantIndex
	}
	var b bindingSet
	var err error
	if b.variant, err = c.Variants[variant].PlanBindings(c.Channel, defs); err != nil {
		return bindingSet{}, err
	}
	if wa := c.Fallback.WhatsApp; wa != nil {
		if b.waBody, b.waHeader, err = wa.PlanBindings(len(wa.BodyBindings), len(wa.HeaderBindings), defs); err != nil {
			return bindingSet{}, err
		}
		b.hasWA = true
	}
	if fallback := c.Fallback.SMS; fallback != nil {
		if b.sms, err = fallback.PlanBindings(defs); err != nil {
			return bindingSet{}, err
		}
		b.hasSMS = true
	}
	return b, nil
}

func (b bindingSet) all() []campaign.BindingPlan {
	plans := []campaign.BindingPlan{b.variant}
	if b.hasWA {
		plans = append(plans, b.waBody, b.waHeader)
	}
	if b.hasSMS {
		plans = append(plans, b.sms)
	}
	return plans
}

func (b bindingSet) any(test func(campaign.BindingPlan) bool) bool {
	for _, p := range b.all() {
		if test(p) {
			return true
		}
	}
	return false
}

func (s *Service) subjectOf(ctx context.Context, workspaceID string, l *lead.Lead, b bindingSet) (campaign.BindingSubject, error) {
	if l == nil || !b.any(campaign.BindingPlan.NeedsLead) {
		return campaign.BindingSubject{Lead: l}, nil
	}
	if b.any(campaign.BindingPlan.NeedsAddresses) {
		if err := s.deps.Contacts.AttachContactDetails(ctx, workspaceID, []*lead.Lead{l}); err != nil {
			return campaign.BindingSubject{}, fmt.Errorf("addresses of lead %s: %w", l.ID, err)
		}
	}
	subject := campaign.BindingSubject{Lead: l}
	if l.Owner != "" && b.any(campaign.BindingPlan.NeedsOwnerNames) {
		subject.OwnerName = s.deps.Names.Names(l.Owner)[l.Owner]
	}
	return subject, nil
}

func (b bindingSet) resolve(subject campaign.BindingSubject) ([]string, cc.FallbackValues, error) {
	vars, err := b.variant.Resolve(subject)
	if err != nil {
		return nil, cc.FallbackValues{}, err
	}
	var values cc.FallbackValues
	if b.hasWA {
		if values.WhatsAppBody, err = b.waBody.Resolve(subject); err != nil {
			return nil, cc.FallbackValues{}, fmt.Errorf("whatsapp fallback: %w", err)
		}
		if values.WhatsAppHeader, err = b.waHeader.Resolve(subject); err != nil {
			return nil, cc.FallbackValues{}, fmt.Errorf("whatsapp fallback: %w", err)
		}
	}
	if b.hasSMS {
		if values.SMS, err = b.sms.Resolve(subject); err != nil {
			return nil, cc.FallbackValues{}, fmt.Errorf("sms fallback: %w", err)
		}
	}
	return vars, values, nil
}

func (s *Service) message(ctx context.Context, c cc.Campaign, variant int, l *lead.Lead) ([]string, cc.FallbackValues, error) {
	defs, err := s.fields(c.WorkspaceID)
	if err != nil {
		return nil, cc.FallbackValues{}, err
	}
	plans, err := plansFor(c, variant, defs)
	if err != nil {
		return nil, cc.FallbackValues{}, err
	}
	subject, err := s.subjectOf(ctx, c.WorkspaceID, l, plans)
	if err != nil {
		return nil, cc.FallbackValues{}, err
	}
	return plans.resolve(subject)
}
