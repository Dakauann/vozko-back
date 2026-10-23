package container

import (
	"context"
	"fmt"

	"vozko/domain/stage"
	wf "vozko/domain/whatsappform"
	workflow_domain "vozko/domain/workflow"
	"vozko/domain/workflowsecret"
	whatsappform_repository "vozko/infra/repositories/whatsappform"
	workflow_repository "vozko/infra/repositories/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

type triggerFormReads interface {
	Get(ctx context.Context, workspaceID, formID string) (*wf.Form, error)
	List(ctx context.Context, q wf.FormQuery) ([]wf.FormSummary, error)
}

type triggerFormVersions interface {
	Get(ctx context.Context, workspaceID, versionID string) (*wf.Version, error)
}

type triggerFormCatalog struct {
	forms    triggerFormReads
	versions triggerFormVersions
}

func (c triggerFormCatalog) TriggerFormChoices(workspaceID string) ([]workflow_domain.TriggerInputOption, error) {
	summaries, err := c.forms.List(context.Background(), wf.FormQuery{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	choices := make([]workflow_domain.TriggerInputOption, 0, len(summaries))
	for _, s := range summaries {
		choices = append(choices, workflow_domain.TriggerInputOption{Value: s.Form.ID, Label: s.Form.Name})
	}
	return choices, nil
}

func (c triggerFormCatalog) TriggerForm(workspaceID, formID string) (workflow_domain.TriggerForm, error) {
	ctx := context.Background()
	form, err := c.forms.Get(ctx, workspaceID, formID)
	if err != nil {
		return workflow_domain.TriggerForm{}, err
	}
	if form == nil || form.WorkspaceID != workspaceID {
		return workflow_domain.TriggerForm{}, fmt.Errorf("%w: %s", wf.ErrFormNotFound, formID)
	}
	versionID := form.PublishedVersionID
	if versionID == "" {
		versionID = form.DraftVersionID
	}
	version, err := c.versions.Get(ctx, workspaceID, versionID)
	if err != nil {
		return workflow_domain.TriggerForm{}, err
	}
	if version == nil {
		return workflow_domain.TriggerForm{}, fmt.Errorf("%w: version %s of form %s", wf.ErrFormNotFound, versionID, formID)
	}
	out := workflow_domain.TriggerForm{ID: form.ID, Name: form.Name}
	for _, ref := range version.Document.Fields() {
		question := workflow_domain.TriggerFormQuestion{Key: ref.Field.Key, Prompt: ref.Field.Prompt(), Required: ref.Field.Required}
		if ref.Field.Kind.IsSingleChoice() {
			for _, option := range ref.Field.Options {
				question.Choices = append(question.Choices, option.Title)
			}
		}
		out.Questions = append(out.Questions, question)
	}
	return out, nil
}

type triggerStageCatalog struct {
	stages interface {
		ListByWorkspace(workspaceID string) ([]*stage.Stage, error)
	}
}

func (c triggerStageCatalog) TriggerStages(workspaceID string) ([]workflow_domain.TriggerInputOption, error) {
	stages, err := c.stages.ListByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	options := make([]workflow_domain.TriggerInputOption, 0, len(stages))
	for _, s := range stages {
		if s != nil && s.WorkspaceID == workspaceID {
			options = append(options, workflow_domain.TriggerInputOption{Value: s.ID, Label: s.Name})
		}
	}
	return options, nil
}

func (c *Container) workflowTriggerInputs() workflow_usecase.TriggerInputReaders {
	return workflow_usecase.TriggerInputReaders{
		Forms:  triggerFormCatalog{forms: whatsappform_repository.NewFormStore(c.db), versions: whatsappform_repository.NewVersionStore(c.db)},
		Stages: triggerStageCatalog{stages: c.repositories.stage},
	}
}

func (c *Container) lastTriggerInput() *workflow_usecase.LastTriggerInputUseCase {
	return workflow_usecase.NewLastTriggerInputUseCase(workflow_repository.NewTriggerInputHistory(c.db), c.conversationVisibility(), c.workflowTriggerInputs())
}

type workflowSecretNames struct {
	secrets workflowsecret.UseCases
}

func (s workflowSecretNames) SecretNames(ctx context.Context, workspaceID string) ([]string, error) {
	if s.secrets == nil {
		return nil, fmt.Errorf("the workflow secrets are not wired")
	}
	summaries, err := s.secrets.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		names = append(names, summary.Name)
	}
	return names, nil
}

func (c *Container) workflowVariables() *workflow_usecase.VariableCatalogReader {
	return &workflow_usecase.VariableCatalogReader{
		Readers: c.workflowTriggerInputs(),
		Secrets: workflowSecretNames{secrets: c.useCases.workflowSecrets},
		Catalog: c.useCases.nodeCatalogFn,
	}
}
