package conversation_usecase

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	balance_usecase "vozko/usecases/balance"
	tools_usecase "vozko/usecases/tools"
)

func longConversation(n int) []*conversation.Message {
	history := make([]*conversation.Message, 0, n)
	for i := range n {
		if i%2 == 0 {
			history = append(history, customerSays(fmt.Sprintf("pergunta %d", i)))
			continue
		}
		history = append(history, teamSays(fmt.Sprintf("resposta %d", i)))
	}
	return history
}

type grownTranscript struct {
	conversation.MessageRepository
	history []*conversation.Message
}

func (g grownTranscript) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	return g.history, nil
}

func (g grownTranscript) CountByEntry(string, shared.EntryType) (int64, error) {
	return int64(len(g.history)), nil
}

type autoTaskCase struct {
	feature     string
	instruction string
	build       func(history []*conversation.Message, volatile string) AutoTaskPrompt
	volatile    [2]string
	noted       string
}

func autoTaskCases() map[string]autoTaskCase {
	tags := []*stage.Stage{{Name: "Novo", Description: "acabou de chegar"}, {Name: "Agendado", Description: "marcou horário"}}
	return map[string]autoTaskCase{
		"stage": {
			feature:     "auto_stage",
			instruction: autoTagInstruction,
			volatile:    [2]string{"Novo", "Agendado"},
			noted:       `- Tag ATUAL do lead: "Agendado"`,
			build: func(history []*conversation.Message, current string) AutoTaskPrompt {
				return BuildAutoTagPrompt(AutoTagPromptInput{EntryID: "entry-1", CampaignName: "Campanha", MessageCount: len(history), History: history, CurrentTagName: current, Tags: tags})
			},
		},
		"memory": {
			feature:     "auto_memory",
			instruction: autoMemoryInstruction,
			volatile:    [2]string{"Prefere boleto", "Prefere pix"},
			noted:       "Prefere pix",
			build: func(history []*conversation.Message, memory string) AutoTaskPrompt {
				return BuildAutoMemoryPrompt(AutoMemoryPromptInput{EntryID: "entry-1", ContainerName: "Campanha", ContactLabel: "+55 11 99999-0000", MessageCount: len(history), CurrentMemories: "\n# Memórias sobre este lead\n- [m1] " + memory + "\n", History: history})
			},
		},
		"deals": {
			feature:     "auto_deals",
			instruction: autoDealInstruction,
			volatile:    [2]string{"deal-1 R$ 100", "deal-1 R$ 200"},
			noted:       "deal-1 R$ 200",
			build: func(history []*conversation.Message, deals string) AutoTaskPrompt {
				return BuildAutoDealPrompt(AutoDealPromptInput{EntryID: "entry-1", ContainerName: "Campanha", ContactLabel: "+55 11 99999-0000", MessageCount: len(history), CurrentDeals: "Oportunidades abertas:\n- " + deals, History: history})
			},
		},
	}
}

func lastMessage(messages []ai.Message) ai.Message {
	return messages[len(messages)-1]
}

func assertOnlyInTheNote(t *testing.T, name string, prompt AutoTaskPrompt, value string) {
	t.Helper()
	if !strings.Contains(lastMessage(prompt.Messages).Content, value) {
		t.Errorf("%s: %q is missing from the trailing note:\n%s", name, value, lastMessage(prompt.Messages).Content)
	}
	if strings.Contains(prompt.System, value) {
		t.Errorf("%s: %q leaked into the system prompt", name, value)
	}
	for _, message := range prompt.Messages[:len(prompt.Messages)-1] {
		if strings.Contains(message.Content, value) {
			t.Errorf("%s: %q leaked into the transcript", name, value)
		}
	}
}

func TestEachAutoTaskKeepsItsPrefixWhileTheConversationGrows(t *testing.T) {
	for name, task := range autoTaskCases() {
		before := task.build(longConversation(30), task.volatile[0])
		after := task.build(longConversation(31), task.volatile[1])

		if before.System != after.System {
			t.Errorf("%s: the system prompt changed between runs", name)
		}
		if len(before.Messages) != 2 || len(after.Messages) != 2 {
			t.Fatalf("%s: want the transcript and the note, got %d then %d messages", name, len(before.Messages), len(after.Messages))
		}
		if !strings.HasPrefix(after.Messages[0].Content, before.Messages[0].Content) {
			t.Errorf("%s: the transcript did not only grow at its end:\nbefore:\n%s\nafter:\n%s", name, before.Messages[0].Content, after.Messages[0].Content)
		}
		note := lastMessage(after.Messages)
		if note.Role != ai.RoleUser || !strings.HasPrefix(note.Content, ai.ContextNote().Content) {
			t.Errorf("%s: the last message must be the system context note:\n%s", name, note.Content)
		}
		assertOnlyInTheNote(t, name, after, "Total de mensagens na conversa: 31")
		assertOnlyInTheNote(t, name, after, task.noted)
		assertOnlyInTheNote(t, name, after, task.instruction)
		assertOnlyInTheNote(t, name, after, "User: pergunta 30")
		assertOnlyInTheNote(t, name, after, recentMessagesHeading)
		if !strings.Contains(after.Messages[0].Content, earlierHistoryHeading) || !strings.Contains(after.Messages[0].Content, "User: pergunta 0\n") {
			t.Errorf("%s: the earlier history belongs to the transcript message", name)
		}
	}
}

func TestTheStageOptionsCarryNoMarkerForTheCurrentStage(t *testing.T) {
	prompt := autoTaskCases()["stage"].build(longConversation(4), "Agendado")
	if strings.Contains(prompt.System, "→") || strings.Contains(prompt.System, "Tag ATUAL do lead") {
		t.Fatalf("the current stage belongs to the note, never to the option list:\n%s", prompt.System)
	}
	if !strings.Contains(prompt.System, `• "Agendado", marcou horário`) {
		t.Fatalf("the option list lost a stage:\n%s", prompt.System)
	}
	assertOnlyInTheNote(t, "stage", prompt, `- Tag ATUAL do lead: "Agendado"`)
}

func TestTheAnalysisWindowOnlyMovesInBlocks(t *testing.T) {
	for name, task := range autoTaskCases() {
		var previous string
		for _, total := range []int{130, 131, 140, 150} {
			transcript := task.build(longConversation(total), task.volatile[0]).Messages[0].Content
			if previous != "" && !strings.HasPrefix(transcript, previous) {
				t.Fatalf("%s: the window start moved at %d messages", name, total)
			}
			if !strings.Contains(transcript, "User: pergunta 50\n") || strings.Contains(transcript, "Agent: resposta 49\n") {
				t.Fatalf("%s: at %d messages the window must start at the block boundary:\n%s", name, total, transcript)
			}
			previous = transcript
		}
	}
}

func TestAShortConversationLivesInTheNote(t *testing.T) {
	for name, task := range autoTaskCases() {
		prompt := task.build([]*conversation.Message{customerSays("oi"), teamSays("olá")}, task.volatile[0])
		if len(prompt.Messages) != 1 {
			t.Fatalf("%s: without earlier history only the note is sent, got %d messages", name, len(prompt.Messages))
		}
		assertOnlyInTheNote(t, name, prompt, recentMessagesHeading+"\nUser: oi\nAgent: olá")
	}
}

func TestAnAutoTaskRequestDeclaresItsNoteAndScope(t *testing.T) {
	for name, task := range autoTaskCases() {
		prompt := task.build(longConversation(20), task.volatile[0])
		input := prompt.ApplyTo(ai.GenerateInput{WorkspaceID: "ws", Model: "openai/gpt-4o-mini", Temperature: 0.2})

		if input.VolatileTail != 1 {
			t.Errorf("%s: VolatileTail = %d", name, input.VolatileTail)
		}
		want := task.feature + ":entry-1"
		if input.SessionID != want || input.BillingReference != want {
			t.Errorf("%s: session %q, billing %q, want %q", name, input.SessionID, input.BillingReference, want)
		}
		if input.SystemPrompt != prompt.System || len(input.Messages) != len(prompt.Messages) {
			t.Errorf("%s: the prompt was not applied", name)
		}
		if input.WorkspaceID != "ws" || input.Model != "openai/gpt-4o-mini" || input.Temperature != 0.2 {
			t.Errorf("%s: the base request was not kept: %+v", name, input)
		}
	}
}

func TestTheQuietRunCachesEachDecisionAcrossRuns(t *testing.T) {
	var runs [][]ai.GenerateInput
	for _, total := range []int{130, 131} {
		subject := instagramSubject()
		subject.EnableAutoStaging = true
		job, recorder := dealJob(subject, &dealSettingsStub{pipeline: "deals"})
		job.messageRepo = grownTranscript{history: longConversation(total)}
		job.toolRegistry.(registryStub).handlers[tools_usecase.ManageEntryStageToolName] = describingStageTool{}
		if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
			t.Fatal(err)
		}
		if len(recorder.inputs) != 2 {
			t.Fatalf("calls = %d", len(recorder.inputs))
		}
		runs = append(runs, recorder.inputs)
	}
	for i, feature := range []string{"auto_stage", "auto_deals"} {
		before, after := runs[0][i], runs[1][i]
		if after.VolatileTail != 1 || after.SessionID != feature+":entry-1" || after.BillingReference != feature+":entry-1" {
			t.Errorf("%s: tail %d, session %q, billing %q", feature, after.VolatileTail, after.SessionID, after.BillingReference)
		}
		if before.SystemPrompt != after.SystemPrompt {
			t.Errorf("%s: the system prompt changed between runs", feature)
		}
		if !strings.HasPrefix(after.Messages[0].Content, before.Messages[0].Content) {
			t.Errorf("%s: the transcript did not only grow at its end", feature)
		}
		if !strings.Contains(lastMessage(after.Messages).Content, "Total de mensagens na conversa: 131") {
			t.Errorf("%s: the message count belongs to the note", feature)
		}
	}
}

func autoStageCampaign() *agentContext {
	return &agentContext{
		agent:        &agent.Agent{ID: "agent-1"},
		wcCampaign:   &wc.Campaign{ID: "camp-1", Name: "Campanha", WorkspaceID: "ws", EnableAutoStaging: true},
		wcEntry:      &wce.WhatsAppCampaignEntry{ID: "entry-1"},
		wcLeadRecord: &lead.Lead{Number: "5511999990000"},
	}
}

func autoStageAfterReply(t *testing.T, history []*conversation.Message) ai.GenerateInput {
	t.Helper()
	recorder := &generateRecorder{}
	uc := &handleWhatsAppMessageUseCase{
		aiService:    recorder,
		toolRegistry: registryStub{handlers: map[string]toolsdomain.Handler{tools_usecase.ManageEntryStageToolName: describingStageTool{}}},
		messageRepo:  grownTranscript{history: history},
		spendGuard:   balance_usecase.NewSpendGuard(nil, "auto stage test"),
	}
	uc.maybeRunWhatsAppCampaignTools(context.Background(), autoStageCampaign(), nil, "conv-1", history)
	if len(recorder.inputs) != 1 {
		t.Fatalf("auto-stage calls = %d", len(recorder.inputs))
	}
	return recorder.inputs[0]
}

func TestTheAutoStageAfterEachReplyKeepsAStablePrefix(t *testing.T) {
	before := autoStageAfterReply(t, longConversation(130))
	after := autoStageAfterReply(t, longConversation(131))

	if after.VolatileTail != 1 || after.SessionID != "auto_stage:entry-1" || after.BillingReference != "auto_stage:entry-1" {
		t.Fatalf("tail %d, session %q, billing %q", after.VolatileTail, after.SessionID, after.BillingReference)
	}
	if before.SystemPrompt != after.SystemPrompt {
		t.Fatal("the system prompt changed between replies")
	}
	if !strings.HasPrefix(after.Messages[0].Content, before.Messages[0].Content) {
		t.Fatal("the transcript did not only grow at its end")
	}
	note := lastMessage(after.Messages).Content
	if !strings.Contains(note, "Total de mensagens na conversa: 131") || !strings.Contains(note, autoTagInstruction) {
		t.Fatalf("the count and the instruction belong to the note:\n%s", note)
	}
	if len(after.Tools) != 1 || after.Tools[0].Name != tools_usecase.ManageEntryStageToolName {
		t.Fatalf("tools = %v", toolNames(after.Tools))
	}
}
