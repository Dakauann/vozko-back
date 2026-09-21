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

type ChannelAIReplyService struct {
	agents    agent.Repository
	aiService ai.Service
	messages  conversation.MessageRepository
	sender    *MessageSenderService
	guard     loopguard.Guard
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

func (s *ChannelAIReplyService) SetLoopGuard(g loopguard.Guard) {
	s.guard = g
}

func (s *ChannelAIReplyService) SetAssembler(a *agentturn.Assembler) {
	s.assembler = a
}

const historyDepth = 20

var nilServiceWarning sync.Once

func (s *ChannelAIReplyService) Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error) {
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
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m == nil {
			continue
		}
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

	if len(out) == 0 || out[len(out)-1].Content != latest {
		out = append(out, ai.Message{Role: ai.RoleUser, Content: latest})
	}
	return out, nil
}

func (s *ChannelAIReplyService) generateInput(
	ctx context.Context,
	req conversation.AIReplyRequest,
	agentRecord *agent.Agent,
	messages []ai.Message,
	latest string,
) ai.GenerateInput {
	if s.assembler == nil {
		return ai.GenerateInput{
			Model:        agentRecord.MessagingModel,
			SystemPrompt: agentRecord.MessagingPrompt,
			Messages:     messages,
			WorkspaceID:  req.WorkspaceID,
		}
	}

	identity := shared_usecase.ConversationContext{
		Channel:        shared_usecase.ChannelMessaging,
		AgentName:      agentRecord.Name,
		ConversationID: req.EntryID,
	}

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

		RAGQuery: latest,

		LeadID: leadID,

		History: messages,

		Model: agentRecord.MessagingModel,
	})

	assembled.Input.WorkspaceID = req.WorkspaceID
	assembled.Input.ToolExecutionMode = ai.ToolExecutionModeAuto

	if len(assembled.ToolNames) > 0 {
		log.Printf("[channel-ai] entry=%s assembled with %d tool(s): %v",
			req.EntryID, len(assembled.ToolNames), assembled.ToolNames)
	}
	return assembled.Input
}
