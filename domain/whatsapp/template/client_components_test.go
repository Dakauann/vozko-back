package template

import (
	"reflect"
	"testing"

	"vozko/domain/conversation"
)

func TestClientComponents_RoundTripsEveryField(t *testing.T) {
	recommend := true
	expiry := 10

	original := []TemplateComponent{
		{
			Type:   "HEADER",
			Format: "TEXT",
			Text:   "Pedido {{1}}",
			Example: &TemplateExample{
				HeaderText:      []string{"A-1"},
				HeaderHandle:    []string{"handle-1"},
				BodyText:        [][]string{{"Marina"}},
				BodyTextNamed:   []NamedParamExample{{ParamName: "nome", Example: "Marina"}},
				HeaderTextNamed: []NamedParamExample{{ParamName: "codigo", Example: "A-1"}},
			},
		},
		{
			Type:                      "BODY",
			Text:                      "{{1}} e o seu codigo.",
			AddSecurityRecommendation: &recommend,
		},
		{
			Type:                  "FOOTER",
			CodeExpirationMinutes: &expiry,
		},
		{
			Type: "BUTTONS",
			Buttons: []TemplateButton{
				{Type: "OTP", OTPType: "COPY_CODE", Text: "Copiar codigo"},
				{Type: "URL", Text: "Abrir", URL: "https://exemplo.com/{{1}}", Example: "https://exemplo.com/1"},
				{Type: "PHONE_NUMBER", Text: "Ligar", PhoneNumber: "5511987654321"},
			},
		},
	}

	back := FromClientComponents(ToClientComponents(original))

	if !reflect.DeepEqual(original, back) {
		t.Errorf("round trip lost data:\n before: %+v\n  after: %+v", original, back)
	}
}

func TestClientComponents_PointerFieldsKeepTheirAbsence(t *testing.T) {
	off := false
	components := []TemplateComponent{
		{Type: "BODY", AddSecurityRecommendation: &off},
		{Type: "FOOTER"},
	}

	out := ToClientComponents(components)
	if out[0].AddSecurityRecommendation == nil || *out[0].AddSecurityRecommendation {
		t.Error("an explicit false must stay an explicit false")
	}
	if out[1].CodeExpirationMinutes != nil {
		t.Error("an unset expiry must stay unset")
	}
}

func TestClientComponents_EmptyInputIsEmptyOutput(t *testing.T) {
	if got := ToClientComponents(nil); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
	if got := FromClientComponents([]conversation.TemplateComponent{}); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
