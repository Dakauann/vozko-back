package leadaction

import (
	"errors"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/workspace"
)

const (
	ActionSendTemplate   Action = "send_template"
	ActionSendUnofficial Action = "send_unofficial"
)

const (
	CapabilitySendTemplate   workspace.CapabilityKey = "leads.send_template"
	CapabilitySendUnofficial workspace.CapabilityKey = "leads.send_unofficial"
)

var (
	ErrSendParamsRequired   = errors.New("lead action: a send needs its send parameters")
	ErrSendPhoneRequired    = errors.New("lead action: a template send needs the business phone")
	ErrSendTemplateRequired = errors.New("lead action: a template send needs the template")
	ErrSendInstanceRequired = errors.New("lead action: an unofficial send needs the connected number")
	ErrSendMessageRequired  = errors.New("lead action: an unofficial send needs the message")
	ErrSendPacingInvalid    = errors.New("lead action: the pacing of an unofficial send cannot be negative")
)

type SendParams struct {
	Name            string                                    `json:"name"`
	DepartmentID    string                                    `json:"departmentId,omitempty"`
	Split           bool                                      `json:"split,omitempty"`
	BusinessPhoneID string                                    `json:"businessPhoneId,omitempty"`
	TemplateID      string                                    `json:"templateId,omitempty"`
	InstanceID      string                                    `json:"instanceId,omitempty"`
	Message         *unofficial_whatsapp_campaign.MessageSpec `json:"message,omitempty"`
	SendDelayMinMS  int                                       `json:"sendDelayMinMs,omitempty"`
	SendDelayMaxMS  int                                       `json:"sendDelayMaxMs,omitempty"`
	DailyCap        int                                       `json:"dailyCap,omitempty"`
	Bindings        []campaign.VariableBinding                `json:"bindings,omitempty"`
}

func (a Action) Sends() bool {
	return a == ActionSendTemplate || a == ActionSendUnofficial
}

func (a Action) Channel() campaign.Channel {
	switch a {
	case ActionSendTemplate:
		return campaign.ChannelOfficial
	case ActionSendUnofficial:
		return campaign.ChannelUnofficial
	}
	return ""
}

func ActionOfChannel(c campaign.Channel) (Action, bool) {
	switch c {
	case campaign.ChannelOfficial:
		return ActionSendTemplate, true
	case campaign.ChannelUnofficial:
		return ActionSendUnofficial, true
	}
	return "", false
}

func (s SendParams) normalized() SendParams {
	s.Name = strings.TrimSpace(s.Name)
	s.DepartmentID = strings.TrimSpace(s.DepartmentID)
	s.BusinessPhoneID = strings.TrimSpace(s.BusinessPhoneID)
	s.TemplateID = strings.TrimSpace(s.TemplateID)
	s.InstanceID = strings.TrimSpace(s.InstanceID)
	if s.Message != nil {
		message := *s.Message
		message.Normalize()
		s.Message = &message
	}
	return s
}

func (s SendParams) validate(a Action) error {
	if s.Name == "" {
		return ErrNameRequired
	}
	official := s.BusinessPhoneID != "" || s.TemplateID != ""
	unofficial := s.InstanceID != "" || s.Message != nil || s.SendDelayMinMS != 0 || s.SendDelayMaxMS != 0 || s.DailyCap != 0
	switch a {
	case ActionSendTemplate:
		if unofficial {
			return ErrParamsAmbiguous
		}
		if s.BusinessPhoneID == "" {
			return ErrSendPhoneRequired
		}
		if s.TemplateID == "" {
			return ErrSendTemplateRequired
		}
	case ActionSendUnofficial:
		if official {
			return ErrParamsAmbiguous
		}
		if s.InstanceID == "" {
			return ErrSendInstanceRequired
		}
		if s.Message == nil {
			return ErrSendMessageRequired
		}
		if s.SendDelayMinMS < 0 || s.SendDelayMaxMS < 0 || s.DailyCap < 0 {
			return ErrSendPacingInvalid
		}
	}
	return nil
}

func (p Params) validateSend(a Action) error {
	if p.Key != "" || len(p.Value) > 0 || p.OwnerID != nil || p.Blocked != nil || p.BusinessPhoneID != "" || p.exportFields() || p.audienceFields() || p.CallList != nil {
		return ErrParamsAmbiguous
	}
	if p.Send == nil {
		return ErrSendParamsRequired
	}
	return p.Send.validate(a)
}
