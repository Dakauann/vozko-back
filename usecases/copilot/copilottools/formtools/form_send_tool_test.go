package formtools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	wf "vozko/domain/whatsappform"
	"vozko/usecases/copilot/copilottools/tooltest"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

func sendArgs(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "form_id": knownForm,
		"body": "Preencha seus dados para continuarmos.", "cta": "Preencher", "header": "Cadastro", "footer": "Leva um minuto",
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func sendableForms() *fakeForms {
	forms := formsWith(knownDetail())
	forms.sendable = []whatsappform_usecase.SendableForm{{FormID: knownForm, Name: "Captação"}}
	return forms
}

func TestSendWhatsAppFormChecksTheNumberBeforeApprovalAndSends(t *testing.T) {
	forms := sendableForms()
	tool := NewSendWhatsAppFormTool(deps(forms))

	if err := tooltest.Validate(t, tool, tooltest.Member(), sendArgs(nil)); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if forms.sendableQ.PhoneID != knownPhone {
		t.Fatalf("sendable must be asked for the conversation's number: %+v", forms.sendableQ)
	}

	res := tool.Execute(context.Background(), tooltest.Member(), sendArgs(nil))

	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	want := wf.SendCopy{Header: "Cadastro", Body: "Preencha seus dados para continuarmos.", Footer: "Leva um minuto", CTA: "Preencher"}
	if forms.sent.EntryID != knownEntry || forms.sent.FormID != knownForm || forms.sent.Copy != want {
		t.Fatalf("sent = %+v", forms.sent)
	}
	if tooltest.DataOf(res)["send_id"] != "send-1" {
		t.Fatalf("data = %+v", tooltest.DataOf(res))
	}
}

func TestSendWhatsAppFormRefusesBeforeApproval(t *testing.T) {
	closed := openEntry()
	closed.WindowOpen = false
	cases := map[string]struct {
		forms *fakeForms
		deps  func(FormDeps) FormDeps
		args  map[string]interface{}
		want  string
	}{
		"closed window": {forms: sendableForms(), deps: func(d FormDeps) FormDeps { d.Entries = fakeEntries{entry: closed}; return d }, args: sendArgs(nil), want: "send_template"},
		"not published": {forms: formsWith(knownDetail()), args: sendArgs(nil), want: "publish_whatsapp_form"},
		"no body":       {forms: sendableForms(), args: sendArgs(map[string]interface{}{"body": ""}), want: "body"},
		"long button":   {forms: sendableForms(), args: sendArgs(map[string]interface{}{"cta": strings.Repeat("a", conversation.WhatsAppFlowCTAMaxLen+1)}), want: "botão"},
		"other channel": {forms: sendableForms(), args: sendArgs(map[string]interface{}{"entry_type": "instagram"}), want: "entry_type"},
		"unknown chat":  {forms: sendableForms(), args: sendArgs(map[string]interface{}{"entry_id": otherForm}), want: "conversa"},
	}
	for name, c := range cases {
		d := deps(c.forms)
		if c.deps != nil {
			d = c.deps(d)
		}
		err := tooltest.Validate(t, NewSendWhatsAppFormTool(d), tooltest.Member(), c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", name, err)
		}
		if c.forms.called("SendInConversation") {
			t.Errorf("%s: sent during the preflight", name)
		}
	}
}

func TestSendWhatsAppFormExplainsTheTemplateAlternativeWhenTheWindowCloses(t *testing.T) {
	for _, err := range []error{wf.ErrOutsideWindow, conversation.ErrWindowClosed} {
		forms := sendableForms()
		forms.errOn = map[string]error{"SendInConversation": err}

		res := NewSendWhatsAppFormTool(deps(forms)).Execute(context.Background(), tooltest.Member(), sendArgs(nil))

		if res.Status == copilot.StatusOK || !strings.Contains(res.Message, "24 horas") || !strings.Contains(res.Message, "send_template") {
			t.Errorf("%v: %+v", err, res)
		}
	}
}

func TestSendWhatsAppFormCardNamesTheConversationFormAndCopy(t *testing.T) {
	fields := NewSendWhatsAppFormTool(deps(sendableForms())).(copilot.Describer).Describe(context.Background(), tooltest.Member(), sendArgs(nil))

	got := tooltest.FieldMap(fields)
	if !strings.Contains(got["conversation"], "Maria") || got["form"] != "Captação" || got["body"] != "Preencha seus dados para continuarmos." ||
		got["headerText"] != "Cadastro" || got["footer"] != "Leva um minuto" || !strings.Contains(got["sending"], "Preencher") {
		t.Fatalf("fields = %+v", got)
	}
}

func TestSendWhatsAppFormNeedsTheSendCapability(t *testing.T) {
	tool := NewSendWhatsAppFormTool(deps(&fakeForms{}))
	if m := tool.Meta(); !m.Mutating || m.Resource != "whatsapp_forms" || m.Action != "send" {
		t.Fatalf("meta = %+v", m)
	}
	if !requires(tool, "whatsapp_forms:read", "whatsapp_forms:send", "conversations:read") {
		t.Fatalf("requires %v", requirementKeys(tool))
	}
}
