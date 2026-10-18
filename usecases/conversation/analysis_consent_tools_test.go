package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	toolsdomain "vozko/domain/tools"
	lead_usecase "vozko/usecases/lead"
	tools_usecase "vozko/usecases/tools"
)

type neverRecords struct{ calls int }

func (n *neverRecords) Record(context.Context, string, string, lead.ConsentChange) (lead_usecase.ConsentOutcome, error) {
	n.calls++
	return lead_usecase.ConsentOutcome{}, nil
}

type noMessages struct{}

func (noMessages) GetByID(string) (*conversation.Message, error) {
	return nil, conversation.ErrMessageNotFound
}

func TestTheQuietConversationAnalysisNeverRecordsAnOptOut(t *testing.T) {
	optOut, err := tools_usecase.NewRecordOptOutTool(&neverRecords{}, noMessages{})
	if err != nil {
		t.Fatal(err)
	}
	consent, err := tools_usecase.NewRecordConsentTool(&neverRecords{}, noMessages{})
	if err != nil {
		t.Fatal(err)
	}
	subject := instagramSubject()
	job, recorder := dealJob(subject, &dealSettingsStub{pipeline: "deals"})
	job.toolRegistry = registryStub{handlers: map[string]toolsdomain.Handler{
		tools_usecase.AutoManageOpportunityToolName: tools_usecase.NewAutoManageOpportunityTool(dealDeskStub{}, ownerStub{}),
		tools_usecase.RecordOptOutToolName:          optOut,
		tools_usecase.RecordConsentToolName:         consent,
	}}

	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	if len(recorder.inputs) == 0 {
		t.Fatal("the analysis must still run its own tasks")
	}
	for _, input := range recorder.inputs {
		for _, def := range input.Tools {
			if def.Name == tools_usecase.RecordOptOutToolName || def.Name == tools_usecase.RecordConsentToolName {
				t.Fatalf("the analysis offered %s; consent is recorded only in a live agent turn", def.Name)
			}
		}
	}
}
