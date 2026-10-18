package buttonaction_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

type fakeContentEditor struct {
	edits []template.EditContentInput
	err   error
}

func (f *fakeContentEditor) EditContent(_ context.Context, in template.EditContentInput) (*template.Template, error) {
	f.edits = append(f.edits, in)
	if f.err != nil {
		return nil, f.err
	}
	edited := promoTemplate()
	edited.Components = in.Edit.Components
	edited.Status = template.TemplateStatusPending
	return edited, nil
}

func editedPromo(extra ...template.TemplateButton) template.Edit {
	buttons := append([]template.TemplateButton{{Type: "QUICK_REPLY", Text: "Falar com atendente"}}, extra...)
	return template.Edit{Components: []template.TemplateComponent{
		{Type: "BODY", Text: "Ofertas da semana."},
		{Type: "BUTTONS", Buttons: buttons},
	}}
}

func editingFixture(t *testing.T) (*fixture, *fakeContentEditor) {
	t.Helper()
	f := newFixture(t)
	editor := &fakeContentEditor{}
	f.svc.SetContentEditor(editor)
	return f, editor
}

func TestAnEditThatLeavesAMarketingTemplateWithoutAShownExitNeverReachesMeta(t *testing.T) {
	f, editor := editingFixture(t)

	_, err := f.svc.EditTemplate(ctx, actor(), EditTemplateRequest{TemplateID: templateID, Edit: editedPromo()})

	if !errors.Is(err, buttonaction.ErrMarketingNeedsExit) || len(editor.edits) != 0 || f.settings.saves != 0 {
		t.Fatalf("err = %v, edits %d, saves %d", err, len(editor.edits), f.settings.saves)
	}
}

func TestANewExitButtonAddedInTheEditBecomesTheTemplatesExit(t *testing.T) {
	f, editor := editingFixture(t)
	req := EditTemplateRequest{
		TemplateID: templateID,
		Edit:       editedPromo(template.TemplateButton{Type: "QUICK_REPLY", Text: "Parar"}),
		Actions:    []buttonaction.Input{{ButtonIndex: 1, ButtonText: "Parar", Kind: buttonaction.KindOptOut}},
	}

	edited, err := f.svc.EditTemplate(ctx, actor(), req)

	if err != nil || edited.Status != template.TemplateStatusPending {
		t.Fatalf("edited = %+v, err = %v", edited, err)
	}
	if len(editor.edits) != 1 || editor.edits[0].WorkspaceID != ws || editor.edits[0].UserID != userID || editor.edits[0].TemplateID != templateID {
		t.Fatalf("edits = %+v", editor.edits)
	}
	stored, _ := f.settings.Get(ctx, ws, buttonaction.RefOf(promoTemplate()))
	if len(stored.Actions) != 1 || stored.Actions[0].Kind != buttonaction.KindOptOut || stored.Actions[0].ButtonText != "Parar" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestAMetaRefusalOfTheEditStoresNoActions(t *testing.T) {
	f, editor := editingFixture(t)
	editor.err = template.ErrTemplateInUse
	req := EditTemplateRequest{
		TemplateID: templateID,
		Edit:       editedPromo(template.TemplateButton{Type: "QUICK_REPLY", Text: "Parar"}),
		Actions:    []buttonaction.Input{{ButtonIndex: 1, ButtonText: "Parar", Kind: buttonaction.KindOptOut}},
	}

	if _, err := f.svc.EditTemplate(ctx, actor(), req); !errors.Is(err, template.ErrTemplateInUse) || f.settings.saves != 0 {
		t.Fatalf("err = %v, saves %d", err, f.settings.saves)
	}
}

func TestAnEditThatShowsAWordKeepsTheSavedActions(t *testing.T) {
	f, editor := editingFixture(t)
	edit := editedPromo()
	edit.Components = append(edit.Components, template.TemplateComponent{Type: "FOOTER", Text: "Responda /sair"})

	if _, err := f.svc.EditTemplate(ctx, actor(), EditTemplateRequest{TemplateID: templateID, Edit: edit}); err != nil {
		t.Fatal(err)
	}
	if len(editor.edits) != 1 || f.settings.saves != 0 {
		t.Fatalf("edits %d, saves %d", len(editor.edits), f.settings.saves)
	}
}

func TestEditingWithoutAnEditorFailsClosed(t *testing.T) {
	f := newFixture(t)
	edit := editedPromo()
	edit.Components = append(edit.Components, template.TemplateComponent{Type: "FOOTER", Text: "Responda /sair"})

	if _, err := f.svc.EditTemplate(ctx, actor(), EditTemplateRequest{TemplateID: templateID, Edit: edit}); !errors.Is(err, buttonaction.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestAWorkspaceWithoutTheTemplateCannotEditIt(t *testing.T) {
	f, editor := editingFixture(t)
	stranger := actor()
	stranger.WorkspaceID = "ws-stranger"

	if _, err := f.svc.EditTemplate(ctx, stranger, EditTemplateRequest{TemplateID: templateID, Edit: editedPromo()}); err == nil || len(editor.edits) != 0 {
		t.Fatalf("err = %v, edits %d", err, len(editor.edits))
	}
}
