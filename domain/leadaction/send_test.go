package leadaction

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/workspace"
)

func templateSend() *SendParams {
	return &SendParams{Name: " Matrículas ", BusinessPhoneID: " bp-1 ", TemplateID: " tpl-1 ", Bindings: []campaign.VariableBinding{{Source: campaign.BindFirstName}}}
}

func unofficialSend() *SendParams {
	return &SendParams{Name: "Matrículas", InstanceID: "inst-1", Message: &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Olá {{1}}"}}}
}

func TestSendActions(t *testing.T) {
	if !ActionSendTemplate.Sends() || !ActionSendUnofficial.Sends() || ActionExport.Sends() {
		t.Fatal("only the two send actions send")
	}
	if ActionSendTemplate.Channel() != campaign.ChannelOfficial || ActionSendUnofficial.Channel() != campaign.ChannelUnofficial || ActionBlock.Channel() != "" {
		t.Fatal("channels are wrong")
	}
	if got, ok := ActionOfChannel(campaign.ChannelUnofficial); !ok || got != ActionSendUnofficial {
		t.Fatalf("ActionOfChannel = %q %v", got, ok)
	}
	if _, ok := ActionOfChannel("sms"); ok {
		t.Fatal("an unknown channel got an action")
	}
	if got := ActionSendTemplate.Requirements(Params{}, false); !reflect.DeepEqual(got, []workspace.CapabilityKey{CapabilitySendTemplate}) {
		t.Fatalf("send_template requires %v", got)
	}
	if got := ActionSendUnofficial.Requirements(Params{}, false); !reflect.DeepEqual(got, []workspace.CapabilityKey{CapabilitySendUnofficial}) {
		t.Fatalf("send_unofficial requires %v", got)
	}
}

func TestSendParamsValidate(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		params Params
		want   error
	}{
		{"a template send", ActionSendTemplate, Params{Send: templateSend()}, nil},
		{"an unofficial send", ActionSendUnofficial, Params{Send: unofficialSend()}, nil},
		{"a send without its parameters", ActionSendTemplate, Params{}, ErrSendParamsRequired},
		{"a send without a name", ActionSendTemplate, Params{Send: &SendParams{BusinessPhoneID: "bp", TemplateID: "t"}}, ErrNameRequired},
		{"a template send without the phone", ActionSendTemplate, Params{Send: &SendParams{Name: "n", TemplateID: "t"}}, ErrSendPhoneRequired},
		{"a template send without the template", ActionSendTemplate, Params{Send: &SendParams{Name: "n", BusinessPhoneID: "bp"}}, ErrSendTemplateRequired},
		{"a template send with a message", ActionSendTemplate, Params{Send: &SendParams{Name: "n", BusinessPhoneID: "bp", TemplateID: "t", InstanceID: "i"}}, ErrParamsAmbiguous},
		{"an unofficial send without the number", ActionSendUnofficial, Params{Send: &SendParams{Name: "n", Message: &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oi"}}}}, ErrSendInstanceRequired},
		{"an unofficial send without the message", ActionSendUnofficial, Params{Send: &SendParams{Name: "n", InstanceID: "i"}}, ErrSendMessageRequired},
		{"an unofficial send with a template", ActionSendUnofficial, Params{Send: &SendParams{Name: "n", InstanceID: "i", TemplateID: "t", Message: &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oi"}}}}, ErrParamsAmbiguous},
		{"a send with another action's field", ActionSendTemplate, Params{Send: templateSend(), Key: "k"}, ErrParamsAmbiguous},
		{"a send with the block phone", ActionSendTemplate, Params{Send: templateSend(), BusinessPhoneID: "bp"}, ErrParamsAmbiguous},
		{"a classify carrying a send", ActionClassify, Params{Key: "k", Value: json.RawMessage(`1`), Send: templateSend()}, ErrParamsAmbiguous},
		{"an export carrying a send", ActionExport, Params{Send: templateSend()}, ErrParamsAmbiguous},
		{"pacing on a template send", ActionSendTemplate, Params{Send: &SendParams{Name: "n", BusinessPhoneID: "bp", TemplateID: "t", DailyCap: 10}}, ErrParamsAmbiguous},
		{"a negative daily cap", ActionSendUnofficial, Params{Send: &SendParams{Name: "n", InstanceID: "i", DailyCap: -1, Message: &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oi"}}}}, ErrSendPacingInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.params.Validate(tc.action); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSendParamsAreNormalized(t *testing.T) {
	p := Params{Send: templateSend()}.Normalized()
	if p.Send.Name != "Matrículas" || p.Send.BusinessPhoneID != "bp-1" || p.Send.TemplateID != "tpl-1" {
		t.Fatalf("normalized = %+v", p.Send)
	}
}

func TestSendErrorCodes(t *testing.T) {
	for err, code := range map[error]string{
		ErrSendParamsRequired:   "lead_action_send_params_required",
		ErrSendPhoneRequired:    "lead_action_send_phone_required",
		ErrSendTemplateRequired: "lead_action_send_template_required",
		ErrSendInstanceRequired: "lead_action_send_instance_required",
		ErrSendMessageRequired:  "lead_action_send_message_required",
		ErrSendPacingInvalid:    "lead_action_send_pacing_invalid",
	} {
		if got := ErrorCode(err); got != code {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, code)
		}
	}
}
