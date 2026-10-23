package campaignaudience_usecase

import (
	"context"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
)

type officialCreate struct {
	inner    wc.CreateCampaignUseCase
	audience *Service
}

func OfficialCreate(inner wc.CreateCampaignUseCase, service *Service) wc.CreateCampaignUseCase {
	return &officialCreate{inner: inner, audience: service}
}

func requester(workspaceID string, r *campaignaudience.Request) (Actor, error) {
	if strings.TrimSpace(r.UserID) == "" || strings.TrimSpace(workspaceID) == "" {
		return Actor{}, campaign.ErrCreationScopeMissing
	}
	return Actor{WorkspaceID: strings.TrimSpace(workspaceID), UserID: strings.TrimSpace(r.UserID), IsAdmin: r.IsAdmin}, nil
}

func (c *officialCreate) Execute(ctx context.Context, input *wc.Campaign) (*wc.Campaign, error) {
	if input == nil || input.Recipients == nil {
		return c.inner.Execute(ctx, input)
	}
	if len(input.PhoneInputs) > 0 {
		return nil, campaignaudience.ErrSourceAmbiguous
	}
	actor, err := requester(input.WorkspaceID, input.Recipients)
	if err != nil {
		return nil, err
	}
	if c.audience == nil {
		return nil, campaignaudience.ErrUnavailable
	}
	recipients, err := c.audience.Recipients(ctx, actor, campaignaudience.ChannelWhatsApp, Params{TemplateID: input.TemplateID, BusinessPhoneID: input.BusinessPhoneID, Automation: input.Automation()}, input.Recipients.Spec)
	if err != nil {
		return nil, err
	}
	resolved := *input
	resolved.Recipients = nil
	resolved.PhoneInputs = make([]wc.PhoneInput, 0, len(recipients))
	for _, r := range recipients {
		resolved.PhoneInputs = append(resolved.PhoneInputs, wc.PhoneInput{
			Number: r.Number, LeadID: r.LeadID, Name: r.Name, Variables: r.Variables, Metadata: r.Metadata, Skip: r.Skip, Missing: r.Missing,
		})
	}
	return c.inner.Execute(ctx, &resolved)
}

type unofficialCreate struct {
	inner    uwc.CreateCampaignUseCase
	audience *Service
}

func UnofficialCreate(inner uwc.CreateCampaignUseCase, service *Service) uwc.CreateCampaignUseCase {
	return &unofficialCreate{inner: inner, audience: service}
}

func (c *unofficialCreate) Execute(ctx context.Context, in *uwc.Campaign, scope uw.DepartmentScope) (*uwc.Campaign, error) {
	if in == nil || in.Recipients == nil {
		return c.inner.Execute(ctx, in, scope)
	}
	if in.TargetsGroups() {
		return nil, campaignaudience.ErrSourceUnsupported
	}
	if len(in.Targets) > 0 {
		return nil, campaignaudience.ErrSourceAmbiguous
	}
	actor, err := requester(in.WorkspaceID, in.Recipients)
	if err != nil {
		return nil, err
	}
	if c.audience == nil {
		return nil, campaignaudience.ErrUnavailable
	}
	message := in.Message
	recipients, err := c.audience.Recipients(ctx, actor, campaignaudience.ChannelUnofficial, Params{Message: &message, InstanceID: in.InstanceID, Automation: in.Automation()}, in.Recipients.Spec)
	if err != nil {
		return nil, err
	}
	resolved := *in
	resolved.Recipients = nil
	resolved.Targets = make([]uwc.TargetInput, 0, len(recipients))
	for _, r := range recipients {
		resolved.Targets = append(resolved.Targets, uwc.TargetInput{
			Number: r.Number, LeadID: r.LeadID, Name: r.Name, Variables: r.Variables, Metadata: r.Metadata, Skip: r.Skip, Missing: r.Missing,
		})
	}
	return c.inner.Execute(ctx, &resolved, scope)
}
