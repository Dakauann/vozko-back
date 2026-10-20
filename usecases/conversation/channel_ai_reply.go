package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/agent"
	"vozko/domain/ai"
	aa "vozko/domain/ai_attendance"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/usecases/agentctx"
	"vozko/usecases/agentturn"
	"vozko/usecases/conversation/loopguard"
	shared_usecase "vozko/usecases/shared"
)

type ChannelAIReplyService struct {
	agents    agent.Repository
	aiService ai.Service
	messages  conversation.MessageRepository
	sender    AgentTextSender
	adapters  conversation.AdapterRegistry
	handOff   AgentReplyHandOff
	events    ce.Logger
	guard     loopguard.Guard
	assembler *agentturn.Assembler

	workflowNames ActorNameResolver
	turnLock      *AgentTurnLock
	aiAttendance  AIAttendanceRecorder
	entryLeads    ContinuationLeads
}

type ContinuationLeads interface {
	lead.EntryLeads
	FindByID(workspaceID, id string) (*lead.Lead, error)
}

func (s *ChannelAIReplyService) SetEntryLeads(leads ContinuationLeads) {
	s.entryLeads = leads
}

func (s *ChannelAIReplyService) SetAgentTurnLock(lock *AgentTurnLock) {
	s.turnLock = lock
}

func (s *ChannelAIReplyService) SetAIAttendance(recorder AIAttendanceRecorder) {
	s.aiAttendance = recorder
}

func NewChannelAIReplyService(
	agents agent.Repository,
	aiService ai.Service,
	messages conversation.MessageRepository,
	sender AgentTextSender,
	adapters conversation.AdapterRegistry,
	handOff AgentReplyHandOff,
	events ce.Logger,
) *ChannelAIReplyService {
	missing := []string{}
	for name, dep := range map[string]any{
		"agent repository":    agents,
		"ai service":          aiService,
		"message repository":  messages,
		"agent text sender":   sender,
		"channel adapters":    adapters,
		"hand-off":            handOff,
		"conversation events": events,
	} {
		if dep == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		panic("channel ai reply: missing " + strings.Join(missing, ", "))
	}
	return &ChannelAIReplyService{
		agents:    agents,
		aiService: aiService,
		messages:  messages,
		sender:    sender,
		adapters:  adapters,
		handOff:   handOff,
		events:    events,
	}
}

func (s *ChannelAIReplyService) SetLoopGuard(g loopguard.Guard) {
	s.guard = g
}

func (s *ChannelAIReplyService) SetAssembler(a *agentturn.Assembler) {
	s.assembler = a
}

func (s *ChannelAIReplyService) SetWorkflowNames(names ActorNameResolver) {
	s.workflowNames = names
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

	release, held := s.turnLock.HoldForInbound(ctx, req.EntryID, req.EntryType)
	defer release()
	if !held {
		return nil, nil
	}
	message, err := s.reply(ctx, req, text, "")
	return message, failedReply(err)
}

func (s *ChannelAIReplyService) ContinuationWindowOpen(ctx context.Context, turn AgentContinuationTurn) (bool, error) {
	adapter, err := s.adapters.For(turn.EntryType)
	if err != nil {
		return false, err
	}
	ec, err := adapter.ResolveEntry(ctx, turn.EntryID)
	if err != nil {
		return false, err
	}
	window, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return false, err
	}
	return window.Open, nil
}

func (s *ChannelAIReplyService) ContinueAgentTurn(ctx context.Context, turn AgentContinuationTurn) error {
	req := conversation.AIReplyRequest{
		WorkspaceID:           turn.WorkspaceID,
		EntryID:               turn.EntryID,
		EntryType:             turn.EntryType,
		AgentID:               turn.AgentID,
		AgentResponsesEnabled: true,
	}
	leadID, contact, err := s.leadOfTurn(ctx, turn)
	if err != nil {
		return err
	}
	if err := refusedContact(contact); err != nil {
		return err
	}
	if leadID != "" {
		req.LeadID = &leadID
	}
	if req.Disclosure, err = s.disclosureOf(ctx, turn); err != nil {
		return err
	}
	_, err = s.reply(ctx, req, "", turn.Note)
	return err
}

var errContinuationLeadsUnwired = errors.New("channel continuation: the lead of the conversation cannot be read, no lead store wired")

func (s *ChannelAIReplyService) leadOfTurn(ctx context.Context, turn AgentContinuationTurn) (string, *lead.Lead, error) {
	if s.entryLeads == nil {
		return "", nil, errContinuationLeadsUnwired
	}
	leadID, err := s.entryLeads.LeadOfEntry(ctx, turn.WorkspaceID, shared.EntryRef{EntryID: turn.EntryID, EntryType: turn.EntryType})
	if errors.Is(err, lead.ErrLeadNotFound) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, turnNotStarted(fmt.Errorf("read the lead of %s %s for the continuation: %w", turn.EntryType, turn.EntryID, err))
	}
	contact, err := s.entryLeads.FindByID(turn.WorkspaceID, leadID)
	if err != nil {
		return "", nil, turnNotStarted(fmt.Errorf("read lead %s for the continuation: %w", leadID, err))
	}
	if contact == nil {
		return "", nil, fmt.Errorf("read lead %s for the continuation: %w", leadID, lead.ErrLeadNotFound)
	}
	return leadID, contact, nil
}

func turnNotStarted(err error) error {
	return fmt.Errorf("%w: %w", conversation.ErrAgentTurnNotStarted, err)
}

func (s *ChannelAIReplyService) disclosureOf(ctx context.Context, turn AgentContinuationTurn) (string, error) {
	adapter, err := s.adapters.For(turn.EntryType)
	if err != nil {
		return "", err
	}
	disclosing, ok := adapter.(conversation.DisclosingAdapter)
	if !ok {
		return "", nil
	}
	ec, err := adapter.ResolveEntry(ctx, turn.EntryID)
	if err != nil {
		return "", turnNotStarted(fmt.Errorf("resolve %s %s for its automation disclosure: %w", turn.EntryType, turn.EntryID, err))
	}
	disclosure, err := disclosing.AutomationDisclosure(ctx, ec)
	if err != nil {
		return "", turnNotStarted(fmt.Errorf("read the automation disclosure of %s %s: %w", turn.EntryType, turn.EntryID, err))
	}
	return disclosure, nil
}

func (s *ChannelAIReplyService) reply(ctx context.Context, req conversation.AIReplyRequest, text, triggerNote string) (*conversation.Message, error) {
	caps, err := conversation.CapabilitiesOf(s.adapters, req.EntryType)
	if err != nil {
		log.Printf("[channel-ai] entry=%s the text rules of %s are unknown, not replying: %v", req.EntryID, req.EntryType, err)
		return nil, err
	}

	agentRecord, err := s.agents.FindByID(req.AgentID)
	if err != nil || agentRecord == nil {
		log.Printf("[channel-ai] entry=%s agent %s unavailable: %v", req.EntryID, req.AgentID, err)
		return nil, err
	}

	ctx = agentctx.WithAgent(ctx, agentRecord)
	ctx = agentctx.WithToolExecutionTracker(ctx, agentctx.NewToolExecutionTracker())

	history, err := s.recentHistory(req)
	if err != nil {
		return nil, err
	}
	messages := promptFrom(history, text, historySourceLabels(s.workflowNames, history))

	now := time.Now().UTC()
	room, fits := caps.Reserving(conversation.WithAutomationDisclosure(req.Disclosure, "", history, now))
	if !fits {
		return s.withholdAndHandOff(req, disclosureFillsLimit())
	}

	input := s.assembleTurn(ctx, req, agentRecord, messages, text, triggerNote)
	input.SystemPrompt = withChannelTextRules(input.SystemPrompt, room)
	out, err := s.aiService.Generate(ctx, input)
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
		return nil, skippedReply(replySkipEmptyReply)
	}

	if violations := room.TextViolations(reply); len(violations) > 0 {
		rewritten, remaining := s.rewriteWithinRules(ctx, req, input, reply, room, violations)
		if rewritten == "" {
			return s.withholdAndHandOff(req, remaining)
		}
		reply = rewritten
	}

	reply = conversation.WithAutomationDisclosure(req.Disclosure, reply, history, now)
	if violations := caps.TextViolations(reply); len(violations) > 0 {
		return s.withholdAndHandOff(req, violations)
	}
	message, err := s.sender.SendAgentTextMessage(req.EntryID, string(req.EntryType), reply, req.AgentID)
	if err != nil {
		if err == conversation.ErrOutboundWindowClosed {
			log.Printf("[channel-ai] entry=%s outbound window closed, reply withheld", req.EntryID)
			return nil, skippedReply(replySkipWindowClosed)
		}
		return nil, err
	}

	if s.guard != nil {
		s.guard.RecordAIResponse(ctx, req.WorkspaceID, req.EntryID)
	}
	s.recordAIReply(req, agentRecord, message)
	return message, nil
}

func (s *ChannelAIReplyService) recordAIReply(req conversation.AIReplyRequest, agentRecord *agent.Agent, message *conversation.Message) {
	if s.aiAttendance == nil || message == nil {
		return
	}
	s.aiAttendance.RecordAIReply(aa.StartInput{
		WorkspaceID: req.WorkspaceID,
		EntryID:     req.EntryID,
		EntryType:   string(req.EntryType),
		AgentID:     agentRecord.ID,
		Channel:     string(req.EntryType),
		Model:       strings.TrimSpace(agentRecord.MessagingModel),
	}, message.ID)
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
	history, err := s.recentHistory(req)
	if err != nil {
		return nil, err
	}
	return promptFrom(history, latest, historySourceLabels(s.workflowNames, history)), nil
}

func (s *ChannelAIReplyService) recentHistory(req conversation.AIReplyRequest) ([]*conversation.Message, error) {
	total, err := s.messages.CountByEntry(req.EntryID, req.EntryType)
	if err != nil {
		return nil, err
	}
	window := int(total) - ai.HistoryWindowStart(int(total), historyDepth)
	if window <= 0 {
		return nil, nil
	}
	return s.messages.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   req.EntryID,
		EntryType: req.EntryType,
		Limit:     window,
	})
}

func promptFrom(history []*conversation.Message, latest string, labels conversation.SourceLabels) []ai.Message {
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
		body := strings.TrimSpace(m.ReadableText())
		if body == "" {
			continue
		}
		role := ai.RoleAssistant
		if m.FromCustomer() {
			role = ai.RoleUser
		}
		out = append(out, ai.Message{Role: role, Content: labels.Label(m, body)})
	}

	if latest != "" && (len(out) == 0 || out[len(out)-1].Content != latest) {
		out = append(out, ai.Message{Role: ai.RoleUser, Content: latest})
	}
	return out
}

func (s *ChannelAIReplyService) generateInput(
	ctx context.Context,
	req conversation.AIReplyRequest,
	agentRecord *agent.Agent,
	messages []ai.Message,
	latest string,
) ai.GenerateInput {
	return s.assembleTurn(ctx, req, agentRecord, messages, latest, "")
}

func (s *ChannelAIReplyService) assembleTurn(
	ctx context.Context,
	req conversation.AIReplyRequest,
	agentRecord *agent.Agent,
	messages []ai.Message,
	latest string,
	triggerNote string,
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
		EntryID:              req.EntryID,
		EntryType:            string(req.EntryType),

		RAGQuery: latest,

		LeadID: leadID,

		Session: agentturn.ReplySession(req.EntryID),

		History:     messages,
		TriggerNote: triggerNote,

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
