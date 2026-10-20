package formtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/copilot"
	wf "vozko/domain/whatsappform"
	"vozko/usecases/copilot/copilottools/tooltest"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

func TestPublishWhatsAppFormChecksTheDraftBeforeApproval(t *testing.T) {
	forms := formsWith(knownDetail())
	tool := NewPublishWhatsAppFormTool(deps(forms))

	if err := tooltest.Validate(t, tool, tooltest.Member(), map[string]interface{}{"form_id": knownForm}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if forms.checked == nil || forms.checked.Screens[0].Key != "s_dados" {
		t.Fatalf("the draft must go through the use case check: %+v", forms.checked)
	}

	forms.problems = wf.Problems{{Screen: "s_interesse", Property: "submitLabel", Code: wf.ProblemRequired}}
	err := tooltest.Validate(t, tool, tooltest.Member(), map[string]interface{}{"form_id": knownForm})
	if err == nil || !strings.Contains(err.Error(), "s_interesse") || forms.called("Publish:"+knownForm) {
		t.Fatalf("err = %v, calls %v", err, forms.calls)
	}
}

func TestPublishWhatsAppFormRefusesAnArchivedFormOrStoredDraftProblems(t *testing.T) {
	archived := knownDetail()
	now := time.Now()
	archived.Form.ArchivedAt = &now
	if err := tooltest.Validate(t, NewPublishWhatsAppFormTool(deps(formsWith(archived))), tooltest.Member(), map[string]interface{}{"form_id": knownForm}); err == nil || !strings.Contains(err.Error(), "arquivado") {
		t.Fatalf("archived err = %v", err)
	}

	broken := knownDetail()
	broken.Draft.Problems = wf.Problems{{Screen: "s_dados", Code: wf.ProblemMetaValidation, Message: "unknown component"}}
	if err := tooltest.Validate(t, NewPublishWhatsAppFormTool(deps(formsWith(broken))), tooltest.Member(), map[string]interface{}{"form_id": knownForm}); err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("broken err = %v", err)
	}
}

func TestPublishWhatsAppFormCardShowsTheAccounts(t *testing.T) {
	forms := formsWith(knownDetail())
	forms.targets = []whatsappform_usecase.PublishTarget{{MetaWABAID: knownWABA, Supported: true}, {MetaWABAID: "waba-2", Supported: true}, {MetaWABAID: "waba-new", Supported: true}, {MetaWABAID: "waba-360"}}
	fields := NewPublishWhatsAppFormTool(deps(forms)).(copilot.Describer).Describe(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if tooltest.FieldValue(fields, "form") != "Captação" || !strings.Contains(tooltest.FieldValue(fields, "draft"), "2") {
		t.Fatalf("fields = %+v", fields)
	}
	destination := tooltest.FieldValue(fields, "destination")
	if !strings.Contains(destination, knownWABA) || !strings.Contains(destination, "waba-2") || !strings.Contains(destination, "publicado") || !strings.Contains(destination, "falhou") ||
		!strings.Contains(destination, "waba-new (primeira publicação)") || !strings.Contains(destination, "waba-360 (provedor sem suporte, fica de fora)") {
		t.Fatalf("destination = %q", destination)
	}
}

func TestPublishWhatsAppFormReturnsEveryAccountOutcome(t *testing.T) {
	forms := formsWith(knownDetail())
	forms.published = whatsappform_usecase.PublishResult{Outcomes: []whatsappform_usecase.PublishOutcome{
		{MetaWABAID: knownWABA, Status: wf.PublicationPublished},
		{MetaWABAID: "waba-2", Status: wf.PublicationFailed, Code: wf.ErrorCode(wf.ErrProviderUnsupported), Error: wf.ErrProviderUnsupported.Error()},
		{MetaWABAID: "waba-3", Status: wf.PublicationFailed, Code: wf.ErrorCode(wf.ErrDocumentInvalid), Problems: wf.Problems{{Screen: "s_dados", Code: wf.ProblemMetaValidation, Message: "bad label"}}},
	}}

	res := NewPublishWhatsAppFormTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if res.Status != copilot.StatusOK || !forms.called("Publish:"+knownForm) {
		t.Fatalf("res = %+v", res)
	}
	data := tooltest.DataOf(res)
	outcomes := data["outcomes"].([]map[string]interface{})
	if len(outcomes) != 3 || outcomes[0]["account"] != knownWABA || outcomes[0]["status"] != "published" {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	if !strings.Contains(outcomes[1]["problem"].(string), "WhatsApp oficial") {
		t.Fatalf("the refusal must be explained in Portuguese: %+v", outcomes[1])
	}
	if !strings.Contains(outcomes[2]["problem"].(string), "bad label") {
		t.Fatalf("Meta problems must be reported: %+v", outcomes[2])
	}
	if data["published_on"] != 1 {
		t.Fatalf("published_on = %v", data["published_on"])
	}
}

func TestPublishWhatsAppFormIsAConfigureChange(t *testing.T) {
	tool := NewPublishWhatsAppFormTool(deps(&fakeForms{}))
	if m := tool.Meta(); !m.Mutating || m.Resource != "whatsapp_forms" || m.Action != "configure" {
		t.Fatalf("meta = %+v", m)
	}
	if !requires(tool, "whatsapp_forms:read", "whatsapp_forms:configure") {
		t.Fatalf("requires %v", requirementKeys(tool))
	}
}

func TestPublishWhatsAppFormExplainsWhenThereIsNoAccount(t *testing.T) {
	forms := formsWith(knownDetail())
	forms.errOn = map[string]error{"Publish:" + knownForm: wf.ErrNoEligibleAccount}

	res := NewPublishWhatsAppFormTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if res.Status == copilot.StatusOK || !strings.Contains(res.Message, "número do WhatsApp oficial") {
		t.Fatalf("res = %+v", res)
	}
}
