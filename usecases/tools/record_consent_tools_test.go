package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/tools"
	lead_usecase "vozko/usecases/lead"
)

type consentRecorderStub struct {
	calls   []recordedConsent
	outcome lead_usecase.ConsentOutcome
	err     error
}

type recordedConsent struct {
	workspaceID, leadID string
	change              lead.ConsentChange
}

func (s *consentRecorderStub) Record(_ context.Context, workspaceID, leadID string, change lead.ConsentChange) (lead_usecase.ConsentOutcome, error) {
	s.calls = append(s.calls, recordedConsent{workspaceID: workspaceID, leadID: leadID, change: change})
	if s.err != nil {
		return lead_usecase.ConsentOutcome{}, s.err
	}
	return s.outcome, nil
}

type citedMessagesStub struct {
	byID    map[string]*conversation.Message
	err     error
	lookups []string
}

func (s *citedMessagesStub) GetByID(id string) (*conversation.Message, error) {
	s.lookups = append(s.lookups, id)
	if s.err != nil {
		return nil, s.err
	}
	if m, ok := s.byID[id]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

var consentAskedAt = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func conversationMessages() *citedMessagesStub {
	inEntry := func(id, entry string, sentBy conversation.SentBy, text string, at time.Time) *conversation.Message {
		return &conversation.Message{ID: id, EntryID: entry, EntryType: shared.EntryTypeWhatsApp, Text: text, SentBy: sentBy, CreatedAt: at}
	}
	lead := conversation.SentByContact("5511999990000")
	return &citedMessagesStub{byID: map[string]*conversation.Message{
		"m-stop":     inEntry("m-stop", "entry-1", lead, "não quero mais receber isso", consentAskedAt.Add(2*time.Minute)),
		"m-question": inEntry("m-question", "entry-1", conversation.SentByAI("agent-1"), "Posso te mandar novidades por aqui?", consentAskedAt),
		"m-yes":      inEntry("m-yes", "entry-1", lead, "pode sim", consentAskedAt.Add(time.Minute)),
		"m-early":    inEntry("m-early", "entry-1", lead, "oi", consentAskedAt.Add(-time.Minute)),
		"m-other":    inEntry("m-other", "entry-2", conversation.SentByContact("5511888880000"), "pare de me mandar mensagem", consentAskedAt),
		"m-ours":     inEntry("m-ours", "entry-1", conversation.SentByAI("agent-1"), "Você quer parar?", consentAskedAt),
	}}
}

func agentTurnConfig() map[string]interface{} {
	return map[string]interface{}{
		"__workspace_id": "ws-1", "__lead_id": "lead-1", "__agent_id": "agent-1",
		"__entry_id": "entry-1", "__entry_type": string(shared.EntryTypeWhatsApp),
	}
}

func optOutTool(t *testing.T, recorder ConsentRecorder, messages CitedMessages) tools.Handler {
	t.Helper()
	handler, err := NewRecordOptOutTool(recorder, messages)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func consentTool(t *testing.T, recorder ConsentRecorder, messages CitedMessages) tools.Handler {
	t.Helper()
	handler, err := NewRecordConsentTool(recorder, messages)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func runConsentTool(t *testing.T, handler tools.Handler, config, params map[string]interface{}) tools.ExecutionResult {
	t.Helper()
	result, err := handler.ExecuteWithConfig(context.Background(), config, params)
	if err != nil {
		t.Fatalf("refusals are answered as results, got %v", err)
	}
	return result
}

func TestConsentToolsNeedTheirCommandsAndTheMessageReader(t *testing.T) {
	if _, err := NewRecordOptOutTool(nil, conversationMessages()); err == nil {
		t.Fatal("record_opt_out without the consent commands must not be built")
	}
	if _, err := NewRecordOptOutTool(&consentRecorderStub{}, nil); err == nil {
		t.Fatal("record_opt_out without the message reader must not be built")
	}
	if _, err := NewRecordConsentTool(nil, conversationMessages()); err == nil {
		t.Fatal("record_consent without the consent commands must not be built")
	}
	if _, err := NewRecordConsentTool(&consentRecorderStub{}, nil); err == nil {
		t.Fatal("record_consent without the message reader must not be built")
	}
}

func TestConsentToolsAreMessagingActions(t *testing.T) {
	for _, def := range []tools.Definition{
		optOutTool(t, &consentRecorderStub{}, conversationMessages()).Definition(),
		consentTool(t, &consentRecorderStub{}, conversationMessages()).Definition(),
	} {
		if !def.IsVisibleIn(tools.VisibilityMessaging) || def.IsVisibleIn(tools.VisibilityAnalysis) || def.IsVisibleIn(tools.VisibilityPostConversation) {
			t.Errorf("%s visibility = %v, want messaging only", def.Name, def.Visibility)
		}
		if def.Category != tools.CategoryAgentAction {
			t.Errorf("%s category = %s", def.Name, def.Category)
		}
		if !containsString(def.Required, "message_id") {
			t.Errorf("%s must require message_id, got %v", def.Name, def.Required)
		}
	}
	if RecordOptOutToolName != "record_opt_out" || RecordConsentToolName != "record_consent" {
		t.Fatal("the tool names are part of the agent bindings")
	}
}

func TestRecordConsentIsConfiguredWithAnOptionalPurpose(t *testing.T) {
	def := consentTool(t, &consentRecorderStub{}, conversationMessages()).Definition()
	if err := def.ValidateConfig(map[string]interface{}{"purpose": "novidades da loja"}); err != nil {
		t.Fatalf("a purpose is valid config: %v", err)
	}
	if err := def.ValidateConfig(nil); err != nil {
		t.Fatalf("the purpose is optional: %v", err)
	}
	if !containsString(def.Required, "question_message_id") {
		t.Fatalf("the agent's own question must be cited, required = %v", def.Required)
	}
	if err := optOutTool(t, &consentRecorderStub{}, conversationMessages()).Definition().ValidateConfig(map[string]interface{}{"purpose": "x"}); err == nil {
		t.Fatal("record_opt_out takes no configuration")
	}
}

func TestRecordOptOutWithdrawsWithTheLeadsCitedMessage(t *testing.T) {
	recorder := &consentRecorderStub{outcome: lead_usecase.ConsentOutcome{Changed: true, EventID: "ev-1"}}
	result := runConsentTool(t, optOutTool(t, recorder, conversationMessages()), agentTurnConfig(), map[string]interface{}{"message_id": " m-stop "})

	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("record calls = %d", len(recorder.calls))
	}
	call := recorder.calls[0]
	want := lead.ConsentChange{
		Kind: lead.ConsentWithdraw, Source: lead.ConsentAIAgent,
		Actor:    lead.ConsentActor{Type: lead.ConsentActorAgent, ID: "agent-1"},
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-stop", Excerpt: "não quero mais receber isso"},
	}
	if call.workspaceID != "ws-1" || call.leadID != "lead-1" || call.change != want {
		t.Fatalf("recorded %+v", call)
	}
	if result.ContextUpdateText == "" {
		t.Fatal("a recorded opt-out must show in the conversation context")
	}
}

func TestRecordOptOutTwiceChangesNothing(t *testing.T) {
	recorder := &consentRecorderStub{outcome: lead_usecase.ConsentOutcome{Changed: false}}
	result := runConsentTool(t, optOutTool(t, recorder, conversationMessages()), agentTurnConfig(), map[string]interface{}{"message_id": "m-stop"})
	if result.IsError || !strings.Contains(fmt.Sprint(result.Result), "já") || result.ContextUpdateText != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRecordOptOutRefusesMessagesThatAreNotTheLeadsRequestHere(t *testing.T) {
	cases := []struct {
		name      string
		messageID interface{}
		looksUp   bool
	}{
		{"a message from another conversation and lead", "m-other", true},
		{"our own message", "m-ours", true},
		{"an invented id", "3f8a21bc-0000-4000-8000-000000000000", true},
		{"no id", "", false},
		{"an id that is not text", 42, false},
		{"an id too long to be one", strings.Repeat("a", 200), false},
		{"an id with spaces inside", "m stop", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &consentRecorderStub{}
			messages := conversationMessages()
			result := runConsentTool(t, optOutTool(t, recorder, messages), agentTurnConfig(), map[string]interface{}{"message_id": tc.messageID})
			if !result.IsError || len(recorder.calls) != 0 {
				t.Fatalf("result = %+v, calls = %d", result, len(recorder.calls))
			}
			if !strings.Contains(fmt.Sprint(result.Result), "lead_consent_evidence") {
				t.Fatalf("the refusal must carry its code: %v", result.Result)
			}
			if looked := len(messages.lookups) > 0; looked != tc.looksUp {
				t.Fatalf("looked up = %v, want %v", looked, tc.looksUp)
			}
		})
	}
}

func TestRecordOptOutNeedsTheConversationItRunsIn(t *testing.T) {
	for _, missing := range []string{"__workspace_id", "__lead_id", "__entry_id", "__entry_type", "__agent_id"} {
		t.Run(missing, func(t *testing.T) {
			config := agentTurnConfig()
			delete(config, missing)
			recorder := &consentRecorderStub{}
			result := runConsentTool(t, optOutTool(t, recorder, conversationMessages()), config, map[string]interface{}{"message_id": "m-stop"})
			if !result.IsError || len(recorder.calls) != 0 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestRecordOptOutReportsWhyTheRecordFailed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		says string
	}{
		{"a refused change", lead.ErrConsentEvidenceRequired, "lead_consent_evidence_required"},
		{"the lead is gone", lead.ErrLeadNotFound, "lead_not_found"},
		{"the store is down", errors.New("database down"), "tente novamente"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &consentRecorderStub{err: tc.err}
			result := runConsentTool(t, optOutTool(t, recorder, conversationMessages()), agentTurnConfig(), map[string]interface{}{"message_id": "m-stop"})
			if !result.IsError || !strings.Contains(fmt.Sprint(result.Result), tc.says) {
				t.Fatalf("result = %+v, want it to say %q", result, tc.says)
			}
		})
	}
}

func TestRecordOptOutReportsAnUnreadableMessage(t *testing.T) {
	messages := conversationMessages()
	messages.err = errors.New("database down")
	recorder := &consentRecorderStub{}
	result := runConsentTool(t, optOutTool(t, recorder, messages), agentTurnConfig(), map[string]interface{}{"message_id": "m-stop"})
	if !result.IsError || len(recorder.calls) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRecordConsentGrantsAfterTheAgentsOwnQuestion(t *testing.T) {
	recorder := &consentRecorderStub{outcome: lead_usecase.ConsentOutcome{Changed: true, EventID: "ev-2"}}
	config := agentTurnConfig()
	config["purpose"] = "  novidades   da loja "
	result := runConsentTool(t, consentTool(t, recorder, conversationMessages()), config, map[string]interface{}{"question_message_id": "m-question", "message_id": "m-yes"})

	if result.IsError || len(recorder.calls) != 1 {
		t.Fatalf("result = %+v, calls = %d", result, len(recorder.calls))
	}
	want := lead.ConsentChange{
		Kind: lead.ConsentGrant, Source: lead.ConsentAIAgent, Purpose: "novidades   da loja",
		Actor:    lead.ConsentActor{Type: lead.ConsentActorAgent, ID: "agent-1"},
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-yes", Excerpt: "pode sim"},
	}
	if got := recorder.calls[0].change; got != want {
		t.Fatalf("recorded %+v, want %+v", got, want)
	}
}

func TestRecordConsentWithoutTheAgentsPriorQuestionIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]interface{}
	}{
		{"no question cited", map[string]interface{}{"message_id": "m-yes"}},
		{"an invented question", map[string]interface{}{"question_message_id": "q-invented", "message_id": "m-yes"}},
		{"the lead's own message as the question", map[string]interface{}{"question_message_id": "m-early", "message_id": "m-yes"}},
		{"an answer older than the question", map[string]interface{}{"question_message_id": "m-question", "message_id": "m-early"}},
		{"an answer from another conversation", map[string]interface{}{"question_message_id": "m-question", "message_id": "m-other"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &consentRecorderStub{}
			result := runConsentTool(t, consentTool(t, recorder, conversationMessages()), agentTurnConfig(), tc.params)
			if !result.IsError || len(recorder.calls) != 0 {
				t.Fatalf("result = %+v, calls = %d", result, len(recorder.calls))
			}
			if !strings.Contains(fmt.Sprint(result.Result), "lead_consent_evidence") {
				t.Fatalf("the refusal must carry its code: %v", result.Result)
			}
		})
	}
}

func TestRecordConsentRefusesAQuestionFromAnotherAgent(t *testing.T) {
	config := agentTurnConfig()
	config["__agent_id"] = "agent-2"
	recorder := &consentRecorderStub{}
	result := runConsentTool(t, consentTool(t, recorder, conversationMessages()), config, map[string]interface{}{"question_message_id": "m-question", "message_id": "m-yes"})
	if !result.IsError || len(recorder.calls) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestConsentToolsAreOfferedOnlyToAgentsThatSelectedThem(t *testing.T) {
	registry := NewService(
		optOutTool(t, &consentRecorderStub{}, conversationMessages()),
		consentTool(t, &consentRecorderStub{}, conversationMessages()),
		namedHandler{"manage_lead_memory"},
	)
	offered := func(bindings ...agent.ToolBinding) []string {
		resolved := ResolveTools(registry, bindings, agent.ToolVisibilityMessaging, ToolResolverOptions{})
		names := make([]string, 0, len(resolved.Definitions))
		for _, def := range resolved.Definitions {
			names = append(names, def.Name)
		}
		return names
	}

	if got := offered(agent.ToolBinding{Name: "manage_lead_memory"}); containsString(got, RecordOptOutToolName) || containsString(got, RecordConsentToolName) {
		t.Fatalf("an agent that did not pick the consent skills was offered %v", got)
	}
	if got := offered(agent.ToolBinding{Name: RecordOptOutToolName}); len(got) != 1 || got[0] != RecordOptOutToolName {
		t.Fatalf("offered %v, want only record_opt_out", got)
	}
	if got := offered(agent.ToolBinding{Name: RecordOptOutToolName}, agent.ToolBinding{Name: RecordConsentToolName}); len(got) != 2 {
		t.Fatalf("offered %v, want both skills", got)
	}
	if got := ResolveTools(registry, []agent.ToolBinding{{Name: RecordOptOutToolName}}, agent.ToolVisibility(tools.VisibilityAnalysis), ToolResolverOptions{}); len(got.Definitions) != 0 {
		t.Fatalf("record_opt_out must never reach an analysis run: %v", got.Definitions)
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
