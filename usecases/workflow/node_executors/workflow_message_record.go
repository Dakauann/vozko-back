package node_executors

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

func recordWorkflowMessage(ctx context.Context, history conversation.MessageHistoryManager, run *workflow.WorkflowRun, record conversation.MessageHistoryRecord) error {
	record.SentBy = conversation.SentByWorkflow(run.WorkflowID)
	return history.Record(ctx, record)
}
