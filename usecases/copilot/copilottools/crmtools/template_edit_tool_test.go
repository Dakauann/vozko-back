package crmtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/copilot"
	tmpl "vozko/domain/whatsapp/template"
	buttonaction_usecase "vozko/usecases/buttonaction"
	"vozko/usecases/copilot/copilottools/tooltest"
)

const editedTemplateID = "3d4e5f6a-7b8c-4d9e-8f0a-1b2c3d4e5f6a"

type fakeWorkspaceTemplate struct {
	template *tmpl.Template
}

func (f fakeWorkspaceTemplate) Get(_, templateID string) (*tmpl.Template, error) {
	if f.template == nil || f.template.ID != templateID {
		return nil, tmpl.ErrTemplateAccessDenied
	}
	copied := *f.template
	return &copied, nil
}

type fakeTemplateEditor struct {
	actor buttonaction_usecase.Actor
	req   buttonaction_usecase.EditTemplateRequest
	calls int
	err   error
}

func (f *fakeTemplateEditor) EditTemplate(_ context.Context, a buttonaction_usecase.Actor, req buttonaction_usecase.EditTemplateRequest) (*tmpl.Template, error) {
	f.actor, f.req = a, req
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &tmpl.Template{ID: req.TemplateID, Name: "promo", Status: tmpl.TemplateStatusPending}, nil
}

func storedPromo(status tmpl.TemplateStatus, category tmpl.TemplateCategory) *tmpl.Template {
	return &tmpl.Template{
		ID: editedTemplateID, Name: "promo", Language: "pt_BR", Status: status, Category: category,
		Components: []tmpl.TemplateComponent{
			{Type: "HEADER", Format: "IMAGE", Example: &tmpl.TemplateExample{HeaderHandle: []string{"https://scontent.example/old.jpg"}}},
			{Type: "BODY", Text: "Oi"},
		},
	}
}

func editDeps(stored *tmpl.Template, editor *fakeTemplateEditor) TemplateEditDeps {
	return TemplateEditDeps{Templates: fakeWorkspaceTemplate{template: stored}, Editor: editor, Media: bannerMedia}
}

func editArgs(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{"template_id": editedTemplateID, "body": "Olá {{1}}, novidade.", "body_examples": []interface{}{"Ana"}, "footer": "Responda /sair para não receber mais"}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestEditTemplateIsAnUpdateThatNeedsApproval(t *testing.T) {
	m := NewEditTemplateTool(editDeps(storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryMarketing), &fakeTemplateEditor{})).Meta()
	if m.Resource != "whatsapp_templates" || m.Action != "update" || !m.Mutating {
		t.Fatalf("meta = %+v", m)
	}
}

func TestEditTemplateKeepsTheCurrentMediaHeaderAndSendsTheWholeNewVersion(t *testing.T) {
	editor := &fakeTemplateEditor{}

	res := NewEditTemplateTool(editDeps(storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryMarketing), editor)).Execute(context.Background(), tooltest.Member(), editArgs(nil))

	if res.Status != copilot.StatusOK || editor.calls != 1 || editor.req.TemplateID != editedTemplateID {
		t.Fatalf("res = %+v, calls %d", res, editor.calls)
	}
	components := editor.req.Edit.Components
	if len(components) != 3 || components[0].Type != "HEADER" || components[0].Format != "IMAGE" || components[1].Text != "Olá {{1}}, novidade." || components[2].Type != "FOOTER" {
		t.Fatalf("components = %+v", components)
	}
	if components[0].Example != nil || editor.req.Edit.HeaderMediaURL != nil {
		t.Fatalf("a kept media header is uploaded again by the server, never resent as a CDN link: %+v", components[0])
	}
	if editor.actor.WorkspaceID != tooltest.Member().WorkspaceID {
		t.Fatalf("actor = %+v", editor.actor)
	}
}

func TestEditTemplateCanReplaceOrRemoveTheHeader(t *testing.T) {
	editor := &fakeTemplateEditor{}
	tool := NewEditTemplateTool(editDeps(storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryMarketing), editor))

	tool.Execute(context.Background(), tooltest.Member(), editArgs(map[string]interface{}{"header_media_id": knownBanner}))
	if editor.req.Edit.HeaderMediaURL == nil || *editor.req.Edit.HeaderMediaURL != "https://files.test/ws-1/banner.jpg" {
		t.Fatalf("new media = %+v", editor.req.Edit)
	}

	tool.Execute(context.Background(), tooltest.Member(), editArgs(map[string]interface{}{"remove_header": true}))
	if editor.req.Edit.Components[0].Type == "HEADER" {
		t.Fatalf("removed header = %+v", editor.req.Edit.Components)
	}
}

func TestEditTemplateMakesAQuickReplyTheExitButton(t *testing.T) {
	editor := &fakeTemplateEditor{}
	args := editArgs(map[string]interface{}{"footer": "", "quick_replies": []interface{}{"Quero", "Parar"}, "exit_button": "Parar", "exit_confirmation": "Pronto."})

	res := NewEditTemplateTool(editDeps(storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryMarketing), editor)).Execute(context.Background(), tooltest.Member(), args)

	if res.Status != copilot.StatusOK || len(editor.req.Actions) != 1 || editor.req.Actions[0].ButtonIndex != 1 || editor.req.Actions[0].Kind != buttonaction.KindOptOut || editor.req.ExitConfirmation != "Pronto." {
		t.Fatalf("res = %+v, req = %+v", res, editor.req)
	}
}

func TestEditTemplateRefusesWhatCannotBeEditedBeforeTheApproval(t *testing.T) {
	cases := map[string]*tmpl.Template{
		"authentication": storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryAuthentication),
		"in review":      storedPromo(tmpl.TemplateStatusPending, tmpl.TemplateCategoryMarketing),
		"unknown":        nil,
	}
	for name, stored := range cases {
		err := NewEditTemplateTool(editDeps(stored, &fakeTemplateEditor{})).(copilot.Validator).Validate(context.Background(), tooltest.Member(), editArgs(nil))
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bad := editArgs(map[string]interface{}{"quick_replies": []interface{}{"Quero"}, "exit_button": "Parar"})
	if err := NewEditTemplateTool(editDeps(storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryMarketing), &fakeTemplateEditor{})).(copilot.Validator).Validate(context.Background(), tooltest.Member(), bad); err == nil {
		t.Error("an exit button must be one of the quick replies")
	}
}

func TestEditTemplateExplainsEveryRefusal(t *testing.T) {
	cases := map[error]string{
		tmpl.ErrTemplateInUse: "campanha",
		tmpl.ErrEditNotOwner:  "número",
		&tmpl.EditWindowError{NextAt: time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)}: "2026-10-19",
		buttonaction.ErrMarketingNeedsExit:                                            "/sair",
		tmpl.ErrEditUnsupported:                                                       "tela",
	}
	for err, want := range cases {
		res := NewEditTemplateTool(editDeps(storedPromo(tmpl.TemplateStatusApproved, tmpl.TemplateCategoryMarketing), &fakeTemplateEditor{err: err})).Execute(context.Background(), tooltest.Member(), editArgs(nil))
		if res.Status == copilot.StatusOK || !strings.Contains(res.Message, want) {
			t.Errorf("%v: %+v", err, res)
		}
	}
}

func TestCreateTemplateMakesAQuickReplyTheExitButton(t *testing.T) {
	creator := &fakeTemplateCreator{}
	args := newTemplateArgs()
	args["footer"] = ""
	args["quick_replies"] = []interface{}{"Quero reativar", "Não quero receber"}
	args["exit_button"] = "Não quero receber"
	args["exit_confirmation"] = "Pronto, você saiu."
	tool := NewCreateTemplateTool(templateCreateDeps(creator))

	if err := tool.(copilot.Validator).Validate(context.Background(), tooltest.Member(), args); err != nil {
		t.Fatalf("an exit button counts as the marketing exit: %v", err)
	}
	res := tool.Execute(context.Background(), tooltest.Member(), args)

	if res.Status != copilot.StatusOK || len(creator.req.Actions) != 1 || creator.req.Actions[0].ButtonIndex != 1 || creator.req.Actions[0].Kind != buttonaction.KindOptOut || creator.req.ExitConfirmation != "Pronto, você saiu." {
		t.Fatalf("res = %+v, req = %+v", res, creator.req)
	}
}
