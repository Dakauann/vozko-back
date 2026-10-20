package conversation_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type workflowNamesSpy struct {
	names map[string]string
	err   error
	calls [][]string
}

func (s *workflowNamesSpy) ResolveNames(actorIDs ...string) (map[string]string, error) {
	s.calls = append(s.calls, actorIDs)
	return s.names, s.err
}

func historyWithWorkflowMessages() []*conversation.Message {
	return []*conversation.Message{
		{Text: "Quero me matricular", MessageType: conversation.MessageTypeUserMessage, From: "5511999990000", SentBy: conversation.SentByContact("5511999990000")},
		{Text: "Qual o seu CPF?", MessageType: conversation.MessageTypeUserMessage, From: "551140001234", SentBy: conversation.SentByWorkflow("wf-1")},
		{Text: "Já vou verificar", MessageType: conversation.MessageTypeAIResponse, From: "551140001234", SentBy: conversation.SentByAI("agent-1")},
		{Text: "Matrícula feita", MessageType: conversation.MessageTypeUserMessage, From: "551140001234", SentBy: conversation.SentByWorkflow("wf-1")},
	}
}

func TestTheOfficialHistoryLabelsWorkflowMessagesWithOneNameLookup(t *testing.T) {
	names := &workflowNamesSpy{names: map[string]string{"workflow:wf-1": "Matrícula"}}
	uc := &handleWhatsAppMessageUseCase{workflowNames: names}

	history := uc.composeConversationHistory(historyWithWorkflowMessages(), "551140001234", "5511999990000")

	var got []string
	for _, m := range history {
		got = append(got, m.Content)
	}
	want := []string{"Quero me matricular", "[fluxo Matrícula] Qual o seu CPF?", "Já vou verificar", "[fluxo Matrícula] Matrícula feita"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %q, want %q", got, want)
	}
	if len(names.calls) != 1 || !reflect.DeepEqual(names.calls[0], []string{"workflow:wf-1"}) {
		t.Fatalf("name lookups = %v, want one batch", names.calls)
	}
}

func TestTheChannelReplyHistoryLabelsWorkflowMessages(t *testing.T) {
	newestFirst := historyWithWorkflowMessages()
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	names := &workflowNamesSpy{names: map[string]string{"workflow:wf-1": "Matrícula"}}
	svc := &ChannelAIReplyService{messages: stubMessageRepo{newestFirst: newestFirst}, workflowNames: names}

	got, err := svc.buildPrompt(conversation.AIReplyRequest{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Content != "[fluxo Matrícula] Qual o seu CPF?" || got[3].Content != "[fluxo Matrícula] Matrícula feita" || got[0].Content != "Quero me matricular" {
		t.Fatalf("prompt = %+v", got)
	}
	if len(names.calls) != 1 {
		t.Fatalf("name lookups = %v, want one batch", names.calls)
	}
}

func TestAnUnreadableWorkflowNameStillMarksTheSource(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{workflowNames: &workflowNamesSpy{err: errors.New("db down")}}
	history := uc.composeConversationHistory(historyWithWorkflowMessages(), "551140001234", "5511999990000")
	if history[1].Content != "[fluxo] Qual o seu CPF?" {
		t.Fatalf("content = %q", history[1].Content)
	}

	unwired := &handleWhatsAppMessageUseCase{}
	if got := unwired.composeConversationHistory(historyWithWorkflowMessages(), "551140001234", "5511999990000"); got[1].Content != "[fluxo] Qual o seu CPF?" {
		t.Fatalf("content = %q", got[1].Content)
	}
}

func TestAHistoryWithoutWorkflowMessagesLooksNothingUp(t *testing.T) {
	names := &workflowNamesSpy{}
	uc := &handleWhatsAppMessageUseCase{workflowNames: names}
	uc.composeConversationHistory(historyWithWorkflowMessages()[:1], "551140001234", "5511999990000")
	if len(names.calls) != 0 {
		t.Fatalf("name lookups = %v", names.calls)
	}
}
