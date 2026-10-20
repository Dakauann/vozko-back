package node_executors

import (
	"log/slog"

	"vozko/domain/workflow"
)

func RunLogAttrs(run *workflow.WorkflowRun, nodeID string) []any {
	runID, workspaceID := "", ""
	if run != nil {
		runID, workspaceID = run.ID, run.WorkspaceID
	}
	return []any{
		slog.String("run_id", runID),
		slog.String("node_id", nodeID),
		slog.String("workspace_id", workspaceID),
	}
}

func nodeLogAttrs(ctx *workflow.NodeContext) []any {
	nodeID := ""
	if ctx.Node != nil {
		nodeID = ctx.Node.ID
	}
	return RunLogAttrs(ctx.Run, nodeID)
}
