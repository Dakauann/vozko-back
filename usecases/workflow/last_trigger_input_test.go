package workflow_usecase

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

type triggerHistoryStub struct {
	recorded *workflow.RecordedTriggerData
	err      error
	asked    workflow.TriggerType
	scope    *conversation.VisibilityScope
}

func (s *triggerHistoryStub) LatestTriggerData(workspaceID, workflowID string, trigger workflow.TriggerType, visibility conversation.VisibilityScope) (*workflow.RecordedTriggerData, error) {
	s.asked, s.scope = trigger, &visibility
	if workspaceID != "ws1" || workflowID != "wf-1" {
		return nil, nil
	}
	return s.recorded, s.err
}

var recordedAt = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)

func TestTheLastRealInputComesBackInTheSpecShapeRedactedWithItsMaskedKeys(t *testing.T) {
	history := &triggerHistoryStub{recorded: &workflow.RecordedTriggerData{RunID: "run-7", StartedAt: recordedAt, Data: map[string]interface{}{
		"input": map[string]interface{}{"cpf": "529.982.247-25", "curso": "ADS"},
		"agent": map[string]interface{}{"id": "a1", "name": "Bia"},
	}}}
	uc := NewLastTriggerInputUseCase(history, seesEverything{}, TriggerInputReaders{})

	got, err := uc.Execute(viewer, multiTriggerWorkflow(), "agente")
	if err != nil {
		t.Fatal(err)
	}
	if history.asked != workflow.TriggerAgentCall {
		t.Fatalf("asked for %s, want the trigger type of the node", history.asked)
	}
	if got.RunID != "run-7" || !got.StartedAt.Equal(recordedAt) || got.Input["curso"] != "ADS" || got.Input["agent.name"] != "Bia" {
		t.Fatalf("got = %+v", got)
	}
	if got.Input["cpf"] == "529.982.247-25" || !reflect.DeepEqual(got.MaskedKeys, []string{"cpf"}) {
		t.Fatalf("input = %v masked = %v, want the CPF masked and named", got.Input, got.MaskedKeys)
	}
}

func TestTheLastRealInputFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		history *triggerHistoryStub
		nodeID  string
		want    error
	}{
		{"no visible run", &triggerHistoryStub{}, "agente", workflow.ErrNoRecentTriggerInput},
		{"not a trigger", &triggerHistoryStub{}, "copia", workflow.ErrTriggerInputNotFound},
		{"unknown node", &triggerHistoryStub{}, "nope", workflow.ErrTriggerInputNotFound},
		{"unreadable trigger source", &triggerHistoryStub{}, "etapa", workflow.ErrTriggerInputUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewLastTriggerInputUseCase(tc.history, seesEverything{}, TriggerInputReaders{})
			if _, err := uc.Execute(viewer, multiTriggerWorkflow(), tc.nodeID); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAViewerWhoseScopeCannotBeReadSeesNoConversationRun(t *testing.T) {
	history := &triggerHistoryStub{}
	uc := NewLastTriggerInputUseCase(history, inboxRule{err: errScopeRead}, TriggerInputReaders{})
	_, _ = uc.Execute(viewer, multiTriggerWorkflow(), "hook")
	if history.scope == nil || !reflect.DeepEqual(*history.scope, conversation.NothingVisible()) {
		t.Fatalf("scope = %+v, want nothing visible", history.scope)
	}
}
