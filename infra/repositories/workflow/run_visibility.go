package workflow_repository

import (
	"vozko/domain/conversation"
	"vozko/domain/workflow"
	conversation_repository "vozko/infra/repositories/conversation"
)

func visibleRunEntries(workspaceID string, visibility conversation.VisibilityScope) (string, []interface{}) {
	clause, args := conversation_repository.VisibleEntrySQL("workflow_runs.entry_id", "workflow_runs.entry_type", workspaceID, visibility)
	if clause == "" {
		return "", nil
	}
	return "(workflow_runs.entry_type IN ('" + workflow.EntryTypeSIPCall + "', '" + workflow.EntryTypeLead + "') OR " + clause + ")", args
}

func visibleDeliveryEntries(workspaceID string, visibility conversation.VisibilityScope) (string, []interface{}) {
	clause, args := conversation_repository.VisibleEntrySQL("CAST(NULLIF(workflow_webhook_deliveries.entry_id, '') AS uuid)", "workflow_webhook_deliveries.entry_type", workspaceID, visibility)
	if clause == "" {
		return "", nil
	}
	return "(COALESCE(workflow_webhook_deliveries.entry_id, '') = '' OR " + clause + ")", args
}

func visibleAlertConversations(alias, workspaceID string, visibility conversation.VisibilityScope) (string, []interface{}) {
	clause, args := conversation_repository.VisibleEntrySQL("CAST(NULLIF("+alias+"sample, '') AS uuid)", alias+"subject", workspaceID, visibility)
	if clause == "" {
		return "", nil
	}
	return "\n  AND (CASE WHEN " + alias + "kind = '" + string(workflow.AlertInboundLookupFailed) + "' THEN " + clause + " ELSE TRUE END)", args
}
