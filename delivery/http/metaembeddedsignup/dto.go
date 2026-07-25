package metaembeddedsignup

import (
	"encoding/json"
	"strings"

	"vozko/domain/whatsapp/template"
)

type EmbeddedSignupCallbackRequest struct {
	Code          string      `json:"code,omitempty" example:"AQDx1a2b3c4d5e6f"`
	AccessToken   string      `json:"access_token,omitempty" example:"EAAGm0PX4ZCpsBA..."`
	PhoneNumberID string      `json:"phone_number_id,omitempty" example:"109354212345678"`
	WABAID        string      `json:"waba_id,omitempty" example:"102290129340398"`
	BusinessID    string      `json:"business_id,omitempty" example:"178451236789012"`
	UserID        string      `json:"user_id,omitempty" example:"7250123456789012"`
	ExpiresIn     interface{} `json:"expires_in,omitempty"`
	SignedRequest string      `json:"signed_request,omitempty" example:"a1b2c3.eyJ1c2VyX2lkIjoiMTIzNCJ9"`
	FullResponse  interface{} `json:"full_response,omitempty"`
	EmbeddedData  interface{} `json:"embedded_data,omitempty"`
}

type dialog360TemplateStatusEvent struct {
	Event string `json:"event"`
	Type  string `json:"type"`
	Data  struct {
		ID       string `json:"id"`
		Template struct {
			ID             string `json:"id"`
			ExternalID     string `json:"external_id"`
			Name           string `json:"name"`
			NewStatus      string `json:"new_status"`
			Language       string `json:"language"`
			RejectedReason string `json:"rejected_reason"`
		} `json:"template"`
	} `json:"data"`
}

func dialog360TemplateEventToPayload(e dialog360TemplateStatusEvent) *template.TemplateWebhookPayload {
	name := e.Event
	if name == "" {
		name = e.Type
	}
	if name != "waba_template_status_changed" {
		return nil
	}
	channelID := strings.TrimSpace(e.Data.Template.ID)
	if channelID == "" || strings.TrimSpace(e.Data.Template.NewStatus) == "" {
		return nil
	}
	return &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: strings.TrimSpace(e.Data.ID),
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateStatusUpdate,
				Value: template.TemplateWebhookValue{
					Event:                   e.Data.Template.NewStatus,
					ChannelExternalID:       channelID,
					MessageTemplateName:     e.Data.Template.Name,
					MessageTemplateLanguage: e.Data.Template.Language,
					Reason:                  e.Data.Template.RejectedReason,
				},
			}},
		}},
	}
}

func parseDialog360TemplateStatusPayloads(body []byte) []*template.TemplateWebhookPayload {
	var out []*template.TemplateWebhookPayload
	var arr []dialog360TemplateStatusEvent
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		for _, e := range arr {
			if p := dialog360TemplateEventToPayload(e); p != nil {
				out = append(out, p)
			}
		}
		return out
	}
	var single dialog360TemplateStatusEvent
	if err := json.Unmarshal(body, &single); err == nil {
		if p := dialog360TemplateEventToPayload(single); p != nil {
			out = append(out, p)
		}
	}
	return out
}

func parseDialog360PartnerEventNames(body []byte) []string {
	type evt struct {
		Event string `json:"event"`
		Type  string `json:"type"`
	}
	name := func(e evt) string {
		if e.Event != "" {
			return e.Event
		}
		return e.Type
	}
	var arr []evt
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			if n := name(e); n != "" {
				out = append(out, n)
			}
		}
		return out
	}
	var single evt
	if err := json.Unmarshal(body, &single); err == nil {
		if n := name(single); n != "" {
			return []string{n}
		}
	}
	return nil
}

func parseDialog360LiveChannels(body []byte) []string {
	type evt struct {
		Type      string `json:"type"`
		Event     string `json:"event"`
		ChannelID string `json:"channel_id"`
		Status    string `json:"status"`
		Data      struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			AccountMode string `json:"account_mode"`
		} `json:"data"`
		Channel struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"channel"`
	}
	firstNonEmpty := func(vals ...string) string {
		for _, v := range vals {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
		return ""
	}
	collect := func(e evt) string {
		name := firstNonEmpty(e.Event, e.Type)
		status := firstNonEmpty(e.Data.Status, e.Data.AccountMode, e.Status, e.Channel.Status)
		isLive := name == "channel_live" || name == "channel_running" || status == "live" || status == "running"
		if !isLive {
			return ""
		}
		return firstNonEmpty(e.Data.ID, e.ChannelID, e.Channel.ID)
	}
	var arr []evt
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		ids := make([]string, 0, len(arr))
		for _, e := range arr {
			if id := collect(e); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	var single evt
	if err := json.Unmarshal(body, &single); err == nil {
		if id := collect(single); id != "" {
			return []string{id}
		}
	}
	return nil
}
