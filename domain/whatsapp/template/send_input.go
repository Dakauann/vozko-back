package template

import (
	"strings"

	"vozko/domain/conversation"
)

// Turning a template plus its parameters into a provider send used to happen in
// four places: the billed sender, the campaign consumer, the workflow node and
// the conversation composer. They agreed on the easy parts and drifted on the
// rest — one attached header media by id, one by URL, one set the named-format
// flag from the template and one from a separate branch — and none of them knew
// anything about buttons, which is why an authentication template went out with
// its code in the body and nothing on the button for Meta to find.
//
// This is that assembly, once. Every caller passes what it actually has (a
// recipient, body parameters, header parameters) and gets back the send input,
// so a rule added here reaches all four without any of them being edited again.

// SendInputParams is what a caller knows. Everything else comes from the
// template.
type SendInputParams struct {
	To string
	// BodyParams are the body variables in order. For an authentication
	// template the first one is the code, and it is copied onto the button.
	BodyParams []string
	// HeaderParams are the text header variables. Ignored by a media header.
	HeaderParams []string
	// FromPhoneNumberID overrides the client's own number. Empty means the
	// client's default.
	FromPhoneNumberID string
	// BizOpaqueCallbackData rides to Meta and comes back on every delivery
	// status, which is how a status event finds the charge that paid for it.
	BizOpaqueCallbackData string
}

// BuildSendInput assembles the provider call for this template.
//
// It refuses rather than sends when the template needs something the caller did
// not give it. An authentication template with no code is the case that matters:
// Meta answers that with error 132000, a parameter count mismatch that names
// neither the template nor the code, and which an operator reading a campaign
// report has no way to act on.
func (t *Template) BuildSendInput(p SendInputParams) (conversation.SendTemplateMessageInput, error) {
	bodyParamNames, _ := t.GetBodyAndHeaderParameterNames()

	out := conversation.SendTemplateMessageInput{
		To:                     p.To,
		TemplateName:           t.Name,
		Language:               t.Language,
		Parameters:             p.BodyParams,
		ParameterNames:         bodyParamNames,
		IsNamedParameterFormat: t.IsNamedParameterFormat(),
		HeaderTextParams:       p.HeaderParams,
		FromPhoneNumberID:      p.FromPhoneNumberID,
		BizOpaqueCallbackData:  p.BizOpaqueCallbackData,
	}

	if t.HasMediaHeader() {
		// The id is what a send is meant to carry: it is minted at create time
		// and re-used, while the URL is only the create-time example. The URL
		// stays as a fallback because a template whose id was never minted
		// (an older row, a failed upload) would otherwise lose its header
		// entirely, and Meta rejects a media-header template sent without one.
		out.HeaderType = strings.ToLower(t.GetHeaderFormat())
		if id := t.GetHeaderMediaID(); id != "" {
			out.HeaderMediaID = id
		} else {
			out.HeaderMediaURL = t.GetHeaderMediaURL()
		}
	}

	if t.IsAuthentication() {
		code, err := t.AuthenticationCode(p.BodyParams)
		if err != nil {
			return conversation.SendTemplateMessageInput{}, err
		}
		// No OTP button is a legitimate shape: an authentication template can
		// be code-only, with nothing to tap. There is then simply no button
		// component to parameterize.
		if _, index, ok := t.OTPButton(); ok {
			out.Buttons = append(out.Buttons, conversation.TemplateButtonParam{
				// Every OTP button is addressed as "url" on send, copy-code
				// included. Only a coupon COPY_CODE button uses "copy_code",
				// and that is a different button in a different category.
				SubType: conversation.TemplateButtonSubTypeURL,
				Index:   index,
				Text:    code,
			})
		}
	}

	if err := out.Validate(); err != nil {
		return conversation.SendTemplateMessageInput{}, err
	}
	return out, nil
}
