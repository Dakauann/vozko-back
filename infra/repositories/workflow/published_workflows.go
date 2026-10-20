package workflow_repository

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"
)

const publishedWorkflowColumns = `w.id, w.workspace_id, w.department_id, w.name, w.description, w.status, w.type, w.trigger_type, w.trigger_config, w.version, w.created_at, w.updated_at, w.published_version`

func publishedWorkflowsSQL(withDraft, activeOnly bool) string {
	columns := publishedWorkflowColumns
	if withDraft {
		columns += ", w.graph"
	}
	where := "WHERE w.workspace_id = ? AND w.deleted_at IS NULL"
	if activeOnly {
		where += " AND w.status = 'active'"
	}
	return `SELECT ` + columns + `, v.type AS published_type, v.graph AS published_graph
FROM workflows w
LEFT JOIN workflow_versions v ON v.workflow_id = w.id AND v.version = w.published_version
` + where + `
ORDER BY w.created_at`
}

type publishedWorkflowRow struct {
	schema.WorkflowSchema
	PublishedType  *string
	PublishedGraph *string
}

func (r *repository) publishedIn(workspaceID string, withDraft, activeOnly bool) ([]workflow.PublishedWorkflow, error) {
	var rows []publishedWorkflowRow
	if err := r.db.Raw(publishedWorkflowsSQL(withDraft, activeOnly), workspaceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workflow.PublishedWorkflow, 0, len(rows))
	for i := range rows {
		draft, err := mapWorkflowToDomain(&rows[i].WorkflowSchema)
		if err != nil {
			return nil, err
		}
		joined, err := rows[i].joinedVersion()
		if err != nil {
			out = append(out, workflow.PublishedWorkflow{Draft: draft, Err: err})
			continue
		}
		published, err := workflow.PublishedOf(joined, draft)
		out = append(out, workflow.PublishedWorkflow{Draft: draft, Published: published, Err: err})
	}
	return out, nil
}

func (row *publishedWorkflowRow) joinedVersion() (workflow.JoinedVersion, error) {
	if row.PublishedVersion == nil || row.PublishedGraph == nil {
		return workflow.JoinedVersion{}, nil
	}
	var graph workflow.Graph
	if err := json.Unmarshal([]byte(*row.PublishedGraph), &graph); err != nil {
		return workflow.JoinedVersion{}, fmt.Errorf("decode workflow %s version %d: %w", row.ID, *row.PublishedVersion, err)
	}
	return workflow.JoinedVersion{Version: &workflow.WorkflowVersion{
		WorkflowID: row.ID, Version: *row.PublishedVersion, Type: workflow.WorkflowType(derefString(row.PublishedType)), Graph: &graph,
	}}, nil
}

func (r *repository) PublishedInWorkspace(workspaceID string) ([]workflow.PublishedWorkflow, error) {
	return r.publishedIn(workspaceID, true, false)
}

var _ workflow.PublishedWorkflows = (*repository)(nil)

func (r *repository) FindActiveByTrigger(workspaceID string, triggerType workflow.TriggerType) ([]*workflow.Workflow, error) {
	pairs, err := r.publishedIn(workspaceID, false, true)
	if err != nil {
		return nil, err
	}
	result := make([]*workflow.Workflow, 0, len(pairs))
	for _, pair := range pairs {
		if pair.Err != nil {
			slog.Error("workflow.trigger.published_version_unreadable", slog.String("workspace_id", workspaceID), slog.String("workflow_id", pair.Draft.ID), slog.String("error", pair.Err.Error()))
			continue
		}
		if pair.Published == nil {
			slog.Warn("workflow.trigger.active_without_published_version", slog.String("workspace_id", workspaceID), slog.String("workflow_id", pair.Draft.ID))
			continue
		}
		if pair.Published.HasTriggerType(triggerType) {
			result = append(result, pair.Published)
		}
	}
	return result, nil
}
