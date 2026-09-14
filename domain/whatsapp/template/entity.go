package template

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	ErrTemplateNotFound            = errors.New("whatsapp template not found")
	ErrTemplateNameRequired        = errors.New("template name is required")
	ErrExternalIDRequired          = errors.New("external template ID is required")
	ErrTemplateAlreadyExists       = errors.New("template with this external ID already exists")
	ErrTemplateCategoryUnavailable = errors.New("template category is unavailable for billing")

	ErrHeaderMediaURLNotApplicable = errors.New("header media URL can only be set for templates with IMAGE, VIDEO, or DOCUMENT headers")

	ErrTemplateNameInvalidChars    = errors.New("template name must contain only lowercase letters, numbers, and underscores")
	ErrTemplateNameMustStartLetter = errors.New("template name must start with a letter")
	ErrTemplateNameTooLong         = errors.New("template name cannot exceed 512 characters")

	ErrHeaderTextTooLong          = errors.New("header text cannot exceed 60 characters")
	ErrHeaderTextTooManyVariables = errors.New("header text can have at most 1 variable")
	ErrHeaderFormatRequired       = errors.New("header component requires a format (TEXT, IMAGE, VIDEO, DOCUMENT, LOCATION, GIF)")
	ErrHeaderMediaNeedsHandle     = errors.New("media headers (IMAGE, VIDEO, DOCUMENT, GIF) require header_handle in example")

	ErrBodyTextTooLong          = errors.New("body text cannot exceed 1024 characters")
	ErrBodyVariableAtStart      = errors.New("body text cannot start with a variable - add text before it")
	ErrBodyVariableAtEnd        = errors.New("body text cannot end with a variable - add text or punctuation after it")
	ErrBodyConsecutiveVariables = errors.New("body text cannot have consecutive variables - add text between them")
	ErrBodyNeedsExample         = errors.New("body with variables requires example.body_text or example.body_text_named_params")

	ErrFooterTextTooLong  = errors.New("footer text cannot exceed 60 characters")
	ErrFooterHasVariables = errors.New("footer does not support variables")

	ErrTooManyButtons          = errors.New("template cannot have more than 10 buttons")
	ErrButtonTextTooLong       = errors.New("button text cannot exceed 25 characters")
	ErrButtonTextRequired      = errors.New("button text is required")
	ErrButtonURLRequired       = errors.New("URL button requires a url")
	ErrButtonPhoneRequired     = errors.New("PHONE_NUMBER button requires a phone_number")
	ErrButtonsNotGrouped       = errors.New("quick reply buttons must be grouped together, not interspersed with other button types")
	ErrURLButtonVariableNotEnd = errors.New("URL button variable must be at the end of the URL")
	ErrURLButtonTooManyVars    = errors.New("URL button can have at most 1 variable")
	ErrCopyCodeNeedsExample    = errors.New("COPY_CODE button requires an example")

	ErrOTPTypeRequired    = errors.New("an OTP button needs an otp_type - COPY_CODE, ONE_TAP or ZERO_TAP")
	ErrInvalidOTPType     = errors.New("invalid otp_type - must be COPY_CODE, ONE_TAP or ZERO_TAP")
	ErrMultipleOTPButtons = errors.New("a template can carry at most one OTP button")
	// ErrOTPTypeUnsupported is a real otp_type this product cannot BUILD. One-tap
	// and zero-tap need the recipient app's package name and signature hash, which
	// nothing here collects, so Meta would reject the template.
	ErrOTPTypeUnsupported = errors.New("only COPY_CODE one-time password buttons can be created here - ONE_TAP and ZERO_TAP need an Android app registered with Meta")
	// ErrOTPButtonNotAuthentication is a code button on a template Meta would
	// price as marketing. Refused rather than sent: the coupon COPY_CODE button
	// is the thing that belongs there, and it is a different button with a
	// different parameter.
	ErrOTPButtonNotAuthentication   = errors.New("only an AUTHENTICATION template can carry an OTP button")
	ErrAuthenticationNeedsOTPButton = errors.New("an AUTHENTICATION template needs an OTP button")
	ErrCodeExpirationOutOfRange     = errors.New("code_expiration_minutes must be between 1 and 90")
	// The three below are Meta's shape rules for the category. An authentication
	// template is not a template the business writes: WhatsApp writes the body and
	// the footer, translates both, and allows no header at all.
	ErrAuthenticationNoHeader          = errors.New("an AUTHENTICATION template cannot have a HEADER component - WhatsApp does not render one")
	ErrAuthenticationBodyNotEditable   = errors.New("an AUTHENTICATION template body is written by WhatsApp and cannot carry your own text")
	ErrAuthenticationFooterNotEditable = errors.New("an AUTHENTICATION template footer is WhatsApp's expiry line and cannot carry your own text")
	// ErrAuthenticationCodeTooLong is a send Meta refuses: the one-time code is
	// capped at 15 characters.
	ErrAuthenticationCodeTooLong = errors.New("the one-time code cannot exceed 15 characters")
	// ErrAuthenticationCodeRequired is a SEND with no code in it. Meta answers
	// that with error 132000, a parameter count mismatch, which names neither
	// the template nor the missing code.
	ErrAuthenticationCodeRequired = errors.New("an authentication template send needs the one-time code as its first parameter")

	ErrCallPermissionWithButtons      = errors.New("a CALL_PERMISSION_REQUEST template cannot also include a BUTTONS component - WhatsApp renders the permission buttons automatically")
	ErrMultipleCallPermissionRequests = errors.New("template can have at most one CALL_PERMISSION_REQUEST component")

	ErrMixedParameterStyles = errors.New("template variables must use a single style - either numbered ({{1}}) or named ({{name}}), not both")

	ErrInvalidComponentType = errors.New("invalid component type - must be HEADER, BODY, FOOTER, BUTTONS, or CALL_PERMISSION_REQUEST")
	ErrInvalidHeaderFormat  = errors.New("invalid header format - must be TEXT, IMAGE, VIDEO, DOCUMENT, LOCATION, or GIF")
	ErrInvalidButtonType    = errors.New("invalid button type - must be QUICK_REPLY, URL, PHONE_NUMBER, or COPY_CODE")
	ErrInvalidCategory      = errors.New("invalid category - must be MARKETING, UTILITY, or AUTHENTICATION")
)

type TemplateStatus string

const (
	TemplateStatusPending  TemplateStatus = "PENDING"
	TemplateStatusApproved TemplateStatus = "APPROVED"
	TemplateStatusRejected TemplateStatus = "REJECTED"
	TemplateStatusPaused   TemplateStatus = "PAUSED"
	TemplateStatusDisabled TemplateStatus = "DISABLED"
)

func (s TemplateStatus) IsValid() bool {
	switch s {
	case TemplateStatusPending, TemplateStatusApproved, TemplateStatusRejected, TemplateStatusPaused, TemplateStatusDisabled:
		return true
	default:
		return false
	}
}

type TemplateCategory string

const (
	TemplateCategoryMarketing      TemplateCategory = "MARKETING"
	TemplateCategoryUtility        TemplateCategory = "UTILITY"
	TemplateCategoryAuthentication TemplateCategory = "AUTHENTICATION"
)

func (c TemplateCategory) IsValid() bool {
	switch c {
	case TemplateCategoryMarketing, TemplateCategoryUtility, TemplateCategoryAuthentication:
		return true
	default:
		return false
	}
}

// OTPType is what an authentication template's code button does when the
// recipient taps it.
//
// Authentication is the one category Meta writes the copy for: the business
// supplies no body text at all, only a flag for the security line, a number of
// minutes for the expiry line, and one of these buttons. That inversion is why
// the category has rules of its own further down this file.
type OTPType string

const (
	// OTPTypeCopyCode copies the code to the clipboard. The only kind that needs
	// no app-side integration, so it is the one a CRM can offer to anybody.
	OTPTypeCopyCode OTPType = "COPY_CODE"
	// OTPTypeOneTap autofills the code into a registered Android app.
	OTPTypeOneTap OTPType = "ONE_TAP"
	// OTPTypeZeroTap delivers the code to a registered app with no tap at all.
	OTPTypeZeroTap OTPType = "ZERO_TAP"
)

// IsValid reports whether this is one of Meta's otp_type values at all.
//
// Deliberately broader than Supported: a template created in Business Manager
// with a one-tap button syncs into this product and must stay readable and
// sendable. Only BUILDING one here is refused.
func (o OTPType) IsValid() bool {
	switch o {
	case OTPTypeCopyCode, OTPTypeOneTap, OTPTypeZeroTap:
		return true
	default:
		return false
	}
}

// Supported reports whether this product can CREATE a button of this kind.
//
// Only copy-code. One-tap and zero-tap autofill the code into an Android app,
// and Meta only wires that up when the template carries that app's package name
// and signature hash — neither of which anything here collects, so a template
// created with them is one Meta rejects. Refusing with a sentence that explains
// the missing piece beats forwarding a provider error nobody can act on.
func (o OTPType) Supported() bool {
	return o == OTPTypeCopyCode
}

type UsabilityStatus string

const (
	UsabilityStatusReady              UsabilityStatus = "ready"
	UsabilityStatusMissingHeaderMedia UsabilityStatus = "missing_header_media"
	UsabilityStatusNotApproved        UsabilityStatus = "not_approved"
)

type ParameterFormat string

const (
	ParameterFormatPositional ParameterFormat = "positional"
	ParameterFormatNamed      ParameterFormat = "named"
)

func (p ParameterFormat) IsValid() bool {
	switch p {
	case ParameterFormatPositional, ParameterFormatNamed, "":
		return true
	default:
		return false
	}
}

type Template struct {
	ID              string              `json:"id"`
	ExternalID      string              `json:"externalId"`
	WABAId          string              `json:"wabaId"`
	WABAName        string              `json:"wabaName,omitempty"`
	Name            string              `json:"name"`
	Language        string              `json:"language"`
	Category        TemplateCategory    `json:"category"`
	Status          TemplateStatus      `json:"status"`
	ParameterFormat ParameterFormat     `json:"parameterFormat,omitempty"`
	Components      []TemplateComponent `json:"components"`
	HeaderMediaURL  *string             `json:"headerMediaUrl,omitempty"`
	HeaderMediaID   *string             `json:"headerMediaId,omitempty"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type TemplateComponent struct {
	Type       string           `json:"type"`
	Format     string           `json:"format,omitempty"`
	Text       string           `json:"text,omitempty"`
	Buttons    []TemplateButton `json:"buttons,omitempty"`
	Parameters []string         `json:"parameters,omitempty"`
	Example    *TemplateExample `json:"example,omitempty"`

	// AddSecurityRecommendation and CodeExpirationMinutes are how an
	// authentication template's BODY and FOOTER are written: Meta owns the copy
	// and renders it per language, so the business sets a flag and a number
	// instead of text. Pointers because "not set" and "set to false/zero" are
	// different instructions to Meta. Nil on every other category.
	AddSecurityRecommendation *bool `json:"addSecurityRecommendation,omitempty"`
	CodeExpirationMinutes     *int  `json:"codeExpirationMinutes,omitempty"`
}

type TemplateExample struct {
	HeaderText      []string            `json:"header_text,omitempty"`
	HeaderHandle    []string            `json:"header_handle,omitempty"`
	BodyText        [][]string          `json:"body_text,omitempty"`
	BodyTextNamed   []NamedParamExample `json:"body_text_named_params,omitempty"`
	HeaderTextNamed []NamedParamExample `json:"header_text_named_params,omitempty"`
}

type NamedParamExample struct {
	ParamName string `json:"param_name"`
	Example   string `json:"example"`
}

type TemplateButton struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	URL         string `json:"url,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Example     string `json:"example,omitempty"`
	// OTPType is COPY_CODE, ONE_TAP or ZERO_TAP on a type OTP button. See
	// authentication.go for why it is a field of its own rather than a Type.
	OTPType string `json:"otpType,omitempty"`
}

func NewTemplate(name, externalID, language string, category TemplateCategory, status TemplateStatus) (*Template, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrTemplateNameRequired
	}

	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil, ErrExternalIDRequired
	}

	if language == "" {
		language = "pt_BR"
	}

	if !category.IsValid() {
		category = TemplateCategoryMarketing
	}

	if !status.IsValid() {
		status = TemplateStatusPending
	}

	return &Template{
		Name:       strings.ToLower(name),
		ExternalID: externalID,
		Language:   language,
		Category:   category,
		Status:     status,
		Components: []TemplateComponent{},
	}, nil
}

func (t *Template) AddComponent(componentType, format, text string, parameters []string) {
	t.Components = append(t.Components, TemplateComponent{
		Type:       strings.ToUpper(componentType),
		Format:     format,
		Text:       text,
		Parameters: parameters,
	})
}

func (t *Template) IsApproved() bool {
	return t.Status == TemplateStatusApproved
}

func (t *Template) BillingCategory() (string, error) {
	if t == nil {
		return "", ErrTemplateNotFound
	}

	category := TemplateCategory(strings.ToUpper(strings.TrimSpace(string(t.Category))))
	if !category.IsValid() {
		return "", ErrTemplateCategoryUnavailable
	}

	return string(category), nil
}

func (t *Template) CanBeSent() bool {
	return t.Status == TemplateStatusApproved
}

func (t *Template) GetHeaderFormat() string {
	for _, c := range t.Components {
		if strings.ToUpper(c.Type) == "HEADER" {
			return strings.ToUpper(c.Format)
		}
	}
	return ""
}

func (t *Template) HasMediaHeader() bool {
	format := t.GetHeaderFormat()
	return format == "IMAGE" || format == "VIDEO" || format == "DOCUMENT"
}

func (t *Template) GetHeaderMediaURL() string {
	if t.HeaderMediaURL != nil {
		return *t.HeaderMediaURL
	}
	return ""
}

func (t *Template) GetHeaderMediaID() string {
	if t.HeaderMediaID != nil {
		return *t.HeaderMediaID
	}
	return ""
}

func (t *Template) GetUsabilityStatus() UsabilityStatus {
	if t.Status != TemplateStatusApproved {
		return UsabilityStatusNotApproved
	}

	if t.HasMediaHeader() && t.GetHeaderMediaID() == "" {
		return UsabilityStatusMissingHeaderMedia
	}

	return UsabilityStatusReady
}

func (t *Template) IsReadyToSend() bool {
	return t.GetUsabilityStatus() == UsabilityStatusReady
}

func (t *Template) GetUsabilityMessage() string {
	switch t.GetUsabilityStatus() {
	case UsabilityStatusMissingHeaderMedia:
		return "Template has " + t.GetHeaderFormat() + " header but no header media configured. Use PATCH /whatsapp/templates/{id}/header-media to set it (the media will be uploaded to WhatsApp automatically)."
	case UsabilityStatusNotApproved:
		return "Template is not approved by Meta. Current status: " + string(t.Status)
	default:
		return ""
	}
}

func (t *Template) GetBodyText() string {
	for _, c := range t.Components {
		if c.Type == "BODY" {
			return c.Text
		}
	}
	return ""
}

func (t *Template) ParameterCount() int {
	count := 0
	for _, c := range t.Components {
		if c.Type == "BODY" {
			count += countTemplateParameters(c.Text)
		}
		if c.Type == "HEADER" && c.Format == "TEXT" {
			count += countTemplateParameters(c.Text)
		}
	}

	// An authentication template takes the code and nothing else, whatever its
	// stored body text parses to. Meta owns that text: it is created empty (the
	// business sends a flag, not a sentence) and read back rendered, so counting
	// placeholders answers 0 for a template that needs exactly 1 — and a caller
	// that trusts 0, like the campaign consumer slicing entry variables, sends
	// no code at all.
	if t.IsAuthentication() && count < authenticationBodyParamCount {
		return authenticationBodyParamCount
	}
	return count
}

// IsAuthentication reports whether Meta prices and renders this as an
// authentication template.
func (t *Template) IsAuthentication() bool {
	if t == nil {
		return false
	}
	return TemplateCategory(strings.ToUpper(strings.TrimSpace(string(t.Category)))) == TemplateCategoryAuthentication
}

// OTPButton returns the code button and its index WITHIN the BUTTONS component.
//
// The index is the part worth being careful about: it is what the send payload
// keys the parameter on, and it counts buttons, not components. A template with
// a quick reply ahead of its code button puts the code at index 1, and sending
// it at index 0 addresses the quick reply instead.
func (t *Template) OTPButton() (TemplateButton, int, bool) {
	if t == nil {
		return TemplateButton{}, 0, false
	}
	for _, comp := range t.Components {
		if strings.ToUpper(comp.Type) != "BUTTONS" {
			continue
		}
		for i, btn := range comp.Buttons {
			if strings.ToUpper(strings.TrimSpace(btn.Type)) == ButtonTypeOTP {
				return btn, i, true
			}
		}
	}
	return TemplateButton{}, 0, false
}

// AuthenticationCode pulls the one-time code out of the body parameters.
//
// The code is an ordinary first body parameter, which is the whole point: every
// existing send path already collects body parameters (a campaign from its
// entry variables, a workflow from its node config, quick-send from the
// request), so none of them needs a new field to send an authentication
// template. The duplication Meta requires — the same code in the body and on
// the button — happens in BuildSendInput, once.
func (t *Template) AuthenticationCode(bodyParams []string) (string, error) {
	if len(bodyParams) < authenticationBodyParamCount {
		return "", ErrAuthenticationCodeRequired
	}
	code := strings.TrimSpace(bodyParams[0])
	if code == "" {
		return "", ErrAuthenticationCodeRequired
	}
	// Meta caps the code at 15 characters. Counted in runes rather than bytes:
	// the cap is about what fits the button, and a byte count would refuse a
	// shorter code that happens to carry an accent.
	if len([]rune(code)) > MaxAuthenticationCodeLength {
		return "", ErrAuthenticationCodeTooLong
	}
	return code, nil
}

func (t *Template) GetParameterNames() []string {
	var params []string
	seen := make(map[string]bool)

	for _, c := range t.Components {
		if c.Type == "BODY" || (c.Type == "HEADER" && c.Format == "TEXT") {
			extracted := extractTemplateParameters(c.Text)
			for _, p := range extracted {
				if !seen[p] {
					seen[p] = true
					params = append(params, p)
				}
			}
		}
	}
	return params
}

func (t *Template) GetBodyAndHeaderParameterNames() (bodyParams []string, headerParams []string) {
	for _, c := range t.Components {
		if c.Type == "BODY" {
			bodyParams = append(bodyParams, extractTemplateParameters(c.Text)...)
		}
		if c.Type == "HEADER" && c.Format == "TEXT" {
			headerParams = append(headerParams, extractTemplateParameters(c.Text)...)
		}
	}

	// Same rule as ParameterCount, and for the same reason: Meta owns an
	// authentication template's body, so there may be no placeholder to take a
	// name from even though the template takes exactly one parameter. Naming it
	// "1" here is what lets a workflow node, whose config is keyed by parameter
	// name, collect the code at all.
	if t.IsAuthentication() && len(bodyParams) == 0 {
		bodyParams = []string{"1"}
	}
	return
}

func extractTemplateParameters(text string) []string {
	if text == "" {
		return nil
	}

	var params []string
	remaining := text

	for {
		startIdx := strings.Index(remaining, "{{")
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(remaining[startIdx:], "}}")
		if endIdx == -1 {
			break
		}
		paramName := remaining[startIdx+2 : startIdx+endIdx]
		paramName = strings.TrimSpace(paramName)
		if paramName != "" {
			params = append(params, paramName)
		}
		remaining = remaining[startIdx+endIdx+2:]
	}
	return params
}

func countTemplateParameters(text string) int {
	if text == "" {
		return 0
	}

	maxParam := 0
	for i := 1; i <= 99; i++ {
		placeholder := "{{" + strconv.Itoa(i) + "}}"
		if strings.Contains(text, placeholder) {
			maxParam = i
		}
	}

	if maxParam > 0 {
		return maxParam
	}

	params := extractTemplateParameters(text)
	return len(params)
}

// hasMixedParameterStyles reports whether the body/header text mixes numbered
// ({{1}}) and named ({{order}}) placeholders. Meta requires a single
// parameter_format per template, so a mix is rejected with INVALID_FORMAT.
func hasMixedParameterStyles(components []TemplateComponent) bool {
	hasPositional, hasNamed := false, false
	for _, c := range components {
		ct := strings.ToUpper(c.Type)
		if ct != "BODY" && !(ct == "HEADER" && strings.ToUpper(c.Format) == "TEXT") {
			continue
		}
		for _, p := range extractTemplateParameters(c.Text) {
			if _, err := strconv.Atoi(p); err == nil {
				hasPositional = true
			} else {
				hasNamed = true
			}
		}
	}
	return hasPositional && hasNamed
}

func (t *Template) HasOnlyPositionalParameters() bool {
	params := t.GetParameterNames()
	if len(params) == 0 {
		return true
	}

	for _, param := range params {
		if _, err := strconv.Atoi(param); err != nil {
			return false
		}
	}
	return true
}

func (t *Template) IsNamedParameterFormat() bool {
	// The body is the source of truth. A template whose placeholders are named
	// ({{nome}}) is NAMED regardless of a stale or incorrect stored ParameterFormat:
	// synced/duplicate rows sometimes carry a wrong "positional" value even though
	// the approved template uses named variables, and sending those positionally
	// makes Meta reject the whole send with (#100) "Parameter name is missing or
	// empty". Only when the placeholders are purely positional ({{1}}) or absent do
	// we fall back to the stored format.
	if !t.HasOnlyPositionalParameters() {
		return true
	}
	return t.ParameterFormat == ParameterFormatNamed
}

func (t *Template) GetEffectiveParameterFormat() ParameterFormat {
	if t.ParameterFormat != "" && t.ParameterFormat.IsValid() {
		return t.ParameterFormat
	}
	if t.HasOnlyPositionalParameters() {
		return ParameterFormatPositional
	}
	return ParameterFormatNamed
}

// ToMetaAPIFormat maps the domain parameter format to the value Meta's Cloud
// API expects in the top-level "parameter_format" field ("NAMED"/"POSITIONAL").
// Meta requires this to be present and to match the placeholder style used in
// the body: named placeholders ({{order}}) sent without parameter_format=NAMED
// are rejected with rejected_reason=INVALID_FORMAT.
func (p ParameterFormat) ToMetaAPIFormat() string {
	switch p {
	case ParameterFormatNamed:
		return "NAMED"
	case ParameterFormatPositional:
		return "POSITIONAL"
	default:
		return ""
	}
}

const (
	MaxTemplateNameLength = 512
	MaxHeaderTextLength   = 60
	MaxBodyTextLength     = 1024
	MaxFooterTextLength   = 60
	MaxButtonTextLength   = 25
	MaxButtonURLLength    = 2000
	MaxButtons            = 10

	// MinCodeExpirationMinutes and MaxCodeExpirationMinutes bound an
	// authentication template's expiry line. Meta's own limits, enforced here so
	// a rejected template is a validation error the operator can read rather
	// than a 400 they cannot.
	MinCodeExpirationMinutes = 1
	MaxCodeExpirationMinutes = 90

	// MaxAuthenticationCodeLength is Meta's cap on the one-time code itself,
	// which is also the cap on the copy-code button example string.
	MaxAuthenticationCodeLength = 15
)

// ButtonTypeOTP is how an authentication template declares its code button.
// Meta decides the label and the behaviour from the button's OTPType.
//
// Distinct from COPY_CODE, which is the coupon button on a marketing template:
// the two look alike to a reader and take different parameters on send.
const ButtonTypeOTP = "OTP"

// authenticationBodyParamCount is how many parameters an authentication
// template takes: the code, once. Meta's format is fixed, so this is a constant
// rather than something parsed out of the body.
const authenticationBodyParamCount = 1

// ComponentTypeCallPermissionRequest is a parameter-less component that asks the
// user for permission to receive a WhatsApp Business call. Meta renders the
// accept/decline (and temporary/permanent) buttons automatically; the template
// only carries this marker plus the required BODY (and optional HEADER/FOOTER).
const ComponentTypeCallPermissionRequest = "CALL_PERMISSION_REQUEST"

var validComponentTypes = map[string]bool{
	"HEADER":                           true,
	"BODY":                             true,
	"FOOTER":                           true,
	"BUTTONS":                          true,
	ComponentTypeCallPermissionRequest: true,
}

var validHeaderFormats = map[string]bool{
	"TEXT":     true,
	"IMAGE":    true,
	"VIDEO":    true,
	"DOCUMENT": true,
	"LOCATION": true,
	"GIF":      true,
}

var validButtonTypes = map[string]bool{
	"QUICK_REPLY":  true,
	"URL":          true,
	"PHONE_NUMBER": true,
	"COPY_CODE":    true,
	// OTP is the authentication code button. Distinct from COPY_CODE, which is
	// the coupon button on a marketing template: they look alike to a reader and
	// take different parameters on send. See authentication.go.
	ButtonTypeOTP: true,
}

var mediaHeaderFormats = map[string]bool{
	"IMAGE":    true,
	"VIDEO":    true,
	"DOCUMENT": true,
	"GIF":      true,
}

func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrTemplateNameRequired
	}

	if len(name) > MaxTemplateNameLength {
		return ErrTemplateNameTooLong
	}

	if len(name) > 0 && !unicode.IsLetter(rune(name[0])) {
		return ErrTemplateNameMustStartLetter
	}

	validNameRegex := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	if !validNameRegex.MatchString(name) {
		return ErrTemplateNameInvalidChars
	}

	return nil
}

func ValidateComponent(comp TemplateComponent) error {
	compType := strings.ToUpper(comp.Type)

	if !validComponentTypes[compType] {
		return ErrInvalidComponentType
	}

	switch compType {
	case "HEADER":
		return validateHeader(comp)
	case "BODY":
		return validateBody(comp)
	case "FOOTER":
		return validateFooter(comp)
	case "BUTTONS":
		return validateButtons(comp.Buttons)
	}

	return nil
}

func validateHeader(comp TemplateComponent) error {
	format := strings.ToUpper(comp.Format)

	if format == "" {
		return ErrHeaderFormatRequired
	}

	if !validHeaderFormats[format] {
		return ErrInvalidHeaderFormat
	}

	if format == "TEXT" {
		if len(comp.Text) > MaxHeaderTextLength {
			return ErrHeaderTextTooLong
		}

		params := extractTemplateParameters(comp.Text)
		if len(params) > 1 {
			return ErrHeaderTextTooManyVariables
		}

		if len(params) > 0 && (comp.Example == nil || (len(comp.Example.HeaderText) == 0 && len(comp.Example.HeaderTextNamed) == 0)) {
			return errors.New("header text with variables requires example.header_text or example.header_text_named_params")
		}
	}

	if mediaHeaderFormats[format] {
		if comp.Example == nil || len(comp.Example.HeaderHandle) == 0 {
			return ErrHeaderMediaNeedsHandle
		}
	}

	return nil
}

func validateBody(comp TemplateComponent) error {
	text := comp.Text

	if len(text) > MaxBodyTextLength {
		return ErrBodyTextTooLong
	}

	if strings.HasPrefix(strings.TrimSpace(text), "{{") {
		return ErrBodyVariableAtStart
	}

	trimmedText := strings.TrimSpace(text)
	if strings.HasSuffix(trimmedText, "}}") {
		lastVarEnd := strings.LastIndex(trimmedText, "}}")
		if lastVarEnd == len(trimmedText)-2 {
			beforeLastVar := trimmedText[:lastVarEnd]
			lastVarStart := strings.LastIndex(beforeLastVar, "{{")
			if lastVarStart != -1 {
				textAfterVar := trimmedText[lastVarEnd+2:]
				if len(strings.TrimSpace(textAfterVar)) == 0 {
					return ErrBodyVariableAtEnd
				}
			}
		}
	}

	consecutiveVarRegex := regexp.MustCompile(`\}\}\s*\{\{`)
	if consecutiveVarRegex.MatchString(text) {
		return ErrBodyConsecutiveVariables
	}

	params := extractTemplateParameters(text)
	if len(params) > 0 {
		if comp.Example == nil || (len(comp.Example.BodyText) == 0 && len(comp.Example.BodyTextNamed) == 0) {
			return ErrBodyNeedsExample
		}
	}

	return nil
}

func validateFooter(comp TemplateComponent) error {
	if len(comp.Text) > MaxFooterTextLength {
		return ErrFooterTextTooLong
	}

	if strings.Contains(comp.Text, "{{") && strings.Contains(comp.Text, "}}") {
		return ErrFooterHasVariables
	}

	return nil
}

func validateButtons(buttons []TemplateButton) error {
	if len(buttons) > MaxButtons {
		return ErrTooManyButtons
	}

	var buttonTypeOrder []string
	otpCount := 0
	for _, btn := range buttons {
		btnType := strings.ToUpper(strings.TrimSpace(btn.Type))

		if !validButtonTypes[btnType] {
			return ErrInvalidButtonType
		}

		// Meta writes the label for both of these, localized per language, so a
		// missing text is the normal case rather than a mistake. A label the
		// business did supply is still held to the length limit below.
		labelOptional := btnType == "COPY_CODE" || btnType == ButtonTypeOTP
		if !labelOptional {
			if strings.TrimSpace(btn.Text) == "" {
				return ErrButtonTextRequired
			}
		}
		if len(btn.Text) > MaxButtonTextLength {
			return ErrButtonTextTooLong
		}

		if btnType == ButtonTypeOTP {
			otpCount++
			if otpCount > 1 {
				// A second code button has no index Meta would send a second
				// code to, so it is a template that can never be sent.
				return ErrMultipleOTPButtons
			}
			otpType := OTPType(strings.ToUpper(strings.TrimSpace(btn.OTPType)))
			if otpType == "" {
				return ErrOTPTypeRequired
			}
			if !otpType.IsValid() {
				return ErrInvalidOTPType
			}
			// A real otp_type this product cannot build is a different failure
			// from a word that is not an otp_type, and the operator needs the
			// difference: one is a typo, the other needs an Android app.
			if !otpType.Supported() {
				return ErrOTPTypeUnsupported
			}
		}

		switch btnType {
		case "URL":
			if strings.TrimSpace(btn.URL) == "" {
				return ErrButtonURLRequired
			}
			if strings.Contains(btn.URL, "{{") {
				params := extractTemplateParameters(btn.URL)
				if len(params) > 1 {
					return ErrURLButtonTooManyVars
				}
				if len(params) == 1 && !strings.HasSuffix(strings.TrimSpace(btn.URL), "}}") {
					return ErrURLButtonVariableNotEnd
				}
				if len(params) > 0 && btn.Example == "" {
					return errors.New("URL button with variable requires an example value")
				}
			}
		case "PHONE_NUMBER":
			if strings.TrimSpace(btn.PhoneNumber) == "" {
				return ErrButtonPhoneRequired
			}
		case "COPY_CODE":
			if strings.TrimSpace(btn.Example) == "" {
				return ErrCopyCodeNeedsExample
			}
		}

		buttonTypeOrder = append(buttonTypeOrder, btnType)
	}

	if err := validateButtonGrouping(buttonTypeOrder); err != nil {
		return err
	}

	return nil
}

func validateButtonGrouping(buttonTypes []string) error {
	if len(buttonTypes) < 2 {
		return nil
	}

	seenQuickReply := false
	seenOtherAfterQuickReply := false

	for _, btnType := range buttonTypes {
		if btnType == "QUICK_REPLY" {
			if seenOtherAfterQuickReply {
				return ErrButtonsNotGrouped
			}
			seenQuickReply = true
		} else {
			if seenQuickReply {
				seenOtherAfterQuickReply = true
			}
		}
	}

	return nil
}

func ValidateComponents(components []TemplateComponent) error {
	hasBody := false
	hasButtons := false
	callPermissionCount := 0

	for _, comp := range components {
		switch strings.ToUpper(comp.Type) {
		case "BODY":
			hasBody = true
		case "BUTTONS":
			hasButtons = true
		case ComponentTypeCallPermissionRequest:
			callPermissionCount++
		}

		if err := ValidateComponent(comp); err != nil {
			return err
		}
	}

	if !hasBody {
		return errors.New("template must have a BODY component")
	}

	if hasMixedParameterStyles(components) {
		return ErrMixedParameterStyles
	}

	if callPermissionCount > 1 {
		return ErrMultipleCallPermissionRequests
	}

	// A call-permission template's accept/decline buttons are rendered by
	// WhatsApp, so it must not carry its own BUTTONS component.
	if callPermissionCount > 0 && hasButtons {
		return ErrCallPermissionWithButtons
	}

	return nil
}

// ValidateAuthenticationTemplate holds the rules that need the CATEGORY, which
// ValidateComponents does not take.
//
// Separate from ValidateComponents rather than folded into it because the
// category is a fact about the template and the components are a fact about its
// layout, and ValidateComponents is called from places that have only the
// second. Both run on create.
func ValidateAuthenticationTemplate(category TemplateCategory, components []TemplateComponent) error {
	normalized := TemplateCategory(strings.ToUpper(strings.TrimSpace(string(category))))
	isAuth := normalized == TemplateCategoryAuthentication

	hasOTP := false
	for _, comp := range components {
		switch strings.ToUpper(comp.Type) {
		case "HEADER":
			// Meta: "Authentication templates don't use a HEADER component."
			// Text or media, it does not matter; there is no header slot to
			// render one into.
			if isAuth {
				return ErrAuthenticationNoHeader
			}
		case "BODY":
			// The body is Meta's preset, translated per language. Business text
			// here is text Meta refuses, so it is refused where the message can
			// explain that rather than coming back as a rejected_reason.
			if isAuth && strings.TrimSpace(comp.Text) != "" {
				return ErrAuthenticationBodyNotEditable
			}
		case "BUTTONS":
			for _, btn := range comp.Buttons {
				if strings.ToUpper(strings.TrimSpace(btn.Type)) == ButtonTypeOTP {
					hasOTP = true
				}
			}
		case "FOOTER":
			if isAuth && strings.TrimSpace(comp.Text) != "" {
				return ErrAuthenticationFooterNotEditable
			}
			if comp.CodeExpirationMinutes != nil {
				minutes := *comp.CodeExpirationMinutes
				if minutes < MinCodeExpirationMinutes || minutes > MaxCodeExpirationMinutes {
					return ErrCodeExpirationOutOfRange
				}
			}
		}
	}

	if hasOTP && !isAuth {
		return ErrOTPButtonNotAuthentication
	}
	if isAuth && !hasOTP {
		return ErrAuthenticationNeedsOTPButton
	}
	return nil
}
