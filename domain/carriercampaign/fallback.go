package carriercampaign

import (
	"errors"
	"fmt"

	"vozko/domain/campaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

var (
	ErrFallbackNotAllowed         = errors.New("carriercampaign: only rcs campaigns fall back")
	ErrWhatsAppFallbackIncomplete = errors.New("carriercampaign: the WhatsApp fallback needs a number and a template")
	ErrWhatsAppActorRequired      = errors.New("carriercampaign: the WhatsApp fallback acts as the person who configured it")
	ErrSMSRouteRequired           = errors.New("carriercampaign: an sms send needs its route")
	ErrSMSRouteMismatch           = errors.New("carriercampaign: the sms route is not the one the campaign uses")
)

type WhatsAppFallback struct {
	BusinessPhoneID string                     `json:"business_phone_id"`
	TemplateID      string                     `json:"template_id"`
	BodyBindings    []campaign.VariableBinding `json:"body_bindings,omitempty"`
	HeaderBindings  []campaign.VariableBinding `json:"header_bindings,omitempty"`
}

func (w WhatsAppFallback) Validate(fields []*customfield.Definition) error {
	if blank(w.BusinessPhoneID) || blank(w.TemplateID) {
		return ErrWhatsAppFallbackIncomplete
	}
	_, _, err := w.PlanBindings(len(w.BodyBindings), len(w.HeaderBindings), fields)
	return err
}

func (w WhatsAppFallback) PlanBindings(bodySlots, headerSlots int, fields []*customfield.Definition) (body, header campaign.BindingPlan, err error) {
	body, err = campaign.PlanBindings(w.BodyBindings, bodySlots, fields)
	if err != nil {
		return campaign.BindingPlan{}, campaign.BindingPlan{}, fmt.Errorf("whatsapp body: %w", err)
	}
	header, err = campaign.PlanBindings(w.HeaderBindings, headerSlots, fields)
	if err != nil {
		return campaign.BindingPlan{}, campaign.BindingPlan{}, fmt.Errorf("whatsapp header: %w", err)
	}
	return body, header, nil
}

type SMSFallback struct {
	RouteID  string                     `json:"route_id"`
	Text     string                     `json:"text"`
	Bindings []campaign.VariableBinding `json:"bindings,omitempty"`
}

func (s SMSFallback) Validate(fields []*customfield.Definition) error {
	if blank(s.RouteID) {
		return ErrSMSRouteRequired
	}
	if err := sms.Validate(s.Text); err != nil {
		return err
	}
	_, err := s.PlanBindings(fields)
	return err
}

func (s SMSFallback) slots() int {
	return shared.HighestPositionalParameter([]string{s.Text})
}

func (s SMSFallback) PlanBindings(fields []*customfield.Definition) (campaign.BindingPlan, error) {
	return campaign.PlanBindings(s.Bindings, s.slots(), fields)
}

type Fallback struct {
	WhatsApp *WhatsAppFallback `json:"whatsapp,omitempty"`
	SMS      *SMSFallback      `json:"sms,omitempty"`
}

func (f Fallback) Any() bool {
	return f.WhatsApp != nil || f.SMS != nil
}

func (f Fallback) Plan() carriermessaging.FallbackPlan {
	return carriermessaging.FallbackPlan{WhatsApp: f.WhatsApp != nil, SMS: f.SMS != nil}
}

func (f Fallback) Validate(fields []*customfield.Definition) error {
	if f.WhatsApp != nil {
		if err := f.WhatsApp.Validate(fields); err != nil {
			return err
		}
	}
	if f.SMS != nil {
		return f.SMS.Validate(fields)
	}
	return nil
}

type FallbackValues struct {
	WhatsAppBody   []string
	WhatsAppHeader []string
	SMS            []string
}

func (f Fallback) Spec(actorID string, values FallbackValues) (carriermessaging.FallbackSpec, error) {
	var spec carriermessaging.FallbackSpec
	if f.WhatsApp != nil {
		if blank(actorID) {
			return carriermessaging.FallbackSpec{}, ErrWhatsAppActorRequired
		}
		if err := valueCount("whatsapp body", len(f.WhatsApp.BodyBindings), values.WhatsAppBody); err != nil {
			return carriermessaging.FallbackSpec{}, err
		}
		if err := valueCount("whatsapp header", len(f.WhatsApp.HeaderBindings), values.WhatsAppHeader); err != nil {
			return carriermessaging.FallbackSpec{}, err
		}
		spec.WhatsApp = &carriermessaging.WhatsAppFallback{
			ActorID:         actorID,
			BusinessPhoneID: f.WhatsApp.BusinessPhoneID,
			TemplateID:      f.WhatsApp.TemplateID,
			BodyParams:      values.WhatsAppBody,
			HeaderParams:    values.WhatsAppHeader,
		}
	}
	if f.SMS != nil {
		if err := valueCount("sms", f.SMS.slots(), values.SMS); err != nil {
			return carriermessaging.FallbackSpec{}, err
		}
		spec.SMSRouteID = f.SMS.RouteID
		spec.SMSText = shared.RenderPositional(f.SMS.Text, values.SMS)
	}
	return spec, nil
}

func valueCount(part string, want int, values []string) error {
	if len(values) != want {
		return fmt.Errorf("%s: %w: %d variables, %d values", part, campaign.ErrBindingsMismatch, want, len(values))
	}
	return nil
}
