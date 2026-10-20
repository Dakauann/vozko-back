package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/conversation"
)

func formButton() TemplateButton {
	return TemplateButton{Type: ButtonTypeFlow, Text: "Responder", FlowID: "F1", FlowAction: FlowActionNavigate, NavigateScreen: "S_INICIO"}
}

func quickReply(text string) TemplateButton {
	return TemplateButton{Type: ButtonTypeQuickReply, Text: text}
}

func TestFlowButtonRules(t *testing.T) {
	withAction := func(action, screen string) TemplateButton {
		b := formButton()
		b.FlowAction, b.NavigateScreen = action, screen
		return b
	}
	withoutFlow := formButton()
	withoutFlow.FlowID = " "
	withoutText := formButton()
	withoutText.Text = ""
	longText := formButton()
	longText.Text = strings.Repeat("a", MaxButtonTextLength+1)
	lowerType := formButton()
	lowerType.Type = "flow"

	cases := []struct {
		name    string
		buttons []TemplateButton
		want    error
	}{
		{name: "navigate with a screen", buttons: []TemplateButton{formButton()}},
		{name: "no action defaults to navigate", buttons: []TemplateButton{withAction("", "S_INICIO")}},
		{name: "upper case navigate", buttons: []TemplateButton{withAction("NAVIGATE", "S_INICIO")}},
		{name: "lower case type", buttons: []TemplateButton{lowerType}},
		{name: "data exchange needs no screen", buttons: []TemplateButton{withAction(FlowActionDataExchange, "")}},
		{name: "next to quick replies", buttons: []TemplateButton{quickReply("Sim"), quickReply("Nao"), formButton()}},
		{name: "next to a link", buttons: []TemplateButton{formButton(), {Type: "URL", Text: "Site", URL: "https://loja.com"}}},
		{name: "needs a flow", buttons: []TemplateButton{withoutFlow}, want: ErrFlowButtonNeedsFlow},
		{name: "navigate needs a screen", buttons: []TemplateButton{withAction(FlowActionNavigate, "")}, want: ErrFlowButtonNeedsScreen},
		{name: "default action needs a screen", buttons: []TemplateButton{withAction("", " ")}, want: ErrFlowButtonNeedsScreen},
		{name: "unknown action", buttons: []TemplateButton{withAction("open", "S_INICIO")}, want: ErrInvalidFlowAction},
		{name: "needs text", buttons: []TemplateButton{withoutText}, want: ErrButtonTextRequired},
		{name: "text at most 25 characters", buttons: []TemplateButton{longText}, want: ErrButtonTextTooLong},
		{name: "one form per template", buttons: []TemplateButton{formButton(), formButton()}, want: ErrMultipleFlowButtons},
		{name: "quick replies stay grouped", buttons: []TemplateButton{quickReply("Sim"), formButton(), quickReply("Nao")}, want: ErrButtonsNotGrouped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateComponent(TemplateComponent{Type: "BUTTONS", Buttons: tc.buttons})

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFlowButtonErrorsHaveCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{ErrFlowButtonNeedsFlow, CodeFlowButtonNeedsFlow},
		{ErrFlowButtonNeedsScreen, CodeFlowButtonNeedsScreen},
		{ErrInvalidFlowAction, CodeInvalidFlowAction},
		{ErrMultipleFlowButtons, CodeMultipleFlowButtons},
		{ErrFlowTokenRequired, CodeFlowTokenRequired},
		{ErrFlowNotPublished, CodeFlowNotPublished},
		{ErrFormNotSendable, CodeFormNotSendable},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.code {
			t.Errorf("%v code = %q, want %q", tc.err, got, tc.code)
		}
	}
}

func TestFormButtonFindsTheFlowButtonAndItsIndex(t *testing.T) {
	cases := []struct {
		name      string
		tmpl      *Template
		wantIndex int
		wantFound bool
	}{
		{name: "nil template", tmpl: nil},
		{name: "no buttons", tmpl: &Template{Components: []TemplateComponent{{Type: "BODY", Text: "Oi"}}}},
		{name: "after quick replies", tmpl: &Template{Components: []TemplateComponent{{Type: "BODY", Text: "Oi"}, {Type: "BUTTONS", Buttons: []TemplateButton{quickReply("Sim"), quickReply("Nao"), formButton()}}}}, wantIndex: 2, wantFound: true},
		{name: "lower case types", tmpl: &Template{Components: []TemplateComponent{{Type: "buttons", Buttons: []TemplateButton{{Type: "flow", Text: "Responder", FlowID: "F1"}}}}}, wantIndex: 0, wantFound: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			button, index, found := tc.tmpl.FormButton()

			if found != tc.wantFound || index != tc.wantIndex {
				t.Fatalf("found = %v index = %d", found, index)
			}
			if found && button.FlowID != "F1" {
				t.Fatalf("button = %+v", button)
			}
		})
	}
}

func formTemplate(buttons ...TemplateButton) *Template {
	return &Template{
		Name:     "cadastro",
		Language: "pt_BR",
		Category: TemplateCategoryMarketing,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "Responda o cadastro"},
			{Type: "BUTTONS", Buttons: buttons},
		},
	}
}

func TestBuildSendInputCarriesTheFormToken(t *testing.T) {
	cases := []struct {
		name        string
		tmpl        *Template
		token       string
		wantButtons []conversation.TemplateButtonParam
		wantErr     error
	}{
		{
			name:        "form button gets the token",
			tmpl:        formTemplate(formButton()),
			token:       "tok_1",
			wantButtons: []conversation.TemplateButtonParam{{SubType: conversation.TemplateButtonSubTypeFlow, Index: 0, FlowToken: "tok_1"}},
		},
		{
			name:  "form button after quick replies",
			tmpl:  formTemplate(quickReply("Sim"), formButton()),
			token: "tok_1",
			wantButtons: []conversation.TemplateButtonParam{
				{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 0, Payload: QuickReplyPayload(0)},
				{SubType: conversation.TemplateButtonSubTypeFlow, Index: 1, FlowToken: "tok_1"},
			},
		},
		{name: "form button without a token is refused", tmpl: formTemplate(formButton()), wantErr: ErrFlowTokenRequired},
		{name: "blank token is refused", tmpl: formTemplate(formButton()), token: "  ", wantErr: ErrFlowTokenRequired},
		{name: "no form button ignores a token", tmpl: formTemplate(quickReply("Sim")), token: "tok_1", wantButtons: []conversation.TemplateButtonParam{{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 0, Payload: QuickReplyPayload(0)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.tmpl.BuildSendInput(SendInputParams{To: "5511987654321", FlowToken: tc.token})

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if !reflect.DeepEqual(out.Buttons, tc.wantButtons) {
				t.Fatalf("buttons = %+v", out.Buttons)
			}
		})
	}
}

func TestCheckSendInputLeavesOnlyTheTokenToThePreparedSend(t *testing.T) {
	cases := []struct {
		name    string
		tmpl    *Template
		to      string
		wantErr error
	}{
		{name: "form template before its token", tmpl: formTemplate(formButton()), to: "5511987654321"},
		{name: "template without a form", tmpl: formTemplate(quickReply("Sim")), to: "5511987654321"},
		{name: "form template still needs a recipient", tmpl: formTemplate(formButton()), wantErr: conversation.ErrWhatsAppRecipientRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tmpl.CheckSendInput(SendInputParams{To: tc.to})

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestCheckSendInputNeverBuildsAFormSendWithoutItsToken(t *testing.T) {
	tmpl := formTemplate(formButton())

	if err := tmpl.CheckSendInput(SendInputParams{To: "5511987654321"}); err != nil {
		t.Fatalf("check = %v", err)
	}
	if _, err := tmpl.BuildSendInput(SendInputParams{To: "5511987654321"}); !errors.Is(err, ErrFlowTokenRequired) {
		t.Fatalf("build = %v, want ErrFlowTokenRequired", err)
	}
}

func TestClientComponentsKeepTheFlowFields(t *testing.T) {
	original := []TemplateComponent{{Type: "BUTTONS", Buttons: []TemplateButton{formButton()}}}

	client := ToClientComponents(original)

	if got := client[0].Buttons[0]; got.FlowID != "F1" || got.FlowAction != FlowActionNavigate || got.NavigateScreen != "S_INICIO" {
		t.Fatalf("client button = %+v", got)
	}
	if back := FromClientComponents(client); !reflect.DeepEqual(back, original) {
		t.Fatalf("round trip = %+v", back)
	}
}
