package template

import (
	"errors"
	"testing"
)

func authTemplate(buttons ...TemplateButton) *Template {
	return &Template{
		Name:     "codigo_verificacao",
		Language: "pt_BR",
		Category: TemplateCategoryAuthentication,
		Status:   TemplateStatusApproved,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "{{1}} e o seu codigo de verificacao."},
			{Type: "BUTTONS", Buttons: buttons},
		},
	}
}

func copyCodeButton() TemplateButton {
	return TemplateButton{Type: ButtonTypeOTP, OTPType: string(OTPTypeCopyCode)}
}

func TestOTPType_IsValid(t *testing.T) {
	for _, valid := range []OTPType{OTPTypeCopyCode, OTPTypeOneTap, OTPTypeZeroTap} {
		if !valid.IsValid() {
			t.Errorf("%q should be valid", valid)
		}
	}
	for _, invalid := range []OTPType{"", "copy code", "COPY-CODE", "OTP"} {
		if invalid.IsValid() {
			t.Errorf("%q should not be valid", invalid)
		}
	}
}

func TestIsAuthentication(t *testing.T) {
	if !authTemplate(copyCodeButton()).IsAuthentication() {
		t.Error("an AUTHENTICATION template should report itself as one")
	}
	marketing := &Template{Category: TemplateCategoryMarketing}
	if marketing.IsAuthentication() {
		t.Error("a MARKETING template is not an authentication template")
	}
}

// The index is the button's position inside the BUTTONS component, which is
// what Meta's send payload keys the parameter on. Getting it from the wrong
// list (all components, say) puts the code on the wrong button.
func TestOTPButton_IndexIsPositionWithinButtons(t *testing.T) {
	tmpl := authTemplate(
		TemplateButton{Type: "QUICK_REPLY", Text: "Ajuda"},
		copyCodeButton(),
	)

	btn, index, ok := tmpl.OTPButton()
	if !ok {
		t.Fatal("expected to find an OTP button")
	}
	if index != 1 {
		t.Errorf("index = %d, want 1 (second button in the BUTTONS component)", index)
	}
	if btn.OTPType != string(OTPTypeCopyCode) {
		t.Errorf("otpType = %q, want COPY_CODE", btn.OTPType)
	}
}

func TestOTPButton_AbsentWithoutOne(t *testing.T) {
	tmpl := authTemplate(TemplateButton{Type: "QUICK_REPLY", Text: "Ajuda"})
	if _, _, ok := tmpl.OTPButton(); ok {
		t.Error("a template with no OTP button should report none")
	}
}

// An authentication template takes exactly one parameter, the code, whatever
// its body text happens to parse to. Meta owns that body and has been known to
// return it empty, which would otherwise leave every caller sending no
// parameters at all.
func TestParameterCount_AuthenticationIsAlwaysOne(t *testing.T) {
	withText := authTemplate(copyCodeButton())
	if got := withText.ParameterCount(); got != 1 {
		t.Errorf("with body text: got %d, want 1", got)
	}

	emptyBody := authTemplate(copyCodeButton())
	emptyBody.Components[0].Text = ""
	if got := emptyBody.ParameterCount(); got != 1 {
		t.Errorf("with an empty body (Meta returns these): got %d, want 1", got)
	}
}

func TestParameterCount_NonAuthenticationUnchanged(t *testing.T) {
	tmpl := &Template{
		Category:   TemplateCategoryUtility,
		Components: []TemplateComponent{{Type: "BODY", Text: "Ola {{1}}, seu pedido {{2}} saiu."}},
	}
	if got := tmpl.ParameterCount(); got != 2 {
		t.Errorf("got %d, want 2", got)
	}

	noParams := &Template{
		Category:   TemplateCategoryUtility,
		Components: []TemplateComponent{{Type: "BODY", Text: "Recebemos o seu pedido."}},
	}
	if got := noParams.ParameterCount(); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestValidateButtons_OTPRequiresAKnownOTPType(t *testing.T) {
	err := validateButtons([]TemplateButton{{Type: ButtonTypeOTP}})
	if !errors.Is(err, ErrOTPTypeRequired) {
		t.Errorf("got %v, want ErrOTPTypeRequired", err)
	}

	err = validateButtons([]TemplateButton{{Type: ButtonTypeOTP, OTPType: "TWO_TAP"}})
	if !errors.Is(err, ErrInvalidOTPType) {
		t.Errorf("got %v, want ErrInvalidOTPType", err)
	}
}

// Meta supplies the label ("Copiar codigo", localized) when none is given, so a
// missing text must not be rejected the way it is for a quick reply.
func TestValidateButtons_OTPNeedsNoText(t *testing.T) {
	if err := validateButtons([]TemplateButton{copyCodeButton()}); err != nil {
		t.Errorf("an OTP button without text should be valid, got %v", err)
	}
}

func TestValidateButtons_OTPAcceptsACustomLabel(t *testing.T) {
	btn := copyCodeButton()
	btn.Text = "Copiar codigo"
	if err := validateButtons([]TemplateButton{btn}); err != nil {
		t.Errorf("got %v, want nil", err)
	}
}

func TestValidateButtons_OnlyOneOTPButton(t *testing.T) {
	err := validateButtons([]TemplateButton{copyCodeButton(), copyCodeButton()})
	if !errors.Is(err, ErrMultipleOTPButtons) {
		t.Errorf("got %v, want ErrMultipleOTPButtons", err)
	}
}

func TestValidateAuthenticationTemplate_RejectsAnOTPButtonElsewhere(t *testing.T) {
	components := []TemplateComponent{
		{Type: "BODY", Text: "Seu cupom chegou."},
		{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
	}
	err := ValidateAuthenticationTemplate(TemplateCategoryMarketing, components)
	if !errors.Is(err, ErrOTPButtonNotAuthentication) {
		t.Errorf("got %v, want ErrOTPButtonNotAuthentication", err)
	}
}

func TestValidateAuthenticationTemplate_RequiresAnOTPButton(t *testing.T) {
	components := []TemplateComponent{
		{Type: "BODY"},
		{Type: "BUTTONS", Buttons: []TemplateButton{{Type: "QUICK_REPLY", Text: "Ajuda"}}},
	}
	err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components)
	if !errors.Is(err, ErrAuthenticationNeedsOTPButton) {
		t.Errorf("got %v, want ErrAuthenticationNeedsOTPButton", err)
	}
}

func TestValidateAuthenticationTemplate_AcceptsTheMetaShape(t *testing.T) {
	recommend := true
	expiry := 10
	components := []TemplateComponent{
		{Type: "BODY", AddSecurityRecommendation: &recommend},
		{Type: "FOOTER", CodeExpirationMinutes: &expiry},
		{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
	}
	if err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components); err != nil {
		t.Errorf("got %v, want nil", err)
	}
}

func TestValidateAuthenticationTemplate_CodeExpiryBounds(t *testing.T) {
	for _, minutes := range []int{0, 91, -1} {
		expiry := minutes
		components := []TemplateComponent{
			{Type: "BODY"},
			{Type: "FOOTER", CodeExpirationMinutes: &expiry},
			{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
		}
		err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components)
		if !errors.Is(err, ErrCodeExpirationOutOfRange) {
			t.Errorf("%d minutes: got %v, want ErrCodeExpirationOutOfRange", minutes, err)
		}
	}

	for _, minutes := range []int{1, 10, 90} {
		expiry := minutes
		components := []TemplateComponent{
			{Type: "BODY"},
			{Type: "FOOTER", CodeExpirationMinutes: &expiry},
			{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
		}
		if err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components); err != nil {
			t.Errorf("%d minutes: got %v, want nil", minutes, err)
		}
	}
}

// A footer carrying an expiry is generated by Meta, so it legitimately has no
// text. The ordinary footer rules would otherwise be fine with that too, but
// this pins it: the two ways of writing a footer must not collide.
func TestValidateComponents_AuthenticationFooterNeedsNoText(t *testing.T) {
	expiry := 5
	components := []TemplateComponent{
		{Type: "BODY"},
		{Type: "FOOTER", CodeExpirationMinutes: &expiry},
		{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
	}
	if err := ValidateComponents(components); err != nil {
		t.Errorf("got %v, want nil", err)
	}
}

// Meta accepts three otp_types; this product can only BUILD one of them.
//
// ONE_TAP and ZERO_TAP autofill into an Android app, which Meta only wires up
// when the template carries that app's package name and signature hash. Nothing
// in this product collects either, so a template created with them is one Meta
// rejects. Refused here, with a sentence that says why, rather than passed
// through to come back as a provider error nobody can act on.
func TestValidateButtons_RefusesOTPTypesThisProductCannotBuild(t *testing.T) {
	for _, otpType := range []OTPType{OTPTypeOneTap, OTPTypeZeroTap} {
		err := validateButtons([]TemplateButton{{Type: ButtonTypeOTP, OTPType: string(otpType)}})
		if !errors.Is(err, ErrOTPTypeUnsupported) {
			t.Errorf("%s: got %v, want ErrOTPTypeUnsupported", otpType, err)
		}
	}
}

func TestValidateButtons_CopyCodeIsTheSupportedOne(t *testing.T) {
	if err := validateButtons([]TemplateButton{copyCodeButton()}); err != nil {
		t.Errorf("COPY_CODE must be accepted, got %v", err)
	}
}

// An unknown otp_type is still the OTHER error: "you typed something that is not
// an otp_type" and "that otp_type needs an Android app" are different problems.
func TestValidateButtons_UnknownOTPTypeStaysInvalidRatherThanUnsupported(t *testing.T) {
	err := validateButtons([]TemplateButton{{Type: ButtonTypeOTP, OTPType: "TWO_TAP"}})
	if !errors.Is(err, ErrInvalidOTPType) {
		t.Errorf("got %v, want ErrInvalidOTPType", err)
	}
}

// ONE_TAP stays a valid vocabulary word: a template created in Business Manager
// with one syncs in and must remain sendable. Only CREATING one is refused.
func TestOTPType_OneTapIsStillAValidMetaValue(t *testing.T) {
	if !OTPTypeOneTap.IsValid() {
		t.Error("ONE_TAP is a real Meta otp_type and must stay valid")
	}
	if OTPTypeOneTap.Supported() {
		t.Error("ONE_TAP is not something this product can build")
	}
	if !OTPTypeCopyCode.Supported() {
		t.Error("COPY_CODE is what this product builds")
	}
}

// Meta: "Authentication templates don't use a HEADER component." No text header,
// no media header, none. The builder must not be able to produce one, and the
// API is reachable without the builder.
func TestValidateAuthenticationTemplate_RefusesAHeader(t *testing.T) {
	for _, header := range []TemplateComponent{
		{Type: "HEADER", Format: "TEXT", Text: "Seu codigo"},
		{Type: "HEADER", Format: "IMAGE"},
	} {
		components := []TemplateComponent{
			header,
			{Type: "BODY"},
			{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
		}
		err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components)
		if !errors.Is(err, ErrAuthenticationNoHeader) {
			t.Errorf("%s header: got %v, want ErrAuthenticationNoHeader", header.Format, err)
		}
	}
}

// Meta writes the body and translates it. Text the business supplied is text
// Meta will refuse, so it is refused here where the message can say why.
func TestValidateAuthenticationTemplate_RefusesBusinessWrittenBody(t *testing.T) {
	components := []TemplateComponent{
		{Type: "BODY", Text: "Seu codigo e {{1}}, nao compartilhe."},
		{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
	}
	err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components)
	if !errors.Is(err, ErrAuthenticationBodyNotEditable) {
		t.Errorf("got %v, want ErrAuthenticationBodyNotEditable", err)
	}
}

// The footer is Meta's expiry line, not a place to write.
func TestValidateAuthenticationTemplate_RefusesBusinessWrittenFooter(t *testing.T) {
	expiry := 10
	components := []TemplateComponent{
		{Type: "BODY"},
		{Type: "FOOTER", Text: "Ate logo", CodeExpirationMinutes: &expiry},
		{Type: "BUTTONS", Buttons: []TemplateButton{copyCodeButton()}},
	}
	err := ValidateAuthenticationTemplate(TemplateCategoryAuthentication, components)
	if !errors.Is(err, ErrAuthenticationFooterNotEditable) {
		t.Errorf("got %v, want ErrAuthenticationFooterNotEditable", err)
	}
}

// A header on a marketing template is ordinary and must stay allowed.
func TestValidateAuthenticationTemplate_HeaderStillFineElsewhere(t *testing.T) {
	components := []TemplateComponent{
		{Type: "HEADER", Format: "IMAGE"},
		{Type: "BODY", Text: "Sua promocao chegou."},
	}
	if err := ValidateAuthenticationTemplate(TemplateCategoryMarketing, components); err != nil {
		t.Errorf("got %v, want nil", err)
	}
}

// Meta caps the one-time code at 15 characters. Longer is a send Meta refuses.
func TestAuthenticationCode_RejectsACodeOverFifteenCharacters(t *testing.T) {
	tmpl := authTemplate(copyCodeButton())

	if _, err := tmpl.AuthenticationCode([]string{"1234567890123456"}); !errors.Is(err, ErrAuthenticationCodeTooLong) {
		t.Errorf("16 chars: got %v, want ErrAuthenticationCodeTooLong", err)
	}
	if code, err := tmpl.AuthenticationCode([]string{"123456789012345"}); err != nil || code != "123456789012345" {
		t.Errorf("15 chars: got %q, %v, want the code and no error", code, err)
	}
	if code, err := tmpl.AuthenticationCode([]string{"482913"}); err != nil || code != "482913" {
		t.Errorf("ordinary code: got %q, %v", code, err)
	}
}
