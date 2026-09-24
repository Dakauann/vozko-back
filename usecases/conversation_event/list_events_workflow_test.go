package conversation_event_usecase

import (
	"testing"

	ce "vozko/domain/conversation_event"
	"vozko/domain/workflow"
)

const triagem = "44444444-4444-4444-8444-444444444444"

type stubWorkflowNames struct {
	asked []string
}

func (s *stubWorkflowNames) FindByIDs(ids []string) ([]*workflow.Workflow, error) {
	s.asked = append(s.asked, ids...)
	out := []*workflow.Workflow{}
	for _, id := range ids {
		if id == triagem {
			out = append(out, &workflow.Workflow{ID: id, Name: "Triagem"})
		}
	}
	return out, nil
}

func TestExecuteNamesAWorkflowThatHandedOff(t *testing.T) {
	// A workflow transfer node credits the workflow; the timeline must read
	// "Triagem transferiu para ana", not an unnamed actor or an agent.
	uc, _, agents := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "whatsapp", ce.EventAssigned).
			WithActorWorkflow(triagem).
			WithDetails(map[string]string{"from_user_id": "workflow:" + triagem, "to_user_id": ana}).
			Build(),
	})
	names := &stubWorkflowNames{}
	uc.(interface{ SetWorkflowNames(WorkflowNameLookup) }).SetWorkflowNames(names)

	got, _, _ := uc.Execute("ws", "entry", "whatsapp", 50, 0)

	if got[0].ActorName != "Triagem" || got[0].FromName != "Triagem" || got[0].ToName != "ana" {
		t.Fatalf("names = actor %q from %q to %q, want Triagem, Triagem, ana", got[0].ActorName, got[0].FromName, got[0].ToName)
	}
	if agents.calls != 0 {
		t.Fatal("a workflow id must never be looked up as an agent")
	}
	if len(names.asked) != 1 || names.asked[0] != triagem {
		t.Fatalf("workflows asked for %v, want the bare id once", names.asked)
	}
}

func TestExecuteWithoutWorkflowNamesLeavesTheWorkflowUnnamed(t *testing.T) {
	uc, _, _ := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "whatsapp", ce.EventAssigned).WithActorWorkflow(triagem).Build(),
	})

	got, _, _ := uc.Execute("ws", "entry", "whatsapp", 50, 0)

	if got[0].ActorName != "" {
		t.Fatalf("ActorName = %q, want empty", got[0].ActorName)
	}
}
