package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/ai"
	"vozko/domain/channel"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
)

const (
	channelTextRulesHeading   = "## Limites de texto do canal"
	channelTextRulesClosing   = "Uma resposta fora desses limites não é enviada."
	textRulesRewriteHeading   = "AJUSTE DE FORMATO (gerado pelo sistema, não foi escrito por ninguém da conversa):"
	textRulesRewriteAsk       = "Reescreva a sua última resposta, com o mesmo conteúdo essencial, dentro dos limites do canal. Responda só com a nova mensagem."
	withheldReasonTextRules   = "channel_text_rules"
	violationDisclosureFills  = "disclosure_fills_limit"
	disclosureFillsLimitText  = "o aviso automático de IA ocupa todo o limite de texto do canal"
	rewriteWithoutToolsChoice = "none"
)

type AgentTextSender interface {
	SendAgentTextMessage(entryID, entryType, text, agentID string) (*conversation.Message, error)
}

type AgentReplyHandOff interface {
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}

func withChannelTextRules(systemPrompt string, caps channel.Capabilities) string {
	instructions := caps.TextInstructions()
	if len(instructions) == 0 {
		return systemPrompt
	}
	lines := make([]string, 0, len(instructions)+2)
	lines = append(lines, channelTextRulesHeading)
	for _, instruction := range instructions {
		lines = append(lines, "- "+instruction)
	}
	lines = append(lines, "- "+channelTextRulesClosing)
	return strings.TrimSpace(strings.TrimSpace(systemPrompt) + "\n\n" + strings.Join(lines, "\n"))
}

func textRulesRewriteInput(first ai.GenerateInput, draft string, violations []channel.TextViolation) ai.GenerateInput {
	problems := make([]string, 0, len(violations))
	for _, v := range violations {
		problems = append(problems, "- "+v.Message)
	}
	note := textRulesRewriteHeading + "\n" + strings.Join(problems, "\n") + "\n" + textRulesRewriteAsk

	rewrite := first
	rewrite.Messages = append(slices.Clone(first.Messages),
		ai.Message{Role: ai.RoleAssistant, Content: draft},
		ai.Message{Role: ai.RoleUser, Content: note},
	)
	rewrite.ToolExecutionMode = ai.ToolExecutionModeNone
	rewrite.ToolChoice = rewriteWithoutToolsChoice
	return rewrite
}

func disclosureFillsLimit() []channel.TextViolation {
	return []channel.TextViolation{{Code: violationDisclosureFills, Message: disclosureFillsLimitText}}
}

func violationCodes(violations []channel.TextViolation) string {
	codes := make([]string, 0, len(violations))
	for _, v := range violations {
		codes = append(codes, v.Code)
	}
	return strings.Join(codes, ",")
}

func violationMessages(violations []channel.TextViolation) string {
	messages := make([]string, 0, len(violations))
	for _, v := range violations {
		messages = append(messages, v.Message)
	}
	return strings.Join(messages, "; ")
}

func (s *ChannelAIReplyService) rewriteWithinRules(ctx context.Context, req conversation.AIReplyRequest, first ai.GenerateInput, draft string, room channel.Capabilities, violations []channel.TextViolation) (string, []channel.TextViolation) {
	log.Printf("[channel-ai] entry=%s reply breaks the channel text rules (%s), regenerating once", req.EntryID, violationCodes(violations))
	out, err := s.aiService.Generate(ctx, textRulesRewriteInput(first, draft, violations))
	if err != nil {
		log.Printf("[channel-ai] entry=%s regeneration failed: %v", req.EntryID, err)
		return "", violations
	}
	rewritten := ""
	if out != nil {
		rewritten = strings.TrimSpace(out.Message.Content)
	}
	if rewritten == "" {
		return "", violations
	}
	if remaining := room.TextViolations(rewritten); len(remaining) > 0 {
		return "", remaining
	}
	return rewritten, nil
}

func (s *ChannelAIReplyService) withholdAndHandOff(req conversation.AIReplyRequest, violations []channel.TextViolation) (*conversation.Message, error) {
	entryType := string(req.EntryType)
	log.Printf("[channel-ai] entry=%s reply withheld (%s): %s; handing the conversation to a person",
		req.EntryID, violationCodes(violations), violationMessages(violations))

	s.events.Log(ce.New(req.WorkspaceID, req.EntryID, entryType, ce.EventAgentReplyWithheld).
		WithActorAI(req.AgentID).
		WithChannel(entryType).
		WithDetails(map[string]string{
			"agent_id":   req.AgentID,
			"reason":     withheldReasonTextRules,
			"violations": violationCodes(violations),
			"detail":     violationMessages(violations),
		}).
		Build())

	owner, err := s.handOff.HandOffToRoulette(ia.RouletteHandOff{
		WorkspaceID: req.WorkspaceID,
		EntryID:     req.EntryID,
		EntryType:   entryType,
		ByActorID:   actor.FormatAI(req.AgentID),
	})
	if err != nil {
		return nil, fmt.Errorf("channel ai reply: entry %s reply withheld for the channel text rules but the hand-off failed: %w", req.EntryID, err)
	}
	log.Printf("[channel-ai] entry=%s handed to %q after the reply was withheld", req.EntryID, owner)
	return nil, skippedReply(replySkipWithheld)
}
