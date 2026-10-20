package whatsappform_usecase

import (
	"errors"
	"testing"

	wf "vozko/domain/whatsappform"
)

func builder(t *testing.T) *world {
	return newWorld(t, wf.CapabilityView, wf.CapabilityBuild, wf.CapabilityPublish, wf.CapabilityDelete)
}

func (w *world) create(name, starter string) FormDetail {
	w.t.Helper()
	detail, err := w.svc.Create(ctx, w.viewer, CreateInput{Name: name, Starter: starter})
	if err != nil {
		w.t.Fatalf("create %q: %v", name, err)
	}
	return detail
}

func TestCreatingAFormNeedsTheBuildCapability(t *testing.T) {
	w := newWorld(t, wf.CapabilityView)
	if _, err := w.svc.Create(ctx, w.viewer, CreateInput{Name: "Cadastro"}); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.svc.Create(ctx, Actor{UserID: member}, CreateInput{Name: "Cadastro"}); !errors.Is(err, wf.ErrWorkspaceRequired) {
		t.Fatalf("an actor without a workspace is refused, got %v", err)
	}
}

func TestCreateFromAStarterSavesACompiledFirstVersion(t *testing.T) {
	w := builder(t)
	detail := w.create("  Captação  de leads ", "lead_capture")
	if detail.Form.Name != "Captação de leads" || detail.Form.Kind != wf.KindBuilt || detail.Form.CreatedBy != member {
		t.Fatalf("form = %+v", detail.Form)
	}
	if detail.Draft.Number != 1 || !detail.Draft.Compiled() || detail.Draft.FlowVersion != wf.FlowJSONVersion || len(detail.Draft.Document.Screens) != 2 {
		t.Fatalf("draft = %+v", detail.Draft)
	}
	if detail.Form.DraftVersionID != detail.Draft.ID {
		t.Fatal("the form points at its first draft")
	}
}

func TestCreateRefusals(t *testing.T) {
	w := builder(t)
	w.create("Cadastro", "")
	cases := []struct {
		in   CreateInput
		want error
	}{
		{CreateInput{Name: " "}, wf.ErrNameRequired},
		{CreateInput{Name: "Cadastro"}, wf.ErrNameTaken},
		{CreateInput{Name: "Outro", Starter: "nope"}, wf.ErrStarterUnknown},
	}
	for _, tc := range cases {
		if _, err := w.svc.Create(ctx, w.viewer, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%+v: err = %v, want %v", tc.in, err, tc.want)
		}
	}
}

func TestSaveDraftKeepsEveryVersionAndRefusesAStaleBase(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	doc := detail.Draft.Document
	doc.Screens[0].Title = "Seus dados"
	saved, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Number != 2 || saved.ID == detail.Draft.ID || saved.Document.Screens[0].Title != "Seus dados" || !saved.Compiled() {
		t.Fatalf("saved = %+v", saved)
	}
	if _, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID); !errors.Is(err, wf.ErrStaleDraft) {
		t.Fatalf("saving over a draft someone else replaced must be refused, got %v", err)
	}
	same, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, saved.ID)
	if err != nil || same.ID != saved.ID {
		t.Fatalf("an unchanged document keeps the current draft, got %+v %v", same, err)
	}
}

func TestAnInvalidDraftIsSavedWithItsProblems(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	doc := detail.Draft.Document
	doc.Screens[0].Blocks[0].Field.Label = ""
	saved, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID)
	if err != nil {
		t.Fatalf("problems never block a save: %v", err)
	}
	if saved.Compiled() || len(saved.Problems) == 0 || saved.Problems[0].Code != wf.ProblemRequired {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestCheckUsesTheWorkspaceCustomFields(t *testing.T) {
	w := builder(t)
	doc := wf.BlankDocument()
	doc.Screens[0].Blocks[0].Field.Binding = &wf.Binding{Target: wf.BindCustomField, CustomFieldKey: "cpf"}
	problems, err := w.svc.Check(ctx, w.viewer, doc)
	if err != nil || len(problems) != 1 || problems[0].Code != wf.ProblemBindingSensitive {
		t.Fatalf("problems = %+v, %v", problems, err)
	}
}

func TestOptionsLeaveSensitiveCustomFieldsOut(t *testing.T) {
	w := builder(t)
	options, err := w.svc.Options(ctx, w.viewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.CustomFields) != 1 || options.CustomFields[0].Key != "plano" || len(options.CustomFields[0].FieldKinds) == 0 {
		t.Fatalf("custom fields = %+v", options.CustomFields)
	}
	if len(options.Starters) != 7 || options.FlowJSONVersion != wf.FlowJSONVersion || options.Limits.SubmitLabel != wf.MaxFooterLabel {
		t.Fatalf("options = %+v", options)
	}
}

func TestReadingFormsOfAnotherWorkspaceIsNotFound(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.forms.mu.Lock()
	f := w.forms.forms[detail.Form.ID]
	f.WorkspaceID = otherWS
	w.forms.forms[detail.Form.ID] = f
	w.forms.mu.Unlock()
	if _, err := w.svc.Get(ctx, w.viewer, detail.Form.ID); !errors.Is(err, wf.ErrFormNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDuplicateCopiesTheDraftUnderANewName(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "sign_up")
	copied, err := w.svc.Duplicate(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Form.ID == detail.Form.ID || copied.Form.Name != "Cadastro (cópia)" || copied.Draft.Hash != detail.Draft.Hash {
		t.Fatalf("copy = %+v", copied.Form)
	}
	again, err := w.svc.Duplicate(ctx, w.viewer, detail.Form.ID)
	if err != nil || again.Form.Name != "Cadastro (cópia 2)" {
		t.Fatalf("a second copy gets the next free name, got %+v %v", again.Form.Name, err)
	}
}

func TestRename(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	form, err := w.svc.Rename(ctx, w.viewer, detail.Form.ID, " Novo  nome ", "Descrição")
	if err != nil || form.Name != "Novo nome" || form.Description != "Descrição" {
		t.Fatalf("form = %+v %v", form, err)
	}
}

func TestArchivedFormsRefuseDrafts(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if err := w.svc.Archive(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, detail.Draft.Document, detail.Draft.ID); !errors.Is(err, wf.ErrFormArchived) {
		t.Fatalf("err = %v", err)
	}
}
