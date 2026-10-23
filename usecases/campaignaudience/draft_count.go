package campaignaudience_usecase

import (
	"context"

	"vozko/domain/campaignaudience"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
)

type OfficialCounter struct {
	audience *Service
}

func NewOfficialCounter(service *Service) *OfficialCounter {
	return &OfficialCounter{audience: service}
}

func listedCount(ch campaignaudience.Channel, n int) campaignaudience.Count {
	return campaignaudience.Count{Channel: ch, Matched: n, WithContact: n, Sendable: &n}
}

func (c *OfficialCounter) Count(ctx context.Context, camp *wc.Campaign) (campaignaudience.Count, error) {
	if camp == nil {
		return listedCount(campaignaudience.ChannelWhatsApp, 0), nil
	}
	if camp.Recipients == nil {
		return listedCount(campaignaudience.ChannelWhatsApp, len(camp.PhoneInputs)), nil
	}
	if len(camp.PhoneInputs) > 0 {
		return campaignaudience.Count{}, campaignaudience.ErrSourceAmbiguous
	}
	if c == nil || c.audience == nil {
		return campaignaudience.Count{}, campaignaudience.ErrUnavailable
	}
	actor, err := requester(camp.WorkspaceID, camp.Recipients)
	if err != nil {
		return campaignaudience.Count{}, err
	}
	return c.audience.Count(ctx, actor, campaignaudience.ChannelWhatsApp, Params{TemplateID: camp.TemplateID, BusinessPhoneID: camp.BusinessPhoneID, Automation: camp.Automation()}, camp.Recipients.Spec)
}

type UnofficialCounter struct {
	audience *Service
}

func NewUnofficialCounter(service *Service) *UnofficialCounter {
	return &UnofficialCounter{audience: service}
}

func (c *UnofficialCounter) Count(ctx context.Context, camp *uwc.Campaign) (campaignaudience.Count, error) {
	if camp == nil {
		return listedCount(campaignaudience.ChannelUnofficial, 0), nil
	}
	if camp.Recipients == nil {
		return listedCount(campaignaudience.ChannelUnofficial, len(camp.Targets)), nil
	}
	if camp.TargetsGroups() {
		return campaignaudience.Count{}, campaignaudience.ErrSourceUnsupported
	}
	if len(camp.Targets) > 0 {
		return campaignaudience.Count{}, campaignaudience.ErrSourceAmbiguous
	}
	if c == nil || c.audience == nil {
		return campaignaudience.Count{}, campaignaudience.ErrUnavailable
	}
	actor, err := requester(camp.WorkspaceID, camp.Recipients)
	if err != nil {
		return campaignaudience.Count{}, err
	}
	message := camp.Message
	return c.audience.Count(ctx, actor, campaignaudience.ChannelUnofficial, Params{Message: &message, InstanceID: camp.InstanceID, Automation: camp.Automation()}, camp.Recipients.Spec)
}
