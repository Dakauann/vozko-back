package conversation_usecase

import (
	"context"
	"log"
	"strings"
	"sync"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/usecases/agentctx"
	"vozko/usecases/agentturn"
	"vozko/usecases/conversation/loopguard"
	shared_usecase "vozko/usecases/shared"
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
// Automation gating is honoured exactly as WhatsApp honours it, see Reply.
type ChannelAIReplyService struct {
	agents    agent.Repository
	aiService ai.Service
	messages  conversation.MessageRepository
	sender    *MessageSenderService
	guard     loopguard.Guard
	// assembler builds the turn: prompt, tools with channel seeds, identity
	// preamble and knowledge-base grounding. Optional only so an unwired
	// container still replies with plain text instead of failing; every real
	// deployment sets it.
	assembler *agentturn.Assembler
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

// SetAssembler wires the shared agent-turn recipe.
//
// Without it this service could only ever send plain text: no tools, no
// knowledge base, no channel identity, while the WhatsApp pipeline had all
// three. An agent configured with a knowledge base in the UI silently ignored
// it on every channel but WhatsApp.
func (s *ChannelAIReplyService) SetAssembler(a *agentturn.Assembler) {
	s.assembler = a
}

// historyDepth bounds how much transcript is replayed to the model. Deep enough
// for continuity, shallow enough to keep prompt cost predictable on a long
// conversation.
const historyDepth = 20

// nilServiceWarning keeps the nil-receiver report to one line per process. See
// Reply for why the condition is reported at all rather than just handled.
var nilServiceWarning sync.Once

// Reply generates and sends an agent response, or returns nil when the message
// must not be answered.
//
// Every skip is a deliberate, logged decision rather than a silent no-op, since
// "the bot did not answer" is one of the hardest support questions to
// reconstruct after the fact.
func (s *ChannelAIReplyService) Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error) {
	// Nil RECEIVER, not a nil field. Every channel stores this service behind its
	// own small interface, so a container that hands over a nil *ChannelAIReplyService
	// produces a non-nil interface wrapping a nil pointer — and every caller's
	// `!= nil` guard passes before panicking here. Answering "no reply" instead
	// keeps a wiring mistake from taking down inbound message handling for a
	// whole channel.
	//
	// LOUDLY, though: silently declining to answer is exactly the symptom nobody
	// can diagnose from the outside — the operator sees an agent configured, an
	// enabled toggle, and no replies. Once per process, because the receiver is
	// nil for the process's whole life and one message per inbound would bury
	// the log it needs to appear in.
	if s == nil {
		nilServiceWarning.Do(func() {
			log.Printf("[channel-ai-reply] BUG: the AI reply service is nil; no channel will "+
				"answer with an agent until it is wired before the channel runtimes "+
				"(first seen on entry %s/%s, workspace %s)",
				req.EntryType, req.EntryID, req.WorkspaceID)
		})
		return nil, nil
	}
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
			log.Printf("[channel-ai] entry=%s loop suspected (%s, count=%d), not replying",
				req.EntryID, dec.Reason, dec.Count)
			return nil, nil
		}
	}

	agentRecord, err := s.agents.FindByID(req.AgentID)
	if err != nil || agentRecord == nil {
		log.Printf("[channel-ai] entry=%s agent %s unavailable: %v", req.EntryID, req.AgentID, err)
		return nil, err
	}

	// Same per-request context the WhatsApp path installs: the agent for deep
	// tool code (MCP resolution, attribution fallback) and the tracker that
	// debounces a looping model re-firing the same side effect.
	ctx = agentctx.WithAgent(ctx, agentRecord)
	ctx = agentctx.WithToolExecutionTracker(ctx, agentctx.NewToolExecutionTracker())

	messages, err := s.buildPrompt(req, text)
	if err != nil {
		return nil, err
	}

	out, err := s.aiService.Generate(ctx, s.generateInput(ctx, req, agentRecord, messages, text))
	if err != nil {
		log.Printf("[channel-ai] entry=%s generation failed: %v", req.EntryID, err)
		return nil, err
	}

	reply := ""
	if out != nil {
		reply = strings.TrimSpace(out.Message.Content)
	}
	if reply == "" {
		log.Printf("[channel-ai] entry=%s model returned no content, not replying", req.EntryID)
		return nil, nil
	}

	message, err := s.sender.SendAgentTextMessage(req.EntryID, string(req.EntryType), reply, req.AgentID)
	if err != nil {
		// A closed provider window is an expected state, not a fault: the contact
		// must message again before a reply is allowed.
		if err == conversation.ErrOutboundWindowClosed {
			log.Printf("[channel-ai] entry=%s outbound window closed, reply withheld", req.EntryID)
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
		log.Printf("[channel-ai] entry=%s automation disabled for this conversation, not replying", req.EntryID)
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

	// ListByEntryPaginated orders created_at DESC, which is what makes Limit take
	// the most RECENT historyDepth messages instead of the oldest. The model
	// needs them chronologically, so the page is walked backwards.
	//
	// Replaying it as returned handed the model the conversation reversed: the
	// turn immediately before the one being answered was the OLDEST message in
	// the window, usually "/start" or the first greeting, so the agent answered
	// every message with its opening line and never appeared to remember
	// anything. WhatsApp never had this because it loads history ASC.
	out := make([]ai.Message, 0, len(history)+1)
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m == nil {
			continue
		}
		// Tool bookkeeping and call events are transcript rows, not turns in the
		// dialogue. WhatsApp filters them out of its prompt; without the same
		// filter here the model reads a tool result as something the operator
		// said. Reactions carry an emoji, not speech.
		switch m.MessageType {
		case conversation.MessageTypeToolCall,
			conversation.MessageTypeToolResult,
			conversation.MessageTypeSystem,
			conversation.MessageTypeReaction:
			continue
		}
		if m.MessageType.IsCallEvent() {
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

// generateInput builds the model request through the shared assembler.
//
// This is the whole point of adopting it: Instagram and Telegram now get the
// same recipe, resolved tools carrying this conversation's id, the channel
// identity preamble, and knowledge-base grounding, instead of a bare prompt.
// Anything the recipe gains later, every channel gains at the same moment.
//
// The assembler is build-only. Tool EXECUTION is the ai.Service's job
// (ToolExecutionModeAuto), so a tool the model calls runs and its result is fed
// back without this service owning a loop.
func (s *ChannelAIReplyService) generateInput(
	ctx context.Context,
	req conversation.AIReplyRequest,
	agentRecord *agent.Agent,
	messages []ai.Message,
	latest string,
) ai.GenerateInput {
	if s.assembler == nil {
		// Pre-assembler behaviour, kept so an unwired container still answers.
		return ai.GenerateInput{
			Model:        agentRecord.MessagingModel,
			SystemPrompt: agentRecord.MessagingPrompt,
			Messages:     messages,
			WorkspaceID:  req.WorkspaceID,
		}
	}

	identity := shared_usecase.ConversationContext{
		// Messaging, not WhatsApp: the preamble tells the agent which surface it
		// is on, and claiming WhatsApp here would have it offer to "send to your
		// WhatsApp" from inside a Telegram chat.
		Channel:        shared_usecase.ChannelMessaging,
		AgentName:      agentRecord.Name,
		ConversationID: req.EntryID,
	}

	// The seeds every conversation-scoped tool reads to know WHICH
	// conversation it is acting on. finish_conversation and
	// manage_entry_stage both require the entry pair; manage_lead_memory
	// additionally needs the lead and the acting agent.
	seed := map[string]interface{}{
		"__entry_id":     req.EntryID,
		"__entry_type":   string(req.EntryType),
		"__workspace_id": req.WorkspaceID,
		"__agent_id":     agentRecord.ID,
	}
	leadID := ""
	if req.LeadID != nil {
		leadID = strings.TrimSpace(*req.LeadID)
	}
	if leadID != "" {
		seed["__lead_id"] = leadID
	}

	assembled := s.assembler.Assemble(ctx, agentturn.Request{
		Agent:    agentRecord,
		Identity: &identity,

		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             seed,

		// Ground on the message being answered.
		RAGQuery: latest,

		// Lead memories ride the same recipe: unresolved lead (nil/empty)
		// simply renders no block.
		LeadID: leadID,

		// History already ends with the message being answered, so it is not
		// passed again as UserMessage, that would duplicate the last turn.
		History: messages,

		Model: agentRecord.MessagingModel,
	})

	// The assembler leaves execution knobs to the caller by design.
	assembled.Input.WorkspaceID = req.WorkspaceID
	assembled.Input.ToolExecutionMode = ai.ToolExecutionModeAuto

	if len(assembled.ToolNames) > 0 {
		log.Printf("[channel-ai] entry=%s assembled with %d tool(s): %v",
			req.EntryID, len(assembled.ToolNames), assembled.ToolNames)
	}
	return assembled.Input
}
