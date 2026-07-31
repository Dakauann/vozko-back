package conversation_usecase

import (
	"context"
	"log"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/usecases/conversation/loopguard"
)

// ChannelAIReplyService lets an AI agent attend a conversation on any
// adapter-backed channel.
//
// WhatsApp keeps its own dedicated pipeline (handle_whatsapp_message_usecase),
// which carries campaign variables, interactive replies, workflow triggers and
// billing hooks that are WhatsApp concepts. This service is the channel-agnostic
// equivalent for everything else: it reuses the send adapter, the message
// repository and the broadcaster, so a channel gains AI attendance by wiring,
// not by growing its own AI code.
//
// Automation gating is honoured exactly as WhatsApp honours it — see Reply.
type ChannelAIReplyService struct {
	agents    agent.Repository
	aiService ai.Service
	messages  conversation.MessageRepository
	sender    *MessageSenderService
	guard     loopguard.Guard
}

func NewChannelAIReplyService(
	agents agent.Repository,
	aiService ai.Service,
	messages conversation.MessageRepository,
	sender *MessageSenderService,
) *ChannelAIReplyService {
	return &ChannelAIReplyService{
		agents:    agents,
		aiService: aiService,
		messages:  messages,
		sender:    sender,
	}
}

// SetLoopGuard wires the shared runaway-conversation guard. Optional: without it
// the service still replies, it simply loses the bot-to-bot loop protection.
func (s *ChannelAIReplyService) SetLoopGuard(g loopguard.Guard) {
	s.guard = g
}

// historyDepth bounds how much transcript is replayed to the model. Deep enough
// for continuity, shallow enough to keep prompt cost predictable on a long
// conversation.
const historyDepth = 20

// Reply generates and sends an agent response, or returns nil when the message
// must not be answered.
//
// Every skip is a deliberate, logged decision rather than a silent no-op, since
// "the bot did not answer" is one of the hardest support questions to
// reconstruct after the fact.
func (s *ChannelAIReplyService) Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error) {
	if !s.enabled(req) {
		return nil, nil
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		// Media-only messages carry nothing to answer; the operator still sees
		// the message in the inbox.
		return nil, nil
	}

	if s.guard != nil {
		if dec := s.guard.CheckInbound(ctx, req.WorkspaceID, req.EntryID, text); dec.Block {
			log.Printf("[channel-ai] entry=%s loop suspected (%s, count=%d) — not replying",
				req.EntryID, dec.Reason, dec.Count)
			return nil, nil
		}
	}

	agentRecord, err := s.agents.FindByID(req.AgentID)
	if err != nil || agentRecord == nil {
		log.Printf("[channel-ai] entry=%s agent %s unavailable: %v", req.EntryID, req.AgentID, err)
		return nil, err
	}

	messages, err := s.buildPrompt(req, text)
	if err != nil {
		return nil, err
	}

	out, err := s.aiService.Generate(ctx, ai.GenerateInput{
		Model:        agentRecord.MessagingModel,
		SystemPrompt: agentRecord.MessagingPrompt,
		Messages:     messages,
		WorkspaceID:  req.WorkspaceID,
	})
	if err != nil {
		log.Printf("[channel-ai] entry=%s generation failed: %v", req.EntryID, err)
		return nil, err
	}

	reply := ""
	if out != nil {
		reply = strings.TrimSpace(out.Message.Content)
	}
	if reply == "" {
		log.Printf("[channel-ai] entry=%s model returned no content — not replying", req.EntryID)
		return nil, nil
	}

	message, err := s.sender.SendAgentTextMessage(req.EntryID, string(req.EntryType), reply, req.AgentID)
	if err != nil {
		// A closed provider window is an expected state, not a fault: the contact
		// must message again before a reply is allowed.
		if err == conversation.ErrOutboundWindowClosed {
			log.Printf("[channel-ai] entry=%s outbound window closed — reply withheld", req.EntryID)
			return nil, nil
		}
		return nil, err
	}

	if s.guard != nil {
		s.guard.RecordAIResponse(ctx, req.WorkspaceID, req.EntryID)
	}
	return message, nil
}

// enabled resolves the automation gate.
//
// Order matters: the per-conversation override wins when it has been set, so an
// operator who took over a conversation is never spoken over, even while the
// account-wide agent stays on for everyone else.
func (s *ChannelAIReplyService) enabled(req conversation.AIReplyRequest) bool {
	if s.sender == nil || s.aiService == nil || s.agents == nil {
		return false
	}
	if strings.TrimSpace(req.AgentID) == "" {
		return false
	}
	if req.AutomationEnabled != nil && !*req.AutomationEnabled {
		log.Printf("[channel-ai] entry=%s automation disabled for this conversation — not replying", req.EntryID)
		return false
	}
	if !req.AgentResponsesEnabled {
		return false
	}
	return true
}

// buildPrompt replays the recent transcript in provider-neutral form.
//
// Roles are derived from the stored message type rather than the channel, so the
// model sees one consistent conversation shape regardless of where it happened.
func (s *ChannelAIReplyService) buildPrompt(req conversation.AIReplyRequest, latest string) ([]ai.Message, error) {
	history, err := s.messages.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   req.EntryID,
		EntryType: req.EntryType,
		Limit:     historyDepth,
	})
	if err != nil {
		return nil, err
	}

	out := make([]ai.Message, 0, len(history)+1)
	for _, m := range history {
		if m == nil {
			continue
		}
		body := strings.TrimSpace(m.Text)
		if body == "" {
			continue
		}
		role := ai.RoleAssistant
		if m.MessageType.IsInbound() {
			role = ai.RoleUser
		}
		out = append(out, ai.Message{Role: role, Content: body})
	}

	// The message being answered may not be in the page yet (it is written and
	// replied to in the same webhook turn), so append it unless it is already the
	// last entry.
	if len(out) == 0 || out[len(out)-1].Content != latest {
		out = append(out, ai.Message{Role: ai.RoleUser, Content: latest})
	}
	return out, nil
}
