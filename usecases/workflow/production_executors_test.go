package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type silentLines struct{}

type nothingStoredHere struct{}

func (nothingStoredHere) OwnsURL(string) bool { return false }

func externalMediaURLs() *conversation.MediaURLResolver {
	return &conversation.MediaURLResolver{Storage: nothingStoredHere{}}
}

func (silentLines) Log(*ce.ConversationEvent) {}

func TestProductionExecutorsRefuseToStartWithoutTheRefusalLineLogger(t *testing.T) {
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{}); !errors.Is(err, ErrConversationEventsRequired) {
		t.Fatalf("got %v, want ErrConversationEventsRequired", err)
	}
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{ConversationEvents: silentLines{}}); !errors.Is(err, ErrFormSenderRequired) {
		t.Fatalf("got %v, want ErrFormSenderRequired", err)
	}
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{ConversationEvents: silentLines{}, Forms: node_executors.NewFormSenderRelay(), ChoiceStats: &spyChoiceRecorder{}}); !errors.Is(err, ErrMediaURLsRequired) {
		t.Fatalf("got %v, want ErrMediaURLsRequired", err)
	}
	registry := NewNodeExecutorRegistry()
	if err := RegisterProductionExecutors(registry, ExecutorDeps{ConversationEvents: silentLines{}, Forms: node_executors.NewFormSenderRelay(), ChoiceStats: &spyChoiceRecorder{}, MediaURLs: externalMediaURLs()}); err != nil {
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

func TestCarrierNodeExistsOnlyWhileAChannelIsLaunched(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	if err := RegisterProductionExecutors(registry, ExecutorDeps{ConversationEvents: silentLines{}, Forms: node_executors.NewFormSenderRelay(), ChoiceStats: &spyChoiceRecorder{}, MediaURLs: externalMediaURLs()}); err != nil {
		t.Fatal(err)
	}
	_, registered := registry.Get(workflow.NodeTypeActionSendCarrier)
	if launched := len(carriermessaging.LaunchedChannels()) > 0; registered != launched {
		t.Fatalf("carrier node registered = %v while a channel launched is %v", registered, launched)
	}
}
