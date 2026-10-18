package smartcampaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

type createStub struct {
	created []string
	err     error
}

func (c *createStub) Execute(_ context.Context, input *wc.Campaign) (*wc.Campaign, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.created = append(c.created, input.Name)
	return &wc.Campaign{ID: "new-" + input.Name, Name: input.Name, Status: campaign.StatusStopped}, nil
}

type policySaver struct {
	saved map[string]sc.Policy
	err   error
}

func (p *policySaver) Save(target *wc.Campaign, policy sc.Policy) error {
	if p.err != nil {
		return p.err
	}
	if p.saved == nil {
		p.saved = map[string]sc.Policy{}
	}
	p.saved[target.ID] = policy
	return nil
}

type deleteStub struct{ deleted []string }

func (d *deleteStub) Execute(id string) error {
	d.deleted = append(d.deleted, id)
	return nil
}

func TestACampaignCreatedWithAPolicySendsInWavesFromTheStart(t *testing.T) {
	inner, saver, deleter := &createStub{}, &policySaver{}, &deleteStub{}
	policy := sc.PresetPolicy(sc.Standard, sendHours())

	created, err := CreateWithPolicy(inner, saver, deleter).Execute(ctx, &wc.Campaign{Name: "promo", WavePolicy: &policy})

	if err != nil || saved(saver, created.ID).Preset != sc.Standard || len(deleter.deleted) != 0 {
		t.Fatalf("created %+v, err %v, saved %v", created, err, saver.saved)
	}
}

func TestAnInvalidPolicyCreatesNothing(t *testing.T) {
	inner, saver, deleter := &createStub{}, &policySaver{}, &deleteStub{}
	policy := sc.PresetPolicy(sc.Standard, nil)

	_, err := CreateWithPolicy(inner, saver, deleter).Execute(ctx, &wc.Campaign{Name: "promo", WavePolicy: &policy})

	if !errors.Is(err, sc.ErrInvalidPolicy) || len(inner.created) != 0 {
		t.Fatalf("err %v, created %v, want nothing created", err, inner.created)
	}
}

func TestACampaignWhosePolicyCannotBeSavedIsRemoved(t *testing.T) {
	inner, saver, deleter := &createStub{}, &policySaver{err: errBroken}, &deleteStub{}
	policy := sc.PresetPolicy(sc.Standard, sendHours())

	_, err := CreateWithPolicy(inner, saver, deleter).Execute(ctx, &wc.Campaign{Name: "promo", WavePolicy: &policy})

	if !errors.Is(err, errBroken) || len(deleter.deleted) != 1 || deleter.deleted[0] != "new-promo" {
		t.Fatalf("err %v, deleted %v, want the campaign removed so it can never send everything at once", err, deleter.deleted)
	}
}

func TestACampaignWithoutPolicyIsCreatedAsToday(t *testing.T) {
	inner, saver, deleter := &createStub{}, &policySaver{}, &deleteStub{}

	created, err := CreateWithPolicy(inner, saver, deleter).Execute(ctx, &wc.Campaign{Name: "plain"})

	if err != nil || created.ID != "new-plain" || len(saver.saved) != 0 {
		t.Fatalf("created %+v, err %v, saved %v", created, err, saver.saved)
	}
}

func saved(s *policySaver, id string) sc.Policy {
	return s.saved[id]
}
