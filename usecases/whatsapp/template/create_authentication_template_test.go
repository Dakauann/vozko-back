package template_usecase

import (
	"errors"
	"testing"

	"vozko/domain/whatsapp/template"
)

func otpButtonsComponent() template.TemplateComponent {
	return template.TemplateComponent{
		Type: "BUTTONS",
		Buttons: []template.TemplateButton{
			{Type: template.ButtonTypeOTP, OTPType: string(template.OTPTypeCopyCode)},
		},
	}
}

func authCreateInput(comps ...template.TemplateComponent) template.CreateTemplateInput {
	in := baseCreateInput(comps...)
	in.Name = "codigo_verificacao"
	in.Category = template.TemplateCategoryAuthentication
	return in
}

// The shape Meta documents: a body carrying a flag instead of text, a footer
// carrying a number instead of text, and one OTP button.
func TestCreateTemplate_AuthenticationReachesTheProvider(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	recommend := true
	expiry := 10
	_, err := uc.Execute(authCreateInput(
		template.TemplateComponent{Type: "BODY", AddSecurityRecommendation: &recommend},
		template.TemplateComponent{Type: "FOOTER", CodeExpirationMinutes: &expiry},
		otpButtonsComponent(),
	))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if client.createInput == nil {
		t.Fatal("the provider was never called")
	}

	var sawBody, sawFooter, sawButton bool
	for _, c := range client.createInput.Components {
		switch c.Type {
		case "BODY":
			sawBody = c.AddSecurityRecommendation != nil && *c.AddSecurityRecommendation
		case "FOOTER":
			sawFooter = c.CodeExpirationMinutes != nil && *c.CodeExpirationMinutes == 10
		case "BUTTONS":
			sawButton = len(c.Buttons) == 1 &&
				c.Buttons[0].Type == template.ButtonTypeOTP &&
				c.Buttons[0].OTPType == string(template.OTPTypeCopyCode)
		}
	}
	if !sawBody {
		t.Error("the security recommendation flag did not reach the provider")
	}
	if !sawFooter {
		t.Error("the code expiry did not reach the provider")
	}
	if !sawButton {
		t.Error("the OTP button did not reach the provider")
	}
}

// The template is persisted with the button intact, or the next send has
// nothing to find.
func TestCreateTemplate_AuthenticationPersistsTheOTPType(t *testing.T) {
	client := &createMockWAClient{}
	repo := &sendMockTemplateRepo{}
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	uc := NewCreateTemplateUseCase(factory, repo, &fakeSetHeaderMediaUC{})

	if _, err := uc.Execute(authCreateInput(
		template.TemplateComponent{Type: "BODY"},
		otpButtonsComponent(),
	)); err != nil {
		t.Fatalf("create: %v", err)
	}

	if repo.created == nil {
		t.Fatal("nothing was persisted")
	}
	btn, index, ok := repo.created.OTPButton()
	if !ok {
		t.Fatal("the persisted template has no OTP button")
	}
	if index != 0 || btn.OTPType != string(template.OTPTypeCopyCode) {
		t.Errorf("persisted button = %+v at %d, want COPY_CODE at 0", btn, index)
	}
}

// Both halves of the category rule, refused before the provider is called.
func TestCreateTemplate_RefusesAnOTPButtonOnAMarketingTemplate(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	in := baseCreateInput(
		template.TemplateComponent{Type: "BODY", Text: "Seu cupom chegou."},
		otpButtonsComponent(),
	)
	in.Category = template.TemplateCategoryMarketing

	_, err := uc.Execute(in)
	if !errors.Is(err, template.ErrOTPButtonNotAuthentication) {
		t.Fatalf("want ErrOTPButtonNotAuthentication, got %v", err)
	}
	if client.createInput != nil {
		t.Error("a template Meta would reject must not reach the provider")
	}
}

func TestCreateTemplate_RefusesAnAuthenticationTemplateWithNoOTPButton(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	_, err := uc.Execute(authCreateInput(
		template.TemplateComponent{Type: "BODY"},
	))
	if !errors.Is(err, template.ErrAuthenticationNeedsOTPButton) {
		t.Fatalf("want ErrAuthenticationNeedsOTPButton, got %v", err)
	}
	if client.createInput != nil {
		t.Error("the provider must not be called")
	}
}

func TestCreateTemplate_RefusesAnOutOfRangeExpiry(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	expiry := 120
	_, err := uc.Execute(authCreateInput(
		template.TemplateComponent{Type: "BODY"},
		template.TemplateComponent{Type: "FOOTER", CodeExpirationMinutes: &expiry},
		otpButtonsComponent(),
	))
	if !errors.Is(err, template.ErrCodeExpirationOutOfRange) {
		t.Fatalf("want ErrCodeExpirationOutOfRange, got %v", err)
	}
	if client.createInput != nil {
		t.Error("the provider must not be called")
	}
}

// An ordinary template is unaffected by the new category rule.
func TestCreateTemplate_UtilityWithNoOTPButtonStillCreates(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	if _, err := uc.Execute(baseCreateInput(
		template.TemplateComponent{Type: "BODY", Text: "Seu pedido saiu para entrega."},
	)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if client.createInput == nil {
		t.Fatal("the provider should have been called")
	}
}

// parameter_format describes placeholders the business wrote. An authentication
// template has none: Meta writes that body. Sending a meaningless "POSITIONAL"
// on a category whose docs never mention the field is exactly the kind of extra
// field that has already cost this package one provider rejection.
func TestCreateTemplate_AuthenticationOmitsParameterFormat(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	if _, err := uc.Execute(authCreateInput(
		template.TemplateComponent{Type: "BODY"},
		otpButtonsComponent(),
	)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if client.createInput == nil {
		t.Fatal("the provider was never called")
	}
	if client.createInput.ParameterFormat != "" {
		t.Errorf("parameterFormat = %q, want empty for an authentication template",
			client.createInput.ParameterFormat)
	}
}

// The other categories keep it: Meta rejects named placeholders sent without it.
func TestCreateTemplate_UtilityStillSendsParameterFormat(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	if _, err := uc.Execute(baseCreateInput(namedBodyComponent())); err != nil {
		t.Fatalf("create: %v", err)
	}
	if client.createInput.ParameterFormat == "" {
		t.Error("parameterFormat must still be sent for a utility template")
	}
}

// One-tap and zero-tap need an Android app's package name and signature hash,
// which nothing here collects. Refused with our own sentence rather than sent
// for Meta to reject.
func TestCreateTemplate_RefusesOTPTypesThatNeedAnApp(t *testing.T) {
	for _, otpType := range []template.OTPType{template.OTPTypeOneTap, template.OTPTypeZeroTap} {
		client := &createMockWAClient{}
		uc := newCreateUC(client)

		_, err := uc.Execute(authCreateInput(
			template.TemplateComponent{Type: "BODY"},
			template.TemplateComponent{Type: "BUTTONS", Buttons: []template.TemplateButton{
				{Type: template.ButtonTypeOTP, OTPType: string(otpType)},
			}},
		))
		if !errors.Is(err, template.ErrOTPTypeUnsupported) {
			t.Errorf("%s: got %v, want ErrOTPTypeUnsupported", otpType, err)
		}
		if client.createInput != nil {
			t.Errorf("%s: the provider must not be called", otpType)
		}
	}
}
