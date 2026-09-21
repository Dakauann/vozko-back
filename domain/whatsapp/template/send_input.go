package template

import (
	"strings"

	"vozko/domain/conversation"
)

type SendInputParams struct {
	To                    string
	BodyParams            []string
	HeaderParams          []string
	FromPhoneNumberID     string
	BizOpaqueCallbackData string
}

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
		if _, index, ok := t.OTPButton(); ok {
			out.Buttons = append(out.Buttons, conversation.TemplateButtonParam{
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
