package template_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

func approvedAuthentication() *template.Template {
	return &template.Template{
		ID: "tpl-1", WABAId: "waba-1", Name: "codigo_verificacao", Language: "pt_BR",
		Category: template.TemplateCategoryAuthentication,
		Status:   template.TemplateStatusApproved,
		Components: []template.TemplateComponent{
			{Type: "BODY", Text: "{{1}} e o seu codigo de verificacao."},
			{Type: "BUTTONS", Buttons: []template.TemplateButton{
				{Type: template.ButtonTypeOTP, OTPType: string(template.OTPTypeCopyCode)},
			}},
		},
	}
}

func authHarness(t *testing.T) *harness {
	t.Helper()
	return newHarness(t, func(d *BilledTemplateSenderDeps) {
		d.Templates = &stubTemplateRepo{tmpl: approvedAuthentication()}
	})
}

// The send that used to go out wrong: the code reached the body and nothing
// reached the button, and Meta answered 132000.
func TestBilledSend_AuthenticationPutsTheCodeOnTheButton(t *testing.T) {
	h := authHarness(t)

	in := validInput()
	in.BodyParams = []string{"482913"}

	if _, err := h.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("send: %v", err)
	}
	if h.client.sendCount() != 1 {
		t.Fatalf("sendCount = %d, want 1", h.client.sendCount())
	}

	sent := h.client.lastIn
	if len(sent.Parameters) != 1 || sent.Parameters[0] != "482913" {
		t.Errorf("body parameters = %v, want the code", sent.Parameters)
	}
	if len(sent.Buttons) != 1 {
		t.Fatalf("buttons = %v, want one", sent.Buttons)
	}
	if sent.Buttons[0].SubType != conversation.TemplateButtonSubTypeURL {
		t.Errorf("subType = %q, want url", sent.Buttons[0].SubType)
	}
	if sent.Buttons[0].Text != "482913" {
		t.Errorf("button text = %q, want the same code the body carries", sent.Buttons[0].Text)
	}
}

// The reason the payload is assembled before the debit rather than after it.
// A caller that forgot the code must not pay for a message Meta will refuse.
func TestBilledSend_AuthenticationWithoutACodeIsRefusedBeforeTheCharge(t *testing.T) {
	h := authHarness(t)

	in := validInput()
	in.BodyParams = nil

	_, err := h.uc.Execute(context.Background(), in)
	if !errors.Is(err, template.ErrAuthenticationCodeRequired) {
		t.Fatalf("want ErrAuthenticationCodeRequired, got %v", err)
	}
	if h.client.sendCount() != 0 {
		t.Error("a send Meta would refuse must not reach the provider")
	}
	if h.billing.debitCount() != 0 {
		t.Error("nothing may be charged for a send that never happens")
	}
}

// An ordinary template is untouched by any of this.
func TestBilledSend_UtilityStillSendsNoButtons(t *testing.T) {
	h := newHarness(t)

	in := validInput()
	in.BodyParams = []string{"Marina"}

	if _, err := h.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(h.client.lastIn.Buttons) != 0 {
		t.Errorf("buttons = %v, want none", h.client.lastIn.Buttons)
	}
}

// The attempt id still reaches Meta as biz_opaque_callback_data. It is stamped
// on after the payload is built, so this is the test that catches the stamping
// being dropped along with the restructure.
func TestBilledSend_StillCarriesTheAttemptID(t *testing.T) {
	h := newHarness(t)

	res, err := h.uc.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if h.client.lastIn.BizOpaqueCallbackData == "" {
		t.Fatal("biz_opaque_callback_data is empty: a delivery status could not find its charge")
	}
	if h.client.lastIn.BizOpaqueCallbackData != res.AttemptID {
		t.Errorf("bizOpaque = %q, want the attempt id %q",
			h.client.lastIn.BizOpaqueCallbackData, res.AttemptID)
	}
}
