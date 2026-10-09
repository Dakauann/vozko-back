package conversation_usecase

import (
	"fmt"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/stage"
)

func customerSays(text string) *conversation.Message {
	return &conversation.Message{Text: text, MessageType: conversation.MessageTypeUserMessage, Direction: conversation.MessageDirectionInbound}
}

func teamSays(text string) *conversation.Message {
	return &conversation.Message{Text: text, MessageType: conversation.MessageTypeOperator, Direction: conversation.MessageDirectionOutbound}
}

func reversal() []*conversation.Message {
	history := []*conversation.Message{customerSays("quero agendar uma consulta"), teamSays("claro")}
	for i := 0; i < 6; i++ {
		history = append(history, customerSays(fmt.Sprintf("pergunta %d", i)), teamSays(fmt.Sprintf("resposta %d", i)))
	}
	return append(history,
		customerSays("não quero mais, pode cancelar"),
		teamSays("cancelado"),
		customerSays("na verdade quero continuar"),
		teamSays("certo, vamos seguir"),
	)
}

func wholePrompt(prompt AutoTaskPrompt) string {
	parts := []string{prompt.System}
	for _, message := range prompt.Messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n\n")
}

func TestTheRecentMessagesAreSetApartFromTheEarlierHistory(t *testing.T) {
	transcript := splitByRecency(reversal())

	if !strings.HasPrefix(transcript.earlier, earlierHistoryHeading) || !strings.Contains(transcript.earlier, "User: quero agendar uma consulta") {
		t.Fatalf("the opening belongs to the earlier history:\n%s", transcript.earlier)
	}
	if !strings.HasPrefix(transcript.recent, recentMessagesHeading) {
		t.Fatalf("no recent block:\n%s", transcript.recent)
	}
	if strings.Count(transcript.recent, "\n") != recentMessageCount+1 || !strings.Contains(transcript.recent, "User: na verdade quero continuar") {
		t.Fatalf("the last %d messages form the recent block:\n%s", recentMessageCount, transcript.recent)
	}
	if strings.Contains(transcript.recent, "quero agendar uma consulta") {
		t.Fatal("an old message leaked into the recent block")
	}
}

func TestAShortConversationIsAllRecent(t *testing.T) {
	transcript := splitByRecency([]*conversation.Message{customerSays("oi"), teamSays("olá")})
	if transcript.earlier != "" || !strings.HasPrefix(transcript.recent, recentMessagesHeading) {
		t.Fatalf("earlier:\n%s\nrecent:\n%s", transcript.earlier, transcript.recent)
	}
}

func TestEveryPromptOfTheQuietRunFavoursTheLatestPosition(t *testing.T) {
	history := reversal()
	prompts := map[string]AutoTaskPrompt{
		"stage": BuildAutoTagPrompt(AutoTagPromptInput{CampaignName: "c", History: history,
			Tags: []*stage.Stage{{Name: "agendado"}, {Name: "desistiu"}}}),
		"memory": BuildAutoMemoryPrompt(AutoMemoryPromptInput{ContainerName: "c", History: history}),
		"deals":  BuildAutoDealPrompt(AutoDealPromptInput{ContainerName: "c", History: history}),
	}
	for name, prompt := range prompts {
		if !strings.Contains(prompt.System, latestPositionRule) {
			t.Errorf("%s prompt lacks the latest-position rule", name)
		}
		if strings.Contains(wholePrompt(prompt), "%!") {
			t.Errorf("%s prompt has format errors", name)
		}
		if !strings.Contains(lastMessage(prompt.Messages).Content, recentMessagesHeading) {
			t.Errorf("%s prompt does not show which messages are recent", name)
		}
	}
}

func TestADealIsLostOnlyWhenGivingUpIsTheLatestPosition(t *testing.T) {
	if !strings.Contains(autoDealRules, "lose: só quando desistir é a posição MAIS RECENTE") {
		t.Fatal("lose must require the latest position")
	}
}

func TestMemoryLeavesTheStateOfTheConversationToTheFunnel(t *testing.T) {
	if !strings.Contains(autoMemoryRules, "isso é a etapa do funil, não memória") {
		t.Fatal("wanting to continue, giving up or coming back is the stage, never a memory")
	}
	if !strings.Contains(autoMemoryRules, "sem memórias salvas, nunca use 'forget'") {
		t.Fatal("forget needs a memory id from the list")
	}
}

func TestEachDecisionHasItsOwnInstruction(t *testing.T) {
	for name, instruction := range map[string]string{"stage": autoTagInstruction, "memory": autoMemoryInstruction, "deals": autoDealInstruction} {
		if strings.Contains(instruction, "Além disso") {
			t.Errorf("the %s instruction bundles another task", name)
		}
	}
	if !strings.Contains(autoTagInstruction, "posição MAIS RECENTE") || !strings.Contains(autoDealInstruction, "posição MAIS RECENTE") {
		t.Error("stage and opportunity decide by the latest position")
	}
}
