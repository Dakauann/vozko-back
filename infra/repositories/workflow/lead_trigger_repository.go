package workflow_repository

import (
	"encoding/json"

	"vozko/domain/workflow"
)

const workspacesWithActiveTriggerSQL = `SELECT DISTINCT w.workspace_id
FROM workflows w
JOIN workflow_versions v ON v.workflow_id = w.id AND v.version = w.published_version
WHERE w.deleted_at IS NULL AND w.status = 'active' AND v.graph @> CAST(? AS jsonb)
ORDER BY w.workspace_id`

type triggerNodeProbe struct {
	Nodes []triggerNodeType `json:"nodes"`
}

type triggerNodeType struct {
	Type workflow.NodeType `json:"type"`
}

func (r *repository) WorkspacesWithActiveTrigger(triggerType workflow.TriggerType) ([]string, error) {
	probe, err := json.Marshal(triggerNodeProbe{Nodes: []triggerNodeType{{Type: workflow.NodeType(triggerType)}}})
	if err != nil {
		return nil, err
	}
	var workspaces []string
	if err := r.db.Raw(workspacesWithActiveTriggerSQL, string(probe)).Scan(&workspaces).Error; err != nil {
		return nil, err
	}
	return workspaces, nil
}
