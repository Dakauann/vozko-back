package workflow_usecase

import (
	"errors"
	"testing"

	ce "vozko/domain/conversation_event"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type silentLines struct{}

func (silentLines) Log(*ce.ConversationEvent) {}

func TestProductionExecutorsRefuseToStartWithoutTheRefusalLineLogger(t *testing.T) {
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{}); !errors.Is(err, ErrConversationEventsRequired) {
		t.Fatalf("got %v, want ErrConversationEventsRequired", err)
	}
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{ConversationEvents: silentLines{}}); !errors.Is(err, ErrFormSenderRequired) {
		t.Fatalf("got %v, want ErrFormSenderRequired", err)
	}
	registry := NewNodeExecutorRegistry()
	if err := RegisterProductionExecutors(registry, ExecutorDeps{ConversationEvents: silentLines{}, Forms: node_executors.NewFormSenderRelay()}); err != nil {
		t.Fatalf("got %v", err)
	}
	for _, nodeType := range []workflow.NodeType{workflow.NodeTypeActionSendText, workflow.NodeTypeActionSendForm} {
		if _, ok := registry.Get(nodeType); !ok {
			t.Fatalf("%s was not registered", nodeType)
		}
	}
}

func TestSimulationsRecordRefusedSendsNowhereOnPurpose(t *testing.T) {
	if simulationEvents == nil {
		t.Fatal("simulations need an explicit no-op recorder")
	}
	simulationEvents.Log(ce.New("ws", "entry", "whatsapp", ce.EventLeadSendRefused).Build())
}
