package workflow_usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"vozko/domain/workflow"
)

type VariableCatalogReader struct {
	Readers TriggerInputReaders
	Secrets workflow.SecretNameReader
	Catalog func() []workflow.NodeDefinition
}

func (r VariableCatalogReader) At(ctx context.Context, wf *workflow.Workflow, nodeID string) (workflow.VariableCatalog, error) {
	if wf.Graph.FindNode(nodeID) == nil {
		return workflow.VariableCatalog{}, fmt.Errorf("%w: %s", workflow.ErrNodeNotFound, nodeID)
	}
	if r.Secrets == nil {
		return workflow.VariableCatalog{}, fmt.Errorf("%w: no secret reader is wired", workflow.ErrVariablesUnavailable)
	}
	secrets, err := r.Secrets.SecretNames(ctx, wf.WorkspaceID)
	if err != nil {
		slog.Error("workflow.variables.secrets_unreadable", slog.String("workspace_id", wf.WorkspaceID), slog.String("workflow_id", wf.ID), slog.String("node_id", nodeID), slog.String("error", err.Error()))
		return workflow.VariableCatalog{}, fmt.Errorf("%w: %v", workflow.ErrVariablesUnavailable, err)
	}
	var catalog []workflow.NodeDefinition
	if r.Catalog != nil {
		catalog = r.Catalog()
	}
	sources := workflow.VariableSources{
		Specs:   r.Readers.Specs(wf.WorkspaceID, &wf.Graph),
		Forms:   r.formPrompts(wf, nodeID),
		Secrets: secrets,
	}
	return workflow.VariableCatalogAt(&wf.Graph, catalog, sources, nodeID), nil
}

func (r VariableCatalogReader) formPrompts(wf *workflow.Workflow, nodeID string) map[string]workflow.TriggerForm {
	forms := map[string]workflow.TriggerForm{}
	if r.Readers.Forms == nil {
		return forms
	}
	for _, n := range wf.Graph.Nodes {
		if !n.Type.IsFormPrompt() {
			continue
		}
		formID, _ := n.Config[workflow.FormNodeFormIDKey].(string)
		formID = strings.TrimSpace(formID)
		if formID == "" || strings.Contains(formID, "{{") {
			continue
		}
		if _, read := forms[formID]; read {
			continue
		}
		form, err := r.Readers.Forms.TriggerForm(wf.WorkspaceID, formID)
		if err != nil {
			slog.Warn("workflow.variables.form_unreadable", slog.String("workspace_id", wf.WorkspaceID), slog.String("workflow_id", wf.ID), slog.String("node_id", nodeID), slog.String("form_id", formID), slog.String("error", err.Error()))
			continue
		}
		forms[formID] = form
	}
	return forms
}
