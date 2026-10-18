package smartcampaign_usecase

import (
	"context"
	"errors"
	"fmt"

	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

type PolicySaver interface {
	Save(target *wc.Campaign, policy sc.Policy) error
}

type CampaignDeleter interface {
	Execute(campaignID string) error
}

type createWithPolicy struct {
	inner    wc.CreateCampaignUseCase
	policies PolicySaver
	deleter  CampaignDeleter
}

func CreateWithPolicy(inner wc.CreateCampaignUseCase, policies PolicySaver, deleter CampaignDeleter) wc.CreateCampaignUseCase {
	return &createWithPolicy{inner: inner, policies: policies, deleter: deleter}
}

func (c *createWithPolicy) Execute(ctx context.Context, input *wc.Campaign) (*wc.Campaign, error) {
	policy := input.WavePolicy
	if policy == nil {
		return c.inner.Execute(ctx, input)
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	created, err := c.inner.Execute(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := c.policies.Save(created, *policy); err != nil {
		if deleteErr := c.deleter.Execute(created.ID); deleteErr != nil {
			return nil, errors.Join(fmt.Errorf("save the wave policy: %w", err), fmt.Errorf("remove the campaign created without it: %w", deleteErr))
		}
		return nil, fmt.Errorf("save the wave policy: %w", err)
	}
	return created, nil
}
