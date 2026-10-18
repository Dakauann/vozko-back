package workflow_usecase

import (
	"errors"
	"testing"

	ce "vozko/domain/conversation_event"
	"vozko/domain/workflow"
)

type silentLines struct{}

func (silentLines) Log(*ce.ConversationEvent) {}

func TestProductionExecutorsRefuseToStartWithoutTheRefusalLineLogger(t *testing.T) {
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{}); !errors.Is(err, ErrConversationEventsRequired) {
		t.Fatalf("got %v, want ErrConversationEventsRequired", err)
	}
	registry := NewNodeExecutorRegistry()
	if err := RegisterProductionExecutors(registry, ExecutorDeps{ConversationEvents: silentLines{}}); err != nil {
		t.Fatalf("got %v", err)
	}
	if _, ok := registry.Get(workflow.NodeTypeActionSendText); !ok {
		t.Fatal("the executors were not registered")
	}
}

func TestSimulationsRecordRefusedSendsNowhereOnPurpose(t *testing.T) {
	if simulationEvents == nil {
		t.Fatal("simulations need an explicit no-op recorder")
	}
	simulationEvents.Log(ce.New("ws", "entry", "whatsapp", ce.EventLeadSendRefused).Build())
}
