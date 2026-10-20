package node_executors

import (
	"context"
	"testing"

	wf "vozko/domain/whatsappform"
)

func TestAWorkflowTemplateTellsTheFormSendWhichRunSentIt(t *testing.T) {
	sends := &recordingTemplateSends{}
	run := waMediaRun()
	run.WorkflowID = "wf-1"

	if _, _, err := templateSender(sends, &recordingHistory{}).SendTemplate(context.Background(), run, "t1", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	got := sends.sends[0]
	if got.Origin != wf.OriginWorkflow || got.OriginRef != "wf-1" || got.WorkflowRunID != "run-1" || got.LeadID != "lead-1" || got.EntryID != run.EntryID {
		t.Fatalf("billed send %+v, want the workflow origin, run and lead", got)
	}
}
