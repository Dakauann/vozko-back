package template

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
)

func TestBuildSendInput_CarriesTheOrdinaryFields(t *testing.T) {
	tmpl := &Template{
		Name:     "boas_vindas",
		Language: "pt_BR",
		Category: TemplateCategoryMarketing,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "Ola {{1}}, tudo bem?"},
		},
	}

	out, err := tmpl.BuildSendInput(SendInputParams{
		To:                    "5511987654321",
		BodyParams:            []string{"Marina"},
		FromPhoneNumberID:     "phone_1",
		BizOpaqueCallbackData: "attempt_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.TemplateName != "boas_vindas" || out.Language != "pt_BR" {
		t.Errorf("template identity lost: %+v", out)
	}
	if len(out.Parameters) != 1 || out.Parameters[0] != "Marina" {
		t.Errorf("body params = %v", out.Parameters)
	}
	if out.FromPhoneNumberID != "phone_1" || out.BizOpaqueCallbackData != "attempt_1" {
		t.Errorf("send metadata lost: %+v", out)
	}
	if len(out.Buttons) != 0 {
		t.Errorf("a template with no button parameters should send none, got %v", out.Buttons)
	}
}

func TestBuildSendInput_AuthenticationMirrorsTheCodeOntoTheButton(t *testing.T) {
	tmpl := authTemplate(copyCodeButton())

	out, err := tmpl.BuildSendInput(SendInputParams{
		To:         "5511987654321",
		BodyParams: []string{"482913"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Parameters) != 1 || out.Parameters[0] != "482913" {
		t.Fatalf("body params = %v, want the code", out.Parameters)
	}
	if len(out.Buttons) != 1 {
		t.Fatalf("buttons = %v, want exactly one", out.Buttons)
	}

	btn := out.Buttons[0]
	if btn.SubType != conversation.TemplateButtonSubTypeURL {
		t.Errorf("subType = %q, want %q", btn.SubType, conversation.TemplateButtonSubTypeURL)
	}
	if btn.Index != 0 {
		t.Errorf("index = %d, want 0", btn.Index)
	}
	if btn.Text != "482913" {
		t.Errorf("button text = %q, want the same code as the body", btn.Text)
	}
	if btn.CouponCode != "" {
		t.Errorf("an authentication button takes a text parameter, not a coupon code: %q", btn.CouponCode)
	}
}

func TestBuildSendInput_AuthenticationUsesTheButtonsOwnIndex(t *testing.T) {
	tmpl := authTemplate(
		TemplateButton{Type: "QUICK_REPLY", Text: "Ajuda"},
		copyCodeButton(),
	)

	out, err := tmpl.BuildSendInput(SendInputParams{To: "55", BodyParams: []string{"123456"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Buttons) != 1 || out.Buttons[0].Index != 1 {
		t.Errorf("buttons = %+v, want one at index 1", out.Buttons)
	}
}

func TestBuildSendInput_AuthenticationWithoutACodeIsRefused(t *testing.T) {
	tmpl := authTemplate(copyCodeButton())

	for name, params := range map[string][]string{
		"none":  nil,
		"empty": {"   "},
	} {
		if _, err := tmpl.BuildSendInput(SendInputParams{To: "55", BodyParams: params}); !errors.Is(err, ErrAuthenticationCodeRequired) {
			t.Errorf("%s: got %v, want ErrAuthenticationCodeRequired", name, err)
		}
	}
}

func TestBuildSendInput_AuthenticationWithoutAnOTPButtonSendsNoButton(t *testing.T) {
	tmpl := authTemplate()
	tmpl.Components = tmpl.Components[:1]

	out, err := tmpl.BuildSendInput(SendInputParams{To: "55", BodyParams: []string{"123456"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Buttons) != 0 {
		t.Errorf("buttons = %v, want none", out.Buttons)
	}
}

func TestBuildSendInput_NamedParameterFormat(t *testing.T) {
	tmpl := &Template{
		Name:            "pedido",
		Language:        "pt_BR",
		Category:        TemplateCategoryUtility,
		ParameterFormat: ParameterFormatNamed,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "Ola {{nome}}, o pedido {{codigo}} saiu para entrega."},
		},
	}

	out, err := tmpl.BuildSendInput(SendInputParams{To: "55", BodyParams: []string{"Marina", "A-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsNamedParameterFormat {
		t.Error("named format lost")
	}
	if len(out.ParameterNames) != 2 || out.ParameterNames[0] != "nome" {
		t.Errorf("parameterNames = %v", out.ParameterNames)
	}
}

func TestBuildSendInput_MediaHeaderPrefersTheIDAndFallsBackToTheURL(t *testing.T) {
	id := "media_123"
	url := "https://cdn.exemplo.com/capa.jpg"

	withID := &Template{
		Name: "promo", Language: "pt_BR", Category: TemplateCategoryMarketing,
		HeaderMediaID: &id, HeaderMediaURL: &url,
		Components: []TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}},
	}
	out, err := withID.BuildSendInput(SendInputParams{To: "55"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.HeaderType != "image" || out.HeaderMediaID != id || out.HeaderMediaURL != "" {
		t.Errorf("with an id: %+v, want the id alone", out)
	}

	urlOnly := &Template{
		Name: "promo", Language: "pt_BR", Category: TemplateCategoryMarketing,
		HeaderMediaURL: &url,
		Components:     []TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}},
	}
	out, err = urlOnly.BuildSendInput(SendInputParams{To: "55"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.HeaderType != "image" || out.HeaderMediaURL != url || out.HeaderMediaID != "" {
		t.Errorf("without an id: %+v, want the url", out)
	}
}

func TestBuildSendInput_TextHeaderParams(t *testing.T) {
	tmpl := &Template{
		Name: "aviso", Language: "pt_BR", Category: TemplateCategoryUtility,
		Components: []TemplateComponent{
			{Type: "HEADER", Format: "TEXT", Text: "Pedido {{1}}"},
			{Type: "BODY", Text: "Ola, tudo certo."},
		},
	}

	out, err := tmpl.BuildSendInput(SendInputParams{To: "55", HeaderParams: []string{"A-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.HeaderTextParams) != 1 || out.HeaderTextParams[0] != "A-1" {
		t.Errorf("headerTextParams = %v", out.HeaderTextParams)
	}
	if out.HeaderType != "" {
		t.Errorf("a text header must not set a media headerType, got %q", out.HeaderType)
	}
}

func TestBuildSendInput_RequiresARecipient(t *testing.T) {
	tmpl := authTemplate(copyCodeButton())
	if _, err := tmpl.BuildSendInput(SendInputParams{BodyParams: []string{"1"}}); err == nil {
		t.Error("expected an error for an empty recipient")
	}
}
