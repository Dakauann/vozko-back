package container

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/stage"
	wf "vozko/domain/whatsappform"
	workflow_domain "vozko/domain/workflow"
)

type fakeTriggerForms struct{ forms map[string]*wf.Form }

func (f fakeTriggerForms) Get(_ context.Context, workspaceID, formID string) (*wf.Form, error) {
	form := f.forms[formID]
	if form == nil || form.WorkspaceID != workspaceID {
		return nil, nil
	}
	return form, nil
}

func (f fakeTriggerForms) List(_ context.Context, q wf.FormQuery) ([]wf.FormSummary, error) {
	var out []wf.FormSummary
	for _, form := range f.forms {
		if form.WorkspaceID == q.WorkspaceID {
			out = append(out, wf.FormSummary{Form: *form})
		}
	}
	return out, nil
}

type fakeTriggerVersions map[string]*wf.Version

func (v fakeTriggerVersions) Get(_ context.Context, _, versionID string) (*wf.Version, error) {
	return v[versionID], nil
}

func TestTheFormCatalogReadsThePublishedQuestionsWithTheKeysProductionUses(t *testing.T) {
	doc := wf.Document{Screens: []wf.Screen{{Blocks: []wf.Block{
		{Field: &wf.Field{Key: "nome", Kind: wf.FieldShortText, Question: "Seu nome", Required: true}},
		{Field: &wf.Field{Key: "turno", Kind: wf.FieldRadio, Label: "Turno", Options: []wf.Option{{ID: "m", Title: "Manhã"}, {ID: "n", Title: "Noite"}}}},
		{Field: &wf.Field{Key: "areas", Kind: wf.FieldCheckbox, Label: "Áreas", Options: []wf.Option{{ID: "a", Title: "TI"}}}},
	}}}}
	catalog := triggerFormCatalog{
		forms:    fakeTriggerForms{forms: map[string]*wf.Form{"f1": {ID: "f1", WorkspaceID: "ws1", Name: "Inscrição", DraftVersionID: "v2", PublishedVersionID: "v1"}}},
		versions: fakeTriggerVersions{"v1": {ID: "v1", Document: doc}, "v2": {ID: "v2"}},
	}
	got, err := catalog.TriggerForm("ws1", "f1")
	if err != nil {
		t.Fatal(err)
	}
	want := workflow_domain.TriggerForm{ID: "f1", Name: "Inscrição", Questions: []workflow_domain.TriggerFormQuestion{
		{Key: "nome", Prompt: "Seu nome", Required: true},
		{Key: "turno", Prompt: "Turno", Choices: []string{"Manhã", "Noite"}},
		{Key: "areas", Prompt: "Áreas"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if _, err := catalog.TriggerForm("ws2", "f1"); !errors.Is(err, wf.ErrFormNotFound) {
		t.Fatalf("another workspace must not read the form, got %v", err)
	}
	choices, err := catalog.TriggerFormChoices("ws1")
	if err != nil || !reflect.DeepEqual(choices, []workflow_domain.TriggerInputOption{{Value: "f1", Label: "Inscrição"}}) {
		t.Fatalf("choices = %+v err = %v", choices, err)
	}
}

type fakeStageList []*stage.Stage

func (s fakeStageList) ListByWorkspace(string) ([]*stage.Stage, error) { return s, nil }

func TestTheStageCatalogOffersOnlyTheWorkspaceStages(t *testing.T) {
	catalog := triggerStageCatalog{stages: fakeStageList{{ID: "s1", WorkspaceID: "ws1", Name: "Novo"}, {ID: "s9", WorkspaceID: "ws2", Name: "Outro"}}}
	got, err := catalog.TriggerStages("ws1")
	if err != nil || !reflect.DeepEqual(got, []workflow_domain.TriggerInputOption{{Value: "s1", Label: "Novo"}}) {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}
