package formtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	wf "vozko/domain/whatsappform"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func documentArg(t *testing.T, doc wf.Document) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCreateWhatsAppFormFromAStarterChecksItsDocumentAndCreates(t *testing.T) {
	forms := &fakeForms{}
	tool := NewCreateWhatsAppFormTool(deps(forms))
	args := map[string]interface{}{"name": "Captação", "description": "Leads do site", "starter": "lead_capture"}

	if err := tooltest.Validate(t, tool, tooltest.Member(), args); err != nil {
		t.Fatalf("validate: %v", err)
	}
	starter, _ := wf.StarterByKey("lead_capture")
	if forms.checked == nil || len(forms.checked.Screens) != len(starter.Document.Screens) {
		t.Fatalf("the starter document must be checked by the use case before approval: %+v", forms.checked)
	}

	res := tool.Execute(context.Background(), tooltest.Member(), args)

	if res.Status != copilot.StatusOK || forms.created.Name != "Captação" || forms.created.Starter != "lead_capture" || forms.created.Description != "Leads do site" {
		t.Fatalf("res = %+v, created %+v", res, forms.created)
	}
	if forms.called("SaveDraft") {
		t.Fatal("a starter form needs no second save")
	}
	if tooltest.DataOf(res)["form_id"] != knownForm {
		t.Fatalf("data = %+v", tooltest.DataOf(res))
	}
}

func TestCreateWhatsAppFormWithAFullDocumentSavesItAsTheDraft(t *testing.T) {
	forms := &fakeForms{}
	doc := choiceDocument()
	doc.Schema = 0
	args := map[string]interface{}{"name": "Captação", "document": documentArg(t, doc)}
	tool := NewCreateWhatsAppFormTool(deps(forms))

	if err := tooltest.Validate(t, tool, tooltest.Member(), args); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if forms.checked.Schema != wf.DocumentSchema || len(forms.checked.Screens) != 2 {
		t.Fatalf("checked = %+v", forms.checked)
	}

	res := tool.Execute(context.Background(), tooltest.Member(), args)

	if res.Status != copilot.StatusOK || forms.created.Starter != "" || forms.savedDoc == nil || forms.savedBase != "v-1" {
		t.Fatalf("res = %+v, created %+v, base %q", res, forms.created, forms.savedBase)
	}
	if forms.savedDoc.Screens[1].Blocks[1].Field.Options[1].Title != "Vendas" || forms.savedDoc.Schema != wf.DocumentSchema {
		t.Fatalf("saved = %+v", forms.savedDoc)
	}
}

func TestCreateWhatsAppFormReportsProblemsInsteadOfAskingForApproval(t *testing.T) {
	forms := &fakeForms{problems: wf.Problems{{Screen: "s_dados", Block: "b_nome", Property: "label", Code: wf.ProblemTooLong, Limit: 20}}}
	args := map[string]interface{}{"name": "Captação", "document": documentArg(t, choiceDocument())}

	err := tooltest.Validate(t, NewCreateWhatsAppFormTool(deps(forms)), tooltest.Member(), args)

	if err == nil || !strings.Contains(err.Error(), "s_dados") || !strings.Contains(err.Error(), "b_nome") || !strings.Contains(err.Error(), "20") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateWhatsAppFormRefusesAmbiguousOrUnknownSources(t *testing.T) {
	tool := NewCreateWhatsAppFormTool(deps(&fakeForms{}))
	cases := map[string]map[string]interface{}{
		"no name":         {"starter": "nps"},
		"unknown starter": {"name": "Pesquisa", "starter": "quiz"},
		"both sources":    {"name": "Pesquisa", "starter": "nps", "document": documentArg(t, choiceDocument())},
		"long name":       {"name": strings.Repeat("a", wf.MaxNameLength+1), "starter": "nps"},
	}
	for name, args := range cases {
		if err := tooltest.Validate(t, tool, tooltest.Member(), args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCreateWhatsAppFormCardShowsTheScreensAndFields(t *testing.T) {
	args := map[string]interface{}{"name": "Captação", "document": documentArg(t, choiceDocument())}

	fields := NewCreateWhatsAppFormTool(deps(&fakeForms{})).(copilot.Describer).Describe(context.Background(), tooltest.Member(), args)

	if tooltest.FieldValue(fields, "name") != "Captação" {
		t.Fatalf("fields = %+v", fields)
	}
	var screens []string
	for _, f := range fields {
		if f.Key == "questions" {
			screens = append(screens, f.Value)
		}
	}
	if len(screens) != 2 {
		t.Fatalf("one line per screen, got %+v", fields)
	}
	if !strings.Contains(screens[0], "Seus dados") || !strings.Contains(screens[0], "Nome") || !strings.Contains(screens[0], "obrigatório") || !strings.Contains(screens[0], "sensível") {
		t.Fatalf("first screen = %q", screens[0])
	}
	if !strings.Contains(screens[1], "O que você procura?") || !strings.Contains(screens[1], "Produtos") {
		t.Fatalf("second screen = %q", screens[1])
	}
}

func TestCreateWhatsAppFormStarterCardNamesTheStarter(t *testing.T) {
	fields := NewCreateWhatsAppFormTool(deps(&fakeForms{})).(copilot.Describer).Describe(context.Background(), tooltest.Member(), map[string]interface{}{"name": "Pesquisa", "starter": "nps"})

	if !strings.Contains(tooltest.FieldValue(fields, "kind"), "Pesquisa de satisfação") {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestCreateWhatsAppFormKeepsTheFormWhenTheDocumentCannotBeSaved(t *testing.T) {
	forms := &fakeForms{errOn: map[string]error{"SaveDraft": wf.ErrStaleDraft}}

	res := NewCreateWhatsAppFormTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"name": "Captação", "document": documentArg(t, choiceDocument())})

	if res.Status == copilot.StatusOK || !strings.Contains(res.Message, knownForm) || !strings.Contains(res.Message, "rascunho") {
		t.Fatalf("res = %+v", res)
	}
}

func TestCreateWhatsAppFormIsABuildChange(t *testing.T) {
	tool := NewCreateWhatsAppFormTool(deps(&fakeForms{}))
	if m := tool.Meta(); !m.Mutating || m.Resource != "whatsapp_forms" || m.Action != "create" {
		t.Fatalf("meta = %+v", m)
	}
	if !requires(tool, "whatsapp_forms:read", "whatsapp_forms:create", "whatsapp_forms:update") {
		t.Fatalf("requires %v", requirementKeys(tool))
	}
}
