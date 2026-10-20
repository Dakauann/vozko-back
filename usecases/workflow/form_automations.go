package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"vozko/domain/label"
	"vozko/domain/pipeline"
	"vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
	opportunity_usecase "vozko/usecases/opportunity"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

var (
	ErrFormAutomationFormRequired    = errors.New("workflow: the form is required")
	ErrFormAutomationCreatorRequired = errors.New("workflow: the person creating the automation is required")
	ErrNoDefaultDealFunnel           = errors.New("workflow: the workspace has no default deal funnel")
	errFormAutomationsIncomplete     = errors.New("workflow: form automations need workflows, the create use case and the deal funnels")
)

const scaffoldNodeSpacing = 320

type FormAutomation = whatsappform_usecase.FormAutomation

var _ whatsappform_usecase.AutomationCatalog = (*FormAutomations)(nil)

type DealPipelineCatalog interface {
	DealPipelines(workspaceID string) ([]*pipeline.Pipeline, error)
}

type FormAutomations struct {
	workflows workflow.PublishedWorkflows
	create    workflow.CreateWorkflowUseCase
	funnels   DealPipelineCatalog
}

func NewFormAutomations(workflows workflow.PublishedWorkflows, create workflow.CreateWorkflowUseCase, funnels DealPipelineCatalog) (*FormAutomations, error) {
	if workflows == nil || create == nil || funnels == nil {
		return nil, errFormAutomationsIncomplete
	}
	return &FormAutomations{workflows: workflows, create: create, funnels: funnels}, nil
}

func (a *FormAutomations) List(_ context.Context, workspaceID, formID string) ([]FormAutomation, error) {
	workspaceID, formID = strings.TrimSpace(workspaceID), strings.TrimSpace(formID)
	if workspaceID == "" {
		return nil, workflow.ErrWorkspaceIDRequired
	}
	if formID == "" {
		return nil, ErrFormAutomationFormRequired
	}
	pairs, err := a.workflows.PublishedInWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	out := []FormAutomation{}
	for _, pair := range pairs {
		w := pair.Draft
		if w == nil || w.WorkspaceID != workspaceID || w.Status == workflow.WorkflowStatusArchived {
			continue
		}
		running, err := runningFormUse(pair, formID)
		if err != nil {
			return nil, err
		}
		if running != "" {
			out = append(out, FormAutomation{WorkflowID: w.ID, Name: w.Name, Active: true, Use: string(running)})
			continue
		}
		if use, ok := w.FormUseOf(formID); ok {
			out = append(out, FormAutomation{WorkflowID: w.ID, Name: w.Name, Active: false, Use: string(use)})
		}
	}
	return out, nil
}

func runningFormUse(pair workflow.PublishedWorkflow, formID string) (workflow.FormUse, error) {
	w := pair.Draft
	if w.Status != workflow.WorkflowStatusActive {
		return "", nil
	}
	if errors.Is(pair.Err, workflow.ErrVersionNotFound) {
		slog.Warn("workflow.form_automations.published_version_missing", slog.String("workspace_id", w.WorkspaceID), slog.String("workflow_id", w.ID), slog.String("error", pair.Err.Error()))
		return "", nil
	}
	if pair.Err != nil {
		return "", fmt.Errorf("published version of workflow %s: %w", w.ID, pair.Err)
	}
	if pair.Published == nil {
		return "", nil
	}
	use, _ := pair.Published.FormUseOf(formID)
	return use, nil
}

func (a *FormAutomations) Scaffold(ctx context.Context, workspaceID, userID, formID, formName string) (string, error) {
	workspaceID, userID, formID, formName = strings.TrimSpace(workspaceID), strings.TrimSpace(userID), strings.TrimSpace(formID), strings.TrimSpace(formName)
	switch {
	case workspaceID == "":
		return "", workflow.ErrWorkspaceIDRequired
	case userID == "":
		return "", ErrFormAutomationCreatorRequired
	case formID == "":
		return "", ErrFormAutomationFormRequired
	}
	funnelID, err := a.defaultDealFunnel(workspaceID)
	if err != nil {
		return "", err
	}
	if _, scoped := workspace_department.GetCreationScope(ctx); !scoped {
		ctx = workspace_department.WithCreationScope(ctx, workspace_department.CreationScope{UserID: userID})
	}
	created, err := a.create.Execute(ctx, &workflow.Workflow{
		WorkspaceID: workspaceID,
		Name:        scaffoldName(formName),
		Status:      workflow.WorkflowStatusDraft,
		Type:        workflow.WorkflowTypeMessages,
		TriggerType: workflow.TriggerFormSubmitted,
		Graph:       scaffoldGraph(formID, formName, funnelID),
	})
	if err != nil {
		return "", err
	}
	if created == nil {
		return "", fmt.Errorf("workflow: the scaffolded automation for form %s was not stored", formID)
	}
	return created.ID, nil
}

func (a *FormAutomations) defaultDealFunnel(workspaceID string) (string, error) {
	funnels, err := a.funnels.DealPipelines(workspaceID)
	if err != nil {
		return "", fmt.Errorf("deal funnels of workspace %s: %w", workspaceID, err)
	}
	for _, p := range funnels {
		if p != nil && p.IsDefault && p.ObjectType == pipeline.ObjectOpportunity {
			return p.ID, nil
		}
	}
	return "", ErrNoDefaultDealFunnel
}

func scaffoldName(formName string) string {
	if formName == "" {
		return "Respostas do formulário"
	}
	return "Respostas do formulário " + formName
}

func scaffoldGraph(formID, formName, funnelID string) workflow.Graph {
	dealTitle := formName
	if dealTitle == "" {
		dealTitle = "Oportunidade do formulário"
	}
	at := func(step int) workflow.Position {
		return workflow.Position{X: float64(step * scaffoldNodeSpacing), Y: 0}
	}
	return workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "form_submitted", Type: workflow.NodeTypeTriggerFormSubmitted, Position: at(0), Config: map[string]interface{}{
				workflow.FormTriggerFormIDsKey: []interface{}{formID},
			}},
			{ID: "add_label", Type: workflow.NodeTypeActionAssignLabel, Position: at(1), Config: map[string]interface{}{
				"action": string(label.LabelActionAdd), "label_id": "",
			}},
			{ID: "create_deal", Type: workflow.NodeTypeActionManageOpportunity, Position: at(2), Config: map[string]interface{}{
				"pipeline_id": funnelID, "action": string(opportunity_usecase.EntryCreate), "stage_id": "", "title": dealTitle, "value": "",
			}},
			{ID: "end", Type: workflow.NodeTypeEnd, Position: at(3), Config: map[string]interface{}{}},
		},
		Edges: []workflow.Edge{
			{Source: "form_submitted", Target: "add_label"},
			{Source: "add_label", Target: "create_deal", Label: "sucesso"},
			{Source: "create_deal", Target: "end", Label: "sucesso"},
		},
	}
}
