package node_executors

import (
	ce "vozko/domain/conversation_event"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

const stateSendRefusedVisit = "_send_refused_visit"

type refusalLines struct {
	events ce.Logger
}

func (r refusalLines) refuse(run *workflow.WorkflowRun, leadID string, refusal error) error {
	r.writeOnce(run, leadID, refusal)
	return refusedRecipient(leadID, refusal)
}

func (r refusalLines) writeOnce(run *workflow.WorkflowRun, leadID string, refusal error) {
	if r.events == nil || run == nil {
		return
	}
	visit := run.NodeVisit()
	if run.State.GetString(stateSendRefusedVisit) == visit {
		return
	}
	run.State.Set(stateSendRefusedVisit, visit)
	r.events.Log(ce.New(run.WorkspaceID, run.EntryID, run.EntryType, ce.EventLeadSendRefused).
		WithActorWorkflow(run.WorkflowID).
		WithChannel(run.EntryType).
		WithCorrelation(run.ID).
		WithDetails(map[string]string{
			"workflow_id": run.WorkflowID,
			"run_id":      run.ID,
			"node_id":     run.CurrentNodeID,
			"reason":      wo.ErrorCode(refusal),
			"lead_id":     leadID,
		}).
		Build())
}
