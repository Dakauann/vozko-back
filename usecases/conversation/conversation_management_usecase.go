package conversation_usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/agent"
	"vozko/domain/ai"
	analysisdomain "vozko/domain/analysis"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	"vozko/domain/user"
	callpermission "vozko/domain/whatsapp/call_permission"
	whatsappTemplate "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"

	balance_domain "vozko/domain/balance"
)

// workflowRunLookup and workflowLookup are the narrow read-only ports the inbox
// enrichment needs from the workflow package, so the conversation read model does not
// depend on the full workflow repositories.
type workflowRunLookup interface {
	FindActiveByEntries(entryIDs []string) (map[string]*workflow.WorkflowRun, error)
}

type workflowLookup interface {
	FindByIDs(ids []string) ([]*workflow.Workflow, error)
}

type HistoryProviderService struct {
	messageRepo       conversation.MessageRepository
	whatsappRepo      wce.Repository
	leadRepo          lead.Repository
	userRepo          user.UserRepository
	agentRepo         agent.Repository
	messageWindowRepo lmw.Repository
	assignmentRepo    ia.Repository
	workflowRunRepo   workflowRunLookup
	workflowRepo      workflowLookup
}

// SetWorkflowLookups wires the optional workflow read ports used to show which
// workflow (and current node) attends a conversation. When unset, inbox entries simply
// omit workflow-run detail (agents still resolve).
func (s *HistoryProviderService) SetWorkflowLookups(runs workflowRunLookup, workflows workflowLookup) {
	s.workflowRunRepo = runs
	s.workflowRepo = workflows
}

func NewHistoryProviderService(
	messageRepo conversation.MessageRepository,
	whatsappRepo wce.Repository,
	leadRepo lead.Repository,
	userRepo user.UserRepository,
	agentRepo agent.Repository,
	messageWindowRepo lmw.Repository,
) *HistoryProviderService {
	return &HistoryProviderService{
		messageRepo:       messageRepo,
		whatsappRepo:      whatsappRepo,
		leadRepo:          leadRepo,
		userRepo:          userRepo,
		agentRepo:         agentRepo,
		messageWindowRepo: messageWindowRepo,
	}
}

func (s *HistoryProviderService) SetAssignmentRepo(repo ia.Repository) {
	s.assignmentRepo = repo
}

func (s *HistoryProviderService) enrichAssignments(entries []conversation.InboxEntry, workspaceID string) {
	if s.assignmentRepo == nil || len(entries) == 0 || workspaceID == "" {
		return
	}

	entryIDs := make([]string, len(entries))
	for i := range entries {
		entryIDs[i] = entries[i].EntryID
	}

	assignments, err := s.assignmentRepo.FindByEntries(workspaceID, entryIDs)
	if err != nil {
		log.Printf("[HistoryProvider] Error batch-fetching assignments: %v", err)
		return
	}

	assignmentMap := make(map[string]*ia.InboxAssignment, len(assignments))
	userIDSet := make(map[string]struct{})
	for _, a := range assignments {
		assignmentMap[a.EntryID] = a
		if a.AssignedUserID != "" {
			userIDSet[a.AssignedUserID] = struct{}{}
		}
	}

	for i := range entries {
		if a, ok := assignmentMap[entries[i].EntryID]; ok {
			entries[i].AssignedUserID = a.AssignedUserID
		}
	}

	if len(userIDSet) == 0 || s.userRepo == nil {
		return
	}

	userIDs := make([]string, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}

	users, err := s.userRepo.FindByIDs(userIDs)
	if err != nil {
		log.Printf("[HistoryProvider] Error batch-fetching users for assignments: %v", err)
		return
	}

	userMap := make(map[string]string, len(users))
	for _, u := range users {
		username := u.Username
		if username == "" {
			username = strings.Split(u.Email, "@")[0]
		}
		userMap[u.ID] = username
	}

	for i := range entries {
		if entries[i].AssignedUserID != "" {
			entries[i].AssignedUsername = userMap[entries[i].AssignedUserID]
		}
	}
}

func (s *HistoryProviderService) GetHistory(entryID string, entryType shared.EntryType, limit int) ([]*conversation.Message, bool, int64, error) {
	if limit <= 0 {
		limit = conversation.DefaultHistoryPageSize
	}
	if limit > conversation.MaxHistoryPageSize {
		limit = conversation.MaxHistoryPageSize
	}

	messages, err := s.messageRepo.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   entryID,
		EntryType: entryType,
		Limit:     limit + 1,
	})
	if err != nil {
		return nil, false, 0, err
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	reverseMessages(messages)

	total, err := s.messageRepo.CountByEntry(entryID, entryType)
	if err != nil {
		log.Printf("[HistoryProvider] Error counting messages for %s:%s: %v", entryType, entryID, err)
		total = int64(len(messages))
	}

	leadName, leadNumber, leadPicture, _, _, _, _ := s.GetEntryInfo(entryID, string(entryType))

	for _, msg := range messages {
		msg.SenderName, msg.SenderAvatar = s.getSenderInfo(msg.From, msg.MessageType, leadName, leadNumber, leadPicture)
	}

	return messages, hasMore, total, nil
}

func (s *HistoryProviderService) GetHistoryBefore(entryID string, entryType shared.EntryType, before time.Time, limit int) ([]*conversation.Message, bool, error) {
	if limit <= 0 {
		limit = conversation.DefaultHistoryPageSize
	}
	if limit > conversation.MaxHistoryPageSize {
		limit = conversation.MaxHistoryPageSize
	}

	messages, err := s.messageRepo.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   entryID,
		EntryType: entryType,
		Limit:     limit + 1,
		Before:    &before,
	})
	if err != nil {
		return nil, false, err
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	reverseMessages(messages)

	leadName, leadNumber, leadPicture, _, _, _, _ := s.GetEntryInfo(entryID, string(entryType))

	for _, msg := range messages {
		msg.SenderName, msg.SenderAvatar = s.getSenderInfo(msg.From, msg.MessageType, leadName, leadNumber, leadPicture)
	}

	return messages, hasMore, nil
}

func (s *HistoryProviderService) GetHistoryAround(entryID string, entryType shared.EntryType, around time.Time, limit int) ([]*conversation.Message, bool, bool, int64, error) {
	if limit <= 0 {
		limit = conversation.DefaultHistoryPageSize
	}
	if limit > conversation.MaxHistoryPageSize {
		limit = conversation.MaxHistoryPageSize
	}

	half := limit / 2
	if half < 1 {
		half = 1
	}

	beforeMsgs, err := s.messageRepo.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   entryID,
		EntryType: entryType,
		Limit:     half + 1,
		Before:    timePtr(around.Add(time.Millisecond)),
	})
	if err != nil {
		return nil, false, false, 0, err
	}

	hasBefore := len(beforeMsgs) > half
	if hasBefore {
		beforeMsgs = beforeMsgs[:half]
	}

	afterTime := around.Add(time.Millisecond)
	afterMsgs, err := s.messageRepo.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   entryID,
		EntryType: entryType,
		Limit:     half + 1,
		After:     &afterTime,
	})
	if err != nil {
		return nil, false, false, 0, err
	}

	hasAfter := len(afterMsgs) > half
	if hasAfter {
		afterMsgs = afterMsgs[:half]
	}

	reverseMessages(beforeMsgs)
	reverseMessages(afterMsgs)

	combined := make([]*conversation.Message, 0, len(beforeMsgs)+len(afterMsgs))
	combined = append(combined, beforeMsgs...)
	combined = append(combined, afterMsgs...)

	total, _ := s.messageRepo.CountByEntry(entryID, entryType)

	leadName, leadNumber, leadPicture, _, _, _, _ := s.GetEntryInfo(entryID, string(entryType))
	for _, msg := range combined {
		msg.SenderName, msg.SenderAvatar = s.getSenderInfo(msg.From, msg.MessageType, leadName, leadNumber, leadPicture)
	}

	return combined, hasBefore, hasAfter, total, nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func reverseMessages(msgs []*conversation.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func (s *HistoryProviderService) GetWindowStatusForEntry(entryID, entryType string) (windowOpen bool, windowExpiresAt *time.Time) {
	var leadID, businessPhoneID string

	switch entryType {
	case "whatsapp":
		entry, err := s.whatsappRepo.FindByID(entryID)
		if err != nil || entry == nil {
			return false, nil
		}
		leadID = entry.LeadID
		campaign, err := s.whatsappRepo.GetCampaignForEntry(entryID)
		if err == nil && campaign != nil {
			businessPhoneID = campaign.BusinessPhoneID
		}
	default:
		return false, nil
	}

	return s.getWindowStatus(leadID, businessPhoneID)
}

func (s *HistoryProviderService) GetUnreadCount(entryID string, entryType shared.EntryType) (int64, error) {
	return s.messageRepo.CountUnreadByEntry(entryID, entryType)
}

func (s *HistoryProviderService) GetEntryInfo(entryID, entryType string) (leadName, leadNumber, leadPicture string, leadMetadata map[string]interface{}, entryVariables []string, automationEnabled bool, err error) {
	var leadID, workspaceID string
	var entryMetadata map[string]interface{}
	automationEnabled = true

	switch entryType {
	case "whatsapp":
		entry, err := s.whatsappRepo.FindByID(entryID)
		if err != nil {
			return "", "", "", nil, nil, true, err
		}
		leadID = entry.LeadID
		entryVariables = entry.Variables
		entryMetadata = entry.Metadata
		automationEnabled = entry.IsAutomationEnabled()
		if info, infoErr := s.whatsappRepo.GetCampaignForEntry(entryID); infoErr == nil && info != nil {
			workspaceID = info.WorkspaceID
		}
	default:
		return "", "", "", nil, nil, true, errors.New("invalid entry type")
	}

	if workspaceID == "" {
		return "", "", "", nil, nil, automationEnabled, errors.New("unable to resolve workspace for entry")
	}

	leadRecord, err := s.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		return "", "", "", nil, nil, automationEnabled, err
	}

	return leadRecord.Name, leadRecord.Number, leadRecord.ProfilePictureURL, entryMetadata, entryVariables, automationEnabled, nil
}

func (s *HistoryProviderService) GetInboxEntries(userID, workspaceID, campaignID, campaignType string, page, pageSize int) ([]conversation.InboxEntry, int64, error) {
	if campaignID == "" || campaignType == "" {
		return nil, 0, fmt.Errorf("campaignID and campaignType are required")
	}

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = conversation.DefaultInboxPageSize
	}
	if pageSize > conversation.MaxInboxPageSize {
		pageSize = conversation.MaxInboxPageSize
	}

	var entryType shared.EntryType

	switch campaignType {
	case "whatsapp":
		entryType = shared.EntryTypeWhatsApp
	default:
		return nil, 0, fmt.Errorf("invalid campaign type: %s", campaignType)
	}

	log.Printf("[HistoryProvider] Campaign %s (%s): page %d (size %d)", campaignID, campaignType, page, pageSize)

	campaignEntries, totalWithMessages, err := s.messageRepo.GetEntriesWithMessages(campaignID, nil, entryType, page, pageSize, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("error getting entries with messages: %w", err)
	}

	leadIDs := make([]string, 0, len(campaignEntries))
	entryIDs := make([]string, 0, len(campaignEntries))
	leadIDSet := make(map[string]struct{})
	for _, e := range campaignEntries {
		entryIDs = append(entryIDs, e.EntryID)
		if e.LeadID != "" {
			if _, exists := leadIDSet[e.LeadID]; !exists {
				leadIDs = append(leadIDs, e.LeadID)
				leadIDSet[e.LeadID] = struct{}{}
			}
		}
	}

	leadMap := make(map[string]*lead.Lead)
	if len(leadIDs) > 0 {
		leads, err := s.leadRepo.FindByIDs(workspaceID, leadIDs)
		if err == nil {
			for _, l := range leads {
				leadMap[l.ID] = l
			}
		}
	}

	windowMap := s.batchGetWindowStatus(campaignEntries)

	waEntryMap := make(map[string]*wce.WhatsAppCampaignEntry)
	if entryType == shared.EntryTypeWhatsApp && len(entryIDs) > 0 {
		waEntries, err := s.whatsappRepo.FindByIDs(entryIDs)
		if err == nil {
			for _, e := range waEntries {
				waEntryMap[e.ID] = e
			}
		}
	}

	entries := make([]conversation.InboxEntry, 0, len(campaignEntries))
	for _, e := range campaignEntries {
		var leadName, leadNumber, leadPicture string
		var leadBlocked bool
		if l, ok := leadMap[e.LeadID]; ok {
			leadName, leadNumber, leadPicture = l.Name, l.Number, l.ProfilePictureURL
			leadBlocked = l.Blocked
		}
		senderName, senderAvatar := s.getSenderInfo(e.LastMessageFrom, e.LastMessageType, leadName, leadNumber, leadPicture)

		windowOpen := false
		var windowExpiresAt *time.Time
		if ws, ok := windowMap[e.EntryID]; ok {
			windowOpen = ws.open
			windowExpiresAt = ws.expiresAt
		}

		var entryVariables []string
		automationEnabled := true
		var convStatus conversation.ConversationStatus
		var closeSource conversation.CloseSource
		var closeReason conversation.CloseReason
		var closedAt *time.Time
		if entryType == shared.EntryTypeWhatsApp {
			if waEntry, ok := waEntryMap[e.EntryID]; ok {
				entryVariables = waEntry.Variables
				automationEnabled = waEntry.IsAutomationEnabled()
				convStatus = conversation.ConversationStatus(waEntry.ConversationStatus)
				closeSource = conversation.CloseSource(waEntry.CloseSource)
				closeReason = conversation.CloseReason(waEntry.CloseReason)
				closedAt = waEntry.ClosedAt
			}
		}

		entries = append(entries, conversation.InboxEntry{
			EntryID:                 e.EntryID,
			EntryType:               string(e.EntryType),
			LeadID:                  e.LeadID,
			LeadName:                leadName,
			LeadNumber:              leadNumber,
			LeadPicture:             leadPicture,
			Blocked:                 leadBlocked,
			UnreadCount:             e.UnreadCount,
			LastMessagePreview:      s.formatMessagePreview(e),
			LastMessageAt:           e.LastMessageAt,
			LastMessageType:         s.getDisplayMessageType(e),
			LastMessageSender:       senderName,
			LastMessageSenderAvatar: senderAvatar,
			WindowOpen:              windowOpen,
			WindowExpiresAt:         windowExpiresAt,
			BusinessPhoneID:         e.BusinessPhoneID,
			AutomationEnabled:       automationEnabled,
			EntryVariables:          entryVariables,
			ConversationStatus:      convStatus,
			CloseSource:             closeSource,
			CloseReason:             closeReason,
			ClosedAt:                closedAt,
		})
	}

	s.enrichAssignments(entries, workspaceID)
	return entries, totalWithMessages, nil
}

func (s *HistoryProviderService) SearchInboxEntries(input conversation.SearchInboxInput) ([]conversation.InboxEntry, int64, error) {
	isGlobal := input.CampaignID == "" && input.WorkspaceID != ""

	if !isGlobal && (input.CampaignID == "" || input.CampaignType == "") {
		return nil, 0, fmt.Errorf("campaignID and campaignType are required")
	}

	var entryType shared.EntryType

	if isGlobal {
		switch input.CampaignType {
		case "whatsapp":
			entryType = shared.EntryTypeWhatsApp
		default:
			entryType = ""
		}
	} else {
		switch input.CampaignType {
		case "whatsapp":
			entryType = shared.EntryTypeWhatsApp
		default:
			return nil, 0, fmt.Errorf("invalid campaign type: %s", input.CampaignType)
		}
	}

	searchInput := conversation.SearchEntriesInput{
		CampaignID:             input.CampaignID,
		WorkspaceID:            input.WorkspaceID,
		WhatsAppCampaignType:   input.WhatsAppCampaignType,
		DepartmentIDs:          input.DepartmentIDs,
		RestrictDepartments:    input.RestrictDepartments,
		EntryType:              entryType,
		Query:                  input.Query,
		MessageSearch:          input.MessageSearch,
		StageIDs:               input.StageIDs,
		StageWorkspaceID:       input.StageWorkspaceID,
		MinMessageCount:        input.MinMessageCount,
		MaxMessageCount:        input.MaxMessageCount,
		HasUnread:              input.HasUnread,
		WindowOpen:             input.WindowOpen,
		Channel:                input.Channel,
		DateFrom:               input.DateFrom,
		DateTo:                 input.DateTo,
		ConversationStatus:     input.ConversationStatus,
		SortOrder:              input.SortOrder,
		AssignedUserID:         input.AssignedUserID,
		ResponsibleUserID:      input.ResponsibleUserID,
		ResponsibleUnassigned:  input.ResponsibleUnassigned,
		AssigneeOverrideUserID: input.AssigneeOverrideUserID,
		Page:                   input.Page,
		PageSize:               input.PageSize,
	}

	log.Printf("[HistoryProvider] Search: campaign=%s (%s), workspace=%s, query=%q, messageSearch=%q, channel=%q, sort=%s, page=%d",
		input.CampaignID, input.CampaignType, input.WorkspaceID, input.Query, input.MessageSearch, input.Channel, input.SortOrder, input.Page)

	results, totalCount, err := s.messageRepo.SearchEntriesWithMessages(searchInput)
	if err != nil {
		return nil, 0, fmt.Errorf("error searching entries: %w", err)
	}

	leadIDs := make([]string, 0, len(results))
	waEntryIDs := make([]string, 0)
	leadIDSet := make(map[string]struct{})
	for _, e := range results {
		if e.LeadID != "" {
			if _, exists := leadIDSet[e.LeadID]; !exists {
				leadIDs = append(leadIDs, e.LeadID)
				leadIDSet[e.LeadID] = struct{}{}
			}
		}
		switch e.EntryType {
		case shared.EntryTypeWhatsApp:
			waEntryIDs = append(waEntryIDs, e.EntryID)
		}
	}

	leadMap := make(map[string]*lead.Lead)
	if len(leadIDs) > 0 {
		leads, err := s.leadRepo.FindByIDs(input.WorkspaceID, leadIDs)
		if err == nil {
			for _, l := range leads {
				leadMap[l.ID] = l
			}
		}
	}

	windowMap := s.batchGetWindowStatus(results)

	waEntryMap := make(map[string]*wce.WhatsAppCampaignEntry)
	if len(waEntryIDs) > 0 {
		waEntries, err := s.whatsappRepo.FindByIDs(waEntryIDs)
		if err == nil {
			for _, e := range waEntries {
				waEntryMap[e.ID] = e
			}
		}
	}

	entries := make([]conversation.InboxEntry, 0, len(results))
	for _, e := range results {
		var leadName, leadNumber, leadPicture string
		var leadBlocked bool
		if l, ok := leadMap[e.LeadID]; ok {
			leadName, leadNumber, leadPicture = l.Name, l.Number, l.ProfilePictureURL
			leadBlocked = l.Blocked
		}
		senderName, senderAvatar := s.getSenderInfo(e.LastMessageFrom, e.LastMessageType, leadName, leadNumber, leadPicture)

		windowOpen := false
		var windowExpiresAt *time.Time
		if ws, ok := windowMap[e.EntryID]; ok {
			windowOpen = ws.open
			windowExpiresAt = ws.expiresAt
		}

		var entryVariables []string
		automationEnabled := true
		var convStatus conversation.ConversationStatus
		var closeSource conversation.CloseSource
		var closeReason conversation.CloseReason
		var closedAt *time.Time
		if e.EntryType == shared.EntryTypeWhatsApp {
			if waEntry, ok := waEntryMap[e.EntryID]; ok {
				entryVariables = waEntry.Variables
				automationEnabled = waEntry.IsAutomationEnabled()
				convStatus = conversation.ConversationStatus(waEntry.ConversationStatus)
				closeSource = conversation.CloseSource(waEntry.CloseSource)
				closeReason = conversation.CloseReason(waEntry.CloseReason)
				closedAt = waEntry.ClosedAt
			}
		}

		var matchedMessages []conversation.MatchedMessage
		for _, m := range e.MatchedMessages {
			matchedMessages = append(matchedMessages, conversation.MatchedMessage{
				MessageID:   m.MessageID,
				Text:        m.Text,
				From:        m.From,
				MessageType: string(m.MsgType),
				Channel:     string(m.Channel),
				CreatedAt:   m.CreatedAt,
				Position:    m.Position,
				Page:        int(m.Position/int64(conversation.DefaultHistoryPageSize)) + 1,
			})
		}

		entries = append(entries, conversation.InboxEntry{
			EntryID:                 e.EntryID,
			EntryType:               string(e.EntryType),
			CampaignID:              e.CampaignID,
			CampaignName:            e.CampaignName,
			LeadID:                  e.LeadID,
			LeadName:                leadName,
			LeadNumber:              leadNumber,
			LeadPicture:             leadPicture,
			Blocked:                 leadBlocked,
			EntryVariables:          entryVariables,
			UnreadCount:             e.UnreadCount,
			LastMessagePreview:      s.formatMessagePreview(e),
			LastMessageAt:           e.LastMessageAt,
			LastMessageType:         s.getDisplayMessageType(e),
			LastMessageSender:       senderName,
			LastMessageSenderAvatar: senderAvatar,
			WindowOpen:              windowOpen,
			WindowExpiresAt:         windowExpiresAt,
			BusinessPhoneID:         e.BusinessPhoneID,
			AutomationEnabled:       automationEnabled,
			MatchedMessages:         matchedMessages,
			TotalMatches:            e.TotalMatches,
			ConversationStatus:      convStatus,
			CloseSource:             closeSource,
			CloseReason:             closeReason,
			ClosedAt:                closedAt,
		})
	}

	s.enrichAssignments(entries, input.WorkspaceID)
	s.enrichAIHandlers(entries, results)
	return entries, totalCount, nil
}

// enrichAIHandlers attaches the effective AI handler (direct agent or workflow, plus
// the live workflow-run/current-node when running) to each inbox entry. Everything is
// batched over the visible page only — one query for active runs, one for agents, one
// for workflows — so it stays O(page) regardless of how many conversations exist.
func (s *HistoryProviderService) enrichAIHandlers(entries []conversation.InboxEntry, results []conversation.EntryWithLastMessage) {
	if len(entries) == 0 {
		return
	}

	agentIDs := map[string]struct{}{}
	workflowIDs := map[string]struct{}{}
	entryIDs := make([]string, 0, len(results))
	resultByEntry := make(map[string]conversation.EntryWithLastMessage, len(results))
	for _, r := range results {
		resultByEntry[r.EntryID] = r
		entryIDs = append(entryIDs, r.EntryID)
		if r.AgentResponsesEnabled && r.AgentID != "" {
			agentIDs[r.AgentID] = struct{}{}
		}
		if r.WorkflowEnabled && r.WorkflowID != "" {
			workflowIDs[r.WorkflowID] = struct{}{}
		}
	}

	var runByEntry map[string]*workflow.WorkflowRun
	if s.workflowRunRepo != nil {
		if runs, err := s.workflowRunRepo.FindActiveByEntries(entryIDs); err == nil {
			runByEntry = runs
			for _, run := range runs {
				if run != nil && run.WorkflowID != "" {
					workflowIDs[run.WorkflowID] = struct{}{}
				}
			}
		}
	}

	agentMap := map[string]*agent.Agent{}
	if len(agentIDs) > 0 {
		if agents, err := s.agentRepo.FindByIDs(mapKeys(agentIDs)); err == nil {
			for _, a := range agents {
				agentMap[a.ID] = a
			}
		}
	}

	workflowMap := map[string]*workflow.Workflow{}
	if s.workflowRepo != nil && len(workflowIDs) > 0 {
		if wfs, err := s.workflowRepo.FindByIDs(mapKeys(workflowIDs)); err == nil {
			for _, w := range wfs {
				workflowMap[w.ID] = w
			}
		}
	}

	for i := range entries {
		r := resultByEntry[entries[i].EntryID]
		var run *workflow.WorkflowRun
		if runByEntry != nil {
			run = runByEntry[entries[i].EntryID]
		}
		entries[i].AIHandler = buildAIHandler(r, run, agentMap, workflowMap)
	}
}

// buildAIHandler resolves the effective handler for one entry. Workflow beats a direct
// agent (mirrors the message-time rule in resolveAgentContext): a configured workflow —
// or a live run, even if the campaign flag was since toggled off — is the handler.
func buildAIHandler(r conversation.EntryWithLastMessage, run *workflow.WorkflowRun, agentMap map[string]*agent.Agent, workflowMap map[string]*workflow.Workflow) *conversation.AIHandler {
	hasWorkflow := r.WorkflowEnabled && r.WorkflowID != ""
	hasAgent := r.AgentResponsesEnabled && r.AgentID != ""

	if hasWorkflow || run != nil {
		h := &conversation.AIHandler{Kind: "workflow", WorkflowID: r.WorkflowID}
		if run != nil {
			h.WorkflowID = run.WorkflowID
			h.WorkflowRunID = run.ID
			h.RunStatus = string(run.Status)
			h.CurrentNodeID = run.CurrentNodeID
		}
		if wf, ok := workflowMap[h.WorkflowID]; ok {
			h.WorkflowName = wf.Name
			if h.CurrentNodeID != "" {
				if node := wf.Graph.FindNode(h.CurrentNodeID); node != nil {
					h.CurrentNodeType = string(node.Type)
				}
			}
		}
		return h
	}

	if hasAgent {
		h := &conversation.AIHandler{Kind: "agent", AgentID: r.AgentID}
		if a, ok := agentMap[r.AgentID]; ok {
			h.AgentName = a.Name
			h.AgentAvatar = a.AvatarURL
			h.AgentActive = a.IsActive
		}
		return h
	}

	return nil
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (s *HistoryProviderService) GetInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error) {
	e, err := s.messageRepo.GetEntryLastMessage(entryID, shared.EntryType(entryType))
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, nil
	}

	var workspaceID string
	switch shared.EntryType(entryType) {
	case shared.EntryTypeWhatsApp:
		if info, infoErr := s.whatsappRepo.GetCampaignForEntry(entryID); infoErr == nil && info != nil {
			workspaceID = info.WorkspaceID
		}
	}

	leadName, leadNumber, leadPicture, leadMetadata, leadBlocked := s.getLeadInfo(workspaceID, e.LeadID)
	senderName, senderAvatar := s.getSenderInfo(e.LastMessageFrom, e.LastMessageType, leadName, leadNumber, leadPicture)

	var entryVariables []string
	automationEnabled := true
	var convStatus conversation.ConversationStatus
	var closeSource conversation.CloseSource
	var closeReason conversation.CloseReason
	var closedAt *time.Time
	if e.EntryType == shared.EntryTypeWhatsApp {
		if waEntry, err := s.whatsappRepo.FindByID(e.EntryID); err == nil && waEntry != nil {
			entryVariables = waEntry.Variables
			automationEnabled = waEntry.IsAutomationEnabled()
			convStatus = conversation.ConversationStatus(waEntry.ConversationStatus)
			closeSource = conversation.CloseSource(waEntry.CloseSource)
			closeReason = conversation.CloseReason(waEntry.CloseReason)
			closedAt = waEntry.ClosedAt
		}
	}

	entry := &conversation.InboxEntry{
		EntryID:                 e.EntryID,
		EntryType:               string(e.EntryType),
		CampaignID:              e.CampaignID,
		CampaignName:            e.CampaignName,
		LeadID:                  e.LeadID,
		LeadName:                leadName,
		LeadNumber:              leadNumber,
		LeadPicture:             leadPicture,
		LeadMetadata:            leadMetadata,
		Blocked:                 leadBlocked,
		EntryVariables:          entryVariables,
		UnreadCount:             e.UnreadCount,
		LastMessagePreview:      s.formatMessagePreview(*e),
		LastMessageAt:           e.LastMessageAt,
		LastMessageType:         s.getDisplayMessageType(*e),
		LastMessageSender:       senderName,
		LastMessageSenderAvatar: senderAvatar,
		AutomationEnabled:       automationEnabled,
		ConversationStatus:      convStatus,
		CloseSource:             closeSource,
		CloseReason:             closeReason,
		ClosedAt:                closedAt,
	}

	windowOpen, windowExpiresAt := s.getWindowStatus(e.LeadID, e.BusinessPhoneID)
	entry.WindowOpen = windowOpen
	entry.WindowExpiresAt = windowExpiresAt
	entry.BusinessPhoneID = e.BusinessPhoneID

	// Reuse the same assignee enrichment the inbox list uses, so the build logic
	// stays in one place. entry_update broadcasts rebuild the list item from this
	// single-entry load, so without this the assigned agent would disappear
	// whenever an event (e.g. the inbound-call "Chamada recebida" message)
	// triggers an entry_update.
	enriched := []conversation.InboxEntry{*entry}
	s.enrichAssignments(enriched, workspaceID)
	s.enrichAIHandlers(enriched, []conversation.EntryWithLastMessage{*e})
	*entry = enriched[0]

	return entry, nil
}

func (s *HistoryProviderService) getLeadInfo(workspaceID, leadID string) (name, number, picture string, metadata map[string]interface{}, blocked bool) {
	if leadID == "" || workspaceID == "" {
		return "", "", "", nil, false
	}
	lead, err := s.leadRepo.FindByID(workspaceID, leadID)
	if err != nil || lead == nil {
		return "", "", "", nil, false
	}
	return lead.Name, lead.Number, lead.ProfilePictureURL, nil, lead.Blocked
}

func (s *HistoryProviderService) getWindowStatus(leadID, businessPhoneID string) (windowOpen bool, windowExpiresAt *time.Time) {
	if leadID == "" || s.messageWindowRepo == nil {
		return false, nil
	}

	if businessPhoneID == "" {
		windows, err := s.messageWindowRepo.FindAllByLead(leadID)
		if err != nil || len(windows) == 0 {
			return false, nil
		}
		for _, w := range windows {
			if w.IsWindowOpen() {
				expiresAt := w.WindowExpiresAt()
				return true, &expiresAt
			}
		}
		return false, nil
	}

	window, err := s.messageWindowRepo.FindByLeadAndBusinessPhone(leadID, businessPhoneID)
	if err != nil || window == nil {
		return false, nil
	}

	if window.IsWindowOpen() {
		expiresAt := window.WindowExpiresAt()
		return true, &expiresAt
	}

	return false, nil
}

type windowStatus struct {
	open      bool
	expiresAt *time.Time
}

func (s *HistoryProviderService) batchGetWindowStatus(entries []conversation.EntryWithLastMessage) map[string]windowStatus {
	result := make(map[string]windowStatus, len(entries))
	if s.messageWindowRepo == nil || len(entries) == 0 {
		return result
	}

	type entryRef struct {
		entryID string
		leadID  string
	}
	byBPhone := make(map[string][]entryRef)
	for _, e := range entries {
		if e.LeadID != "" && e.BusinessPhoneID != "" {
			byBPhone[e.BusinessPhoneID] = append(byBPhone[e.BusinessPhoneID], entryRef{e.EntryID, e.LeadID})
		}
	}

	for bphoneID, refs := range byBPhone {
		leadIDs := make([]string, len(refs))
		for i, ref := range refs {
			leadIDs[i] = ref.leadID
		}
		windowByLead, err := s.messageWindowRepo.FindOpenWindowsByLeadIDs(leadIDs, bphoneID)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			if w, ok := windowByLead[ref.leadID]; ok && w.IsWindowOpen() {
				expiresAt := w.WindowExpiresAt()
				result[ref.entryID] = windowStatus{open: true, expiresAt: &expiresAt}
			}
		}
	}

	return result
}

func (s *HistoryProviderService) getSenderInfo(from string, messageType conversation.MessageType, leadName, leadNumber, leadPicture string) (name, avatar string) {
	switch messageType {
	case conversation.MessageTypeUserMessage, conversation.MessageTypeAudio, conversation.MessageTypeMedia:
		if leadName != "" {
			return leadName, leadPicture
		}
		return leadNumber, leadPicture

	case conversation.MessageTypeAIResponse:
		if from != "" && s.agentRepo != nil {
			if _, err := uuid.Parse(from); err == nil {
				agent, err := s.agentRepo.FindByID(from)
				if err == nil && agent != nil {
					return agent.Name, agent.AvatarURL
				}
			}
		}
		return "Assistente", ""

	case conversation.MessageTypeSystem:
		return "Sistema", ""

	case conversation.MessageTypeToolCall, conversation.MessageTypeToolResult:
		return "Assistente", ""

	case conversation.MessageTypeOperator:
		if from != "" && s.userRepo != nil {
			if _, err := uuid.Parse(from); err == nil {
				user, err := s.userRepo.FindByID(from)
				if err == nil && user != nil {
					return user.Username, user.Picture
				}
			}
		}
		return "Operador", ""

	default:
		if from != "" && s.userRepo != nil {
			if _, err := uuid.Parse(from); err == nil {
				user, err := s.userRepo.FindByID(from)
				if err == nil && user != nil {
					return user.Username, user.Picture
				}
			}
		}
		return from, ""
	}
}

func (s *HistoryProviderService) formatMessagePreview(e conversation.EntryWithLastMessage) string {
	if e.HasMedia {
		switch e.MediaType {
		case conversation.MediaTypeImage:
			return "[imagem]"
		case conversation.MediaTypeVideo:
			return "[vídeo]"
		case conversation.MediaTypeAudio:
			return "[áudio]"
		case conversation.MediaTypeDocument:
			return "[documento]"
		case conversation.MediaTypeSticker:
			return "[sticker]"
		}
	}

	text := e.LastMessageText
	if len(text) > 50 {
		truncateAt := 47
		for i := 47; i > 30; i-- {
			if text[i] == ' ' {
				truncateAt = i
				break
			}
		}
		text = text[:truncateAt] + "..."
	}
	return text
}

func (s *HistoryProviderService) getDisplayMessageType(e conversation.EntryWithLastMessage) string {
	if e.HasMedia {
		return string(e.MediaType)
	}
	return "text"
}

type MessageMarkerService struct {
	messageRepo           conversation.MessageRepository
	whatsappClientFactory conversation.WhatsAppClientFactory
	whatsappRepo          wce.Repository
	typingMu              sync.Mutex
	lastTypingIndicator   map[string]time.Time
}

const whatsappTypingIndicatorThrottle = 8 * time.Second

func NewMessageMarkerService(
	messageRepo conversation.MessageRepository,
	whatsappClientFactory conversation.WhatsAppClientFactory,
	whatsappRepo wce.Repository,
) *MessageMarkerService {
	return &MessageMarkerService{
		messageRepo:           messageRepo,
		whatsappClientFactory: whatsappClientFactory,
		whatsappRepo:          whatsappRepo,
		lastTypingIndicator:   make(map[string]time.Time),
	}
}

func (s *MessageMarkerService) MarkAsRead(entryID string, entryType shared.EntryType, messageIDs []string, userID string) error {
	_, err := s.messageRepo.MarkAsRead(conversation.MarkAsReadInput{
		EntryID:    entryID,
		EntryType:  entryType,
		MessageIDs: messageIDs,
		ReadBy:     userID,
	})
	if err != nil {
		return err
	}

	go s.sendWhatsAppReadReceipts(entryID, string(entryType), messageIDs)

	return nil
}

func (s *MessageMarkerService) sendWhatsAppReadReceipts(entryID, entryType string, messageIDs []string) {
	if s.whatsappClientFactory == nil || len(messageIDs) == 0 {
		return
	}

	businessPhoneID := s.resolveBusinessPhoneID(entryID, entryType)
	if businessPhoneID == "" {
		return
	}

	whatsappClient, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		log.Printf("[MessageMarker] Failed to resolve WhatsApp client: %v", err)
		return
	}

	var latestWamid string
	for i := len(messageIDs) - 1; i >= 0; i-- {
		msg, err := s.messageRepo.GetByID(messageIDs[i])
		if err != nil || msg == nil {
			continue
		}
		if msg.MessageType.IsInbound() && msg.WhatsAppMessageID != nil && *msg.WhatsAppMessageID != "" {
			latestWamid = *msg.WhatsAppMessageID
			break
		}
	}

	if latestWamid == "" {
		return
	}

	if err := whatsappClient.MarkMessageAsRead(context.Background(), latestWamid); err != nil {
		log.Printf("[MessageMarker] Failed to send WhatsApp read receipt for %s: %v", latestWamid, err)
	} else {
		log.Printf("[MessageMarker] Sent WhatsApp read receipt for message %s", latestWamid)
	}
}

func (s *MessageMarkerService) SendTypingIndicator(entryID string, entryType shared.EntryType) error {
	if s == nil || s.whatsappClientFactory == nil || entryType != shared.EntryTypeWhatsApp {
		return nil
	}

	businessPhoneID := s.resolveBusinessPhoneID(entryID, string(entryType))
	if businessPhoneID == "" {
		log.Printf("[MessageMarker] Skipping WhatsApp typing indicator for %s:%s: no business phone resolved", entryType, entryID)
		return nil
	}

	latestWamid, err := s.findLatestInboundWhatsAppMessageID(entryID, entryType)
	if err != nil {
		return err
	}
	if latestWamid == "" {
		log.Printf("[MessageMarker] Skipping WhatsApp typing indicator for %s:%s: no inbound WhatsApp message id found", entryType, entryID)
		return nil
	}

	typingKey := fmt.Sprintf("%s:%s:%s", entryType, entryID, latestWamid)
	if !s.reserveTypingIndicator(typingKey, time.Now().UTC()) {

		return nil
	}

	whatsappClient, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		return fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}

	if err := whatsappClient.SendTypingIndicator(context.Background(), latestWamid); err != nil {
		return err
	}

	log.Printf("[MessageMarker] Sent WhatsApp typing indicator for message %s", latestWamid)
	return nil
}

func (s *MessageMarkerService) reserveTypingIndicator(key string, now time.Time) bool {
	s.typingMu.Lock()
	defer s.typingMu.Unlock()

	if lastSentAt, ok := s.lastTypingIndicator[key]; ok && now.Sub(lastSentAt) < whatsappTypingIndicatorThrottle {
		return false
	}

	s.lastTypingIndicator[key] = now
	return true
}

func (s *MessageMarkerService) findLatestInboundWhatsAppMessageID(entryID string, entryType shared.EntryType) (string, error) {
	if s.messageRepo == nil {
		return "", nil
	}

	messages, err := s.messageRepo.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   entryID,
		EntryType: entryType,
		Limit:     100,
	})
	if err != nil {
		return "", err
	}

	for _, msg := range messages {
		if msg == nil || !msg.MessageType.IsInbound() || msg.WhatsAppMessageID == nil {
			continue
		}
		wamid := strings.TrimSpace(*msg.WhatsAppMessageID)
		if wamid != "" {
			return wamid, nil
		}
	}

	return "", nil
}

func (s *MessageMarkerService) resolveBusinessPhoneID(entryID, entryType string) string {
	switch entryType {
	case "whatsapp":
		if s.whatsappRepo == nil {
			return ""
		}
		campaign, err := s.whatsappRepo.GetCampaignForEntry(entryID)
		if err != nil {
			log.Printf("[MessageMarker] Error getting campaign for entry %s: %v", entryID, err)
			return ""
		}
		return campaign.BusinessPhoneID
	}
	return ""
}

type MessageSenderService struct {
	messageRepo           conversation.MessageRepository
	leadRepo              lead.Repository
	whatsappRepo          wce.Repository
	messageWindowRepo     lmw.Repository
	mediaRepo             conversation.ConversationMediaRepository
	whatsappClientFactory conversation.WhatsAppClientFactory
	hub                   conversation.EventBroadcaster

	wcCampaignRepo wc.Repository
	aiService      ai.Service
	toolRegistry   toolsdomain.Service
	analysisRepo   analysisdomain.Repository
	stageRepo      stage.Repository
	sharedState    cache.SharedState

	callPermissionRepo callpermission.Repository
}

func (s *MessageSenderService) SetCallPermissionRepo(repo callpermission.Repository) {
	s.callPermissionRepo = repo
}

func NewMessageSenderService(
	messageRepo conversation.MessageRepository,
	leadRepo lead.Repository,
	whatsappRepo wce.Repository,
	messageWindowRepo lmw.Repository,
	mediaRepo conversation.ConversationMediaRepository,
	whatsappClientFactory conversation.WhatsAppClientFactory,
	hub conversation.EventBroadcaster,
	wcCampaignRepo wc.Repository,
	aiService ai.Service,
	toolRegistry toolsdomain.Service,
	analysisRepo analysisdomain.Repository,
	stageRepo stage.Repository,
	sharedState cache.SharedState,
) *MessageSenderService {
	return &MessageSenderService{
		messageRepo:           messageRepo,
		leadRepo:              leadRepo,
		whatsappRepo:          whatsappRepo,
		messageWindowRepo:     messageWindowRepo,
		mediaRepo:             mediaRepo,
		whatsappClientFactory: whatsappClientFactory,
		hub:                   hub,
		wcCampaignRepo:        wcCampaignRepo,
		aiService:             aiService,
		toolRegistry:          toolRegistry,
		analysisRepo:          analysisRepo,
		stageRepo:             stageRepo,
		sharedState:           sharedState,
	}
}

func (s *MessageSenderService) SendTextMessage(entryID, entryType, text, userID, replyToMessageID string) (*conversation.Message, error) {
	leadID, leadNumber, businessPhoneID, err := s.getEntryInfo(entryID, entryType)
	if err != nil {
		return nil, err
	}

	if err := s.checkMessageWindow(leadID, businessPhoneID); err != nil {
		return nil, err
	}

	whatsappClient, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		log.Printf("[MessageSender] Failed to resolve WhatsApp client for business phone %s: %v", businessPhoneID, err)
		return nil, fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}

	contextMessageID := s.resolveContextMessageID(replyToMessageID)

	output, err := whatsappClient.SendTextMessage(context.Background(), conversation.SendTextMessageInput{
		To:               leadNumber,
		Body:             text,
		ContextMessageID: contextMessageID,
	})
	if err != nil {
		log.Printf("[MessageSender] WhatsApp send failed: %v", err)
		return nil, err
	}

	now := time.Now().UTC()
	waMsgID := output.MessageID
	message := &conversation.Message{
		ID:                uuid.NewString(),
		EntryID:           entryID,
		EntryType:         shared.EntryType(entryType),
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       conversation.MessageTypeOperator,
		From:              userID,
		To:                leadNumber,
		Text:              text,
		Read:              true,
		ReadAt:            &now,
		ReadBy:            &userID,
		WhatsAppMessageID: &waMsgID,
		DeliveryStatus:    conversation.DeliveryStatusSent,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if replyToMessageID != "" {
		message.ReplyToMessageID = &replyToMessageID
	}
	message.Normalize()

	if err := s.messageRepo.Create(message); err != nil {
		log.Printf("[MessageSender] Failed to save message: %v", err)
		return nil, fmt.Errorf("failed to save message: %w", err)
	}

	log.Printf("[MessageSender] Sent message to %s, WhatsApp ID: %s", leadNumber, output.MessageID)

	if entryType == "whatsapp" {
		go s.maybeRunWhatsAppCampaignTools(context.Background(), entryID, entryType, leadNumber)
	}
	return message, nil
}

func (s *MessageSenderService) RequestCallPermission(input conversation.RequestCallPermissionInput) (*conversation.Message, error) {
	entryID := strings.TrimSpace(input.EntryID)
	entryType := strings.TrimSpace(input.EntryType)
	userID := strings.TrimSpace(input.SenderID)

	leadID, leadNumber, businessPhoneID, err := s.getEntryInfo(entryID, entryType)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(businessPhoneID) == "" {
		return nil, conversation.ErrWhatsAppCallNotConfigured
	}
	workspaceID, err := s.resolveEntryWorkspace(entryID, entryType)
	if err != nil {
		return nil, err
	}
	if err := s.checkMessageWindow(leadID, businessPhoneID); err != nil {
		return nil, err
	}

	body := strings.TrimSpace(input.BodyText)
	if body == "" {
		body = "Gostaríamos de ligar para você pelo WhatsApp. Você permite receber nossa chamada?"
	}

	whatsappClient, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}

	output, err := whatsappClient.SendCallPermissionRequest(context.Background(), conversation.SendCallPermissionRequestInput{
		To:       leadNumber,
		BodyText: body,
	})
	if err != nil {

		if strings.Contains(err.Error(), "138017") {
			return s.markCallPermissionGranted(workspaceID, entryID, entryType, leadID, leadNumber, businessPhoneID, nil), nil
		}
		return nil, err
	}

	now := time.Now().UTC()
	waMsgID := output.MessageID
	message := &conversation.Message{
		ID:                uuid.NewString(),
		EntryID:           entryID,
		EntryType:         shared.EntryType(entryType),
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       conversation.MessageTypeCallPermissionRequest,
		From:              userID,
		To:                leadNumber,
		Text:              body,
		Read:              true,
		ReadAt:            &now,
		ReadBy:            &userID,
		WhatsAppMessageID: &waMsgID,
		DeliveryStatus:    conversation.DeliveryStatusSent,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	message.Normalize()
	if err := s.messageRepo.Create(message); err != nil {
		return nil, fmt.Errorf("failed to save call permission request: %w", err)
	}

	if s.callPermissionRepo != nil {
		_ = s.callPermissionRepo.Upsert(&callpermission.CallPermission{
			WorkspaceID:     workspaceID,
			BusinessPhoneID: businessPhoneID,
			LeadID:          leadID,
			UserNumber:      leadNumber,
			Status:          callpermission.StatusPending,
			RequestedAt:     &now,
		})
	}

	if s.hub != nil {
		s.hub.BroadcastNewMessage(entryID, entryType, message)
	}
	return message, nil
}

// CallPermissionStatus reports the lead's current WhatsApp call-permission state
// for a conversation so the UI can enable or disable the call action. It resolves
// the lead and business phone the same way RequestCallPermission does, so a
// "granted" result here corresponds to the same permission a call would target.
func (s *MessageSenderService) CallPermissionStatus(entryID, entryType string) (conversation.CallPermissionStatus, error) {
	entryID = strings.TrimSpace(entryID)
	entryType = strings.TrimSpace(entryType)

	_, leadNumber, businessPhoneID, err := s.getEntryInfo(entryID, entryType)
	if err != nil {
		return conversation.CallPermissionStatus{}, err
	}

	// No WhatsApp business phone (or no permission store) means there is nothing to
	// call from — report "none" so the UI simply keeps the call action disabled.
	if strings.TrimSpace(businessPhoneID) == "" || s.callPermissionRepo == nil {
		return conversation.CallPermissionStatus{Status: "none"}, nil
	}

	perm, err := s.callPermissionRepo.FindByPhoneAndNumber(businessPhoneID, leadNumber)
	if err != nil {
		return conversation.CallPermissionStatus{}, err
	}
	if perm == nil {
		return conversation.CallPermissionStatus{Status: "none"}, nil
	}

	active := perm.IsActive(time.Now())
	status := string(perm.Status)
	// A granted permission whose window has lapsed reads as expired to the UI, so
	// the operator knows a fresh request is needed rather than seeing "granted".
	if perm.Status == callpermission.StatusGranted && !active {
		status = string(callpermission.StatusExpired)
	}

	return conversation.CallPermissionStatus{
		Status:    status,
		CanCall:   active,
		ExpiresAt: perm.ExpiresAt,
	}, nil
}

func (s *MessageSenderService) markCallPermissionGranted(workspaceID, entryID, entryType, leadID, leadNumber, businessPhoneID string, expiresAt *time.Time) *conversation.Message {
	now := time.Now().UTC()
	if s.callPermissionRepo != nil {
		_ = s.callPermissionRepo.Upsert(&callpermission.CallPermission{
			WorkspaceID:     workspaceID,
			BusinessPhoneID: businessPhoneID,
			LeadID:          leadID,
			UserNumber:      leadNumber,
			Status:          callpermission.StatusGranted,
			ExpiresAt:       expiresAt,
			RespondedAt:     &now,
		})
	}

	text := "O cliente já autorizou ligações pelo WhatsApp."
	if expiresAt != nil {
		text = "O cliente autorizou ligações pelo WhatsApp até " + expiresAt.Format("02/01/2006 15:04") + "."
	}
	message := &conversation.Message{
		ID:          uuid.NewString(),
		EntryID:     entryID,
		EntryType:   shared.EntryType(entryType),
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeCallPermissionGranted,
		From:        leadNumber,
		Text:        text,
		Read:        true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	message.Normalize()
	if err := s.messageRepo.Create(message); err != nil {
		return message
	}
	if s.hub != nil {
		s.hub.BroadcastNewMessage(entryID, entryType, message)
	}
	return message
}

func (s *MessageSenderService) SendMediaMessage(entryID, entryType, mediaID, mediaType, userID, replyToMessageID string, caption string) (*conversation.Message, error) {
	leadID, leadNumber, businessPhoneID, err := s.getEntryInfo(entryID, entryType)
	if err != nil {
		return nil, err
	}

	if err := s.checkMessageWindow(leadID, businessPhoneID); err != nil {
		return nil, err
	}

	whatsappClient, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		log.Printf("[MessageSender] Failed to resolve WhatsApp client for business phone %s: %v", businessPhoneID, err)
		return nil, fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}

	mediaRecord, err := s.mediaRepo.GetByID(mediaID)
	if err != nil {
		log.Printf("[MessageSender] Failed to get media record: %v", err)
		return nil, fmt.Errorf("media not found: %w", err)
	}

	if mediaRecord == nil {
		return nil, errors.New("media not found")
	}

	mediaData, mimeType, fileName, err := s.downloadMediaFromCDN(mediaRecord.URL, mediaRecord.OriginalFilename)
	if err != nil {
		log.Printf("[MessageSender] Failed to download media from CDN: %v", err)
		return nil, fmt.Errorf("failed to download media: %w", err)
	}

	var output *conversation.SendTextMessageOutput
	var waMediaID string

	contextMessageID := s.resolveContextMessageID(replyToMessageID)

	switch conversation.MediaType(mediaType) {
	case conversation.MediaTypeImage:
		waMediaID, err = whatsappClient.UploadImage(context.Background(), mediaData, fileName, mimeType)
		if err != nil {
			log.Printf("[MessageSender] Failed to upload image to WhatsApp: %v", err)
			return nil, fmt.Errorf("failed to upload image to WhatsApp: %w", err)
		}
		output, err = whatsappClient.SendImageMessage(context.Background(), conversation.SendImageMessageInput{
			To:               leadNumber,
			ImageID:          waMediaID,
			Caption:          caption,
			ContextMessageID: contextMessageID,
		})

	case conversation.MediaTypeVideo:
		waMediaID, err = whatsappClient.UploadMedia(context.Background(), mediaData, fileName, mimeType)
		if err != nil {
			log.Printf("[MessageSender] Failed to upload video to WhatsApp: %v", err)
			return nil, fmt.Errorf("failed to upload video to WhatsApp: %w", err)
		}
		output, err = s.sendVideoMessage(context.Background(), leadNumber, waMediaID, contextMessageID, caption, whatsappClient)

	case conversation.MediaTypeAudio:
		log.Printf("[MessageSender] Converting audio to OGG Opus format...")
		oggData, err := convertAudioToOGGOpus(mediaData)
		if err != nil {
			log.Printf("[MessageSender] Failed to convert audio: %v", err)
			return nil, fmt.Errorf("failed to convert audio: %w", err)
		}
		log.Printf("[MessageSender] Audio converted: %d bytes -> %d bytes", len(mediaData), len(oggData))

		output, err = whatsappClient.SendAudioBytes(context.Background(), leadNumber, oggData, "voice.ogg", contextMessageID)

	case conversation.MediaTypeDocument:
		waMediaID, err = whatsappClient.UploadMedia(context.Background(), mediaData, fileName, mimeType)
		if err != nil {
			log.Printf("[MessageSender] Failed to upload document to WhatsApp: %v", err)
			return nil, fmt.Errorf("failed to upload document to WhatsApp: %w", err)
		}
		output, err = s.sendDocumentMessage(context.Background(), leadNumber, waMediaID, fileName, contextMessageID, caption, whatsappClient)

	case conversation.MediaTypeSticker:
		waMediaID, err = whatsappClient.UploadMedia(context.Background(), mediaData, fileName, mimeType)
		if err != nil {
			log.Printf("[MessageSender] Failed to upload sticker to WhatsApp: %v", err)
			return nil, fmt.Errorf("failed to upload sticker to WhatsApp: %w", err)
		}
		output, err = s.sendStickerMessage(context.Background(), leadNumber, waMediaID, contextMessageID, whatsappClient)

	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	if err != nil {
		log.Printf("[MessageSender] WhatsApp send failed: %v", err)
		return nil, err
	}

	now := time.Now().UTC()
	mt := conversation.MediaType(mediaType)
	waMsgID := output.MessageID
	message := &conversation.Message{
		ID:                uuid.NewString(),
		EntryID:           entryID,
		EntryType:         shared.EntryType(entryType),
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       conversation.MessageTypeOperator,
		From:              userID,
		To:                leadNumber,
		Text:              caption,
		MediaID:           &mediaID,
		MediaType:         mt,
		Read:              true,
		ReadAt:            &now,
		ReadBy:            &userID,
		WhatsAppMessageID: &waMsgID,
		DeliveryStatus:    conversation.DeliveryStatusSent,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if replyToMessageID != "" {
		message.ReplyToMessageID = &replyToMessageID
	}
	message.Normalize()

	if err := s.messageRepo.Create(message); err != nil {
		log.Printf("[MessageSender] Failed to save message: %v", err)
		return nil, fmt.Errorf("failed to save message: %w", err)
	}

	log.Printf("[MessageSender] Sent %s media to %s, WhatsApp ID: %s", mediaType, leadNumber, output.MessageID)

	if entryType == "whatsapp" {
		go s.maybeRunWhatsAppCampaignTools(context.Background(), entryID, entryType, leadNumber)
	}
	return message, nil
}

func (s *MessageSenderService) SendButtonMessage(entryID, entryType, userID, replyToMessageID string, input conversation.SendButtonInput) (*conversation.Message, error) {
	leadID, leadNumber, businessPhoneID, err := s.getEntryInfo(entryID, entryType)
	if err != nil {
		return nil, err
	}

	if err := s.checkMessageWindow(leadID, businessPhoneID); err != nil {
		return nil, err
	}

	whatsappClient, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}

	buttons := make([]conversation.InteractiveButton, len(input.Buttons))
	for i, b := range input.Buttons {
		buttons[i] = conversation.InteractiveButton{
			Type:  conversation.ButtonTypeReply,
			ID:    b.ID,
			Title: b.Title,
		}
	}

	output, err := whatsappClient.SendButtonMessage(context.Background(), conversation.SendButtonMessageInput{
		To:         leadNumber,
		HeaderType: conversation.HeaderType(input.HeaderType),
		HeaderText: input.HeaderText,
		BodyText:   input.BodyText,
		FooterText: input.FooterText,
		Buttons:    buttons,
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	waMsgID := output.MessageID
	message := &conversation.Message{
		ID:                uuid.NewString(),
		EntryID:           entryID,
		EntryType:         shared.EntryType(entryType),
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       conversation.MessageTypeOperator,
		From:              userID,
		To:                leadNumber,
		Text:              input.BodyText,
		Read:              true,
		ReadAt:            &now,
		ReadBy:            &userID,
		WhatsAppMessageID: &waMsgID,
		DeliveryStatus:    conversation.DeliveryStatusSent,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if replyToMessageID != "" {
		message.ReplyToMessageID = &replyToMessageID
	}
	message.Normalize()

	if err := s.messageRepo.Create(message); err != nil {
		return nil, fmt.Errorf("failed to save message: %w", err)
	}

	log.Printf("[MessageSender] Sent button message to %s, WhatsApp ID: %s", leadNumber, output.MessageID)

	if entryType == "whatsapp" {
		go s.maybeRunWhatsAppCampaignTools(context.Background(), entryID, entryType, leadNumber)
	}
	return message, nil
}

func (s *MessageSenderService) maybeRunWhatsAppCampaignTools(_ context.Context, entryID, entryType, _ string) {
	if s.sharedState == nil {
		return
	}
	nowStr := time.Now().UTC().Format(time.RFC3339)
	if err := s.sharedState.HSet(AnalysisDebounceRedisKey, entryID, nowStr); err != nil {
		log.Printf("[MessageSender] failed to stamp analysis debounce for entry %s: %v", entryID, err)
	}
}

func (s *MessageSenderService) downloadMediaFromCDN(mediaURL, originalFilename string) ([]byte, string, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(mediaURL), nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid media URL: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("failed to download media: status=%d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, 32*1024*1024+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read media: %w", err)
	}

	if len(data) > 32*1024*1024 {
		return nil, "", "", fmt.Errorf("media size exceeds 32MB limit")
	}

	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" || mimeType == "application/zip" {
		mimeType = http.DetectContentType(data)
	}

	fileName := originalFilename
	if fileName == "" {
		fileName = path.Base(mediaURL)
		if fileName == "" || fileName == "." || fileName == "/" {
			fileName = "media"
		}
	}

	mimeType = fixMimeTypeByExtension(mimeType, fileName)

	return data, mimeType, fileName, nil
}

func fixMimeTypeByExtension(mimeType, fileName string) string {
	if mimeType != "" && mimeType != "application/octet-stream" && mimeType != "application/zip" {
		return mimeType
	}

	ext := strings.ToLower(path.Ext(fileName))
	switch ext {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".doc":
		return "application/msword"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/aac"
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	case ".amr":
		return "audio/amr"
	case ".mp4":
		return "video/mp4"
	case ".3gp":
		return "video/3gpp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	}

	return mimeType
}

func (s *MessageSenderService) sendVideoMessage(ctx context.Context, to, mediaID, contextMessageID, caption string, client conversation.WhatsAppClient) (*conversation.SendTextMessageOutput, error) {
	return client.SendVideoMessage(ctx, conversation.SendVideoMessageInput{
		To:               to,
		VideoID:          mediaID,
		Caption:          caption,
		ContextMessageID: contextMessageID,
	})
}

func (s *MessageSenderService) sendDocumentMessage(ctx context.Context, to, mediaID, fileName, contextMessageID, caption string, client conversation.WhatsAppClient) (*conversation.SendTextMessageOutput, error) {
	return client.SendDocumentMessage(ctx, conversation.SendDocumentMessageInput{
		To:               to,
		DocumentID:       mediaID,
		Filename:         fileName,
		Caption:          caption,
		ContextMessageID: contextMessageID,
	})
}

func (s *MessageSenderService) sendStickerMessage(ctx context.Context, to, mediaID, contextMessageID string, client conversation.WhatsAppClient) (*conversation.SendTextMessageOutput, error) {
	return client.SendStickerMessage(ctx, conversation.SendStickerMessageInput{
		To:               to,
		StickerID:        mediaID,
		ContextMessageID: contextMessageID,
	})
}

func (s *MessageSenderService) checkMessageWindow(leadID, businessPhoneID string) error {
	if leadID == "" {
		log.Printf("[MessageSender] Missing leadID for window check")
		return conversation.ErrWindowClosed
	}

	if businessPhoneID == "" {
		windows, err := s.messageWindowRepo.FindAllByLead(leadID)
		if err != nil || len(windows) == 0 {
			log.Printf("[MessageSender] No message windows found for lead %s", leadID)
			return conversation.ErrWindowClosed
		}
		for _, w := range windows {
			if w.IsWindowOpen() {
				return nil
			}
		}
		log.Printf("[MessageSender] All message windows closed for lead %s", leadID)
		return conversation.ErrWindowClosed
	}

	isOpen, err := s.messageWindowRepo.IsWindowOpen(leadID, businessPhoneID)
	if err != nil {
		log.Printf("[MessageSender] Error checking message window: %v", err)
		return conversation.ErrWindowClosed
	}

	if !isOpen {
		log.Printf("[MessageSender] 24-hour window closed for lead %s, business phone %s", leadID, businessPhoneID)
		return conversation.ErrWindowClosed
	}

	return nil
}

func (s *MessageSenderService) getEntryInfo(entryID, entryType string) (leadID, leadNumber, businessPhoneID string, err error) {
	switch entryType {
	case "whatsapp":
		entry, err := s.whatsappRepo.FindByID(entryID)
		if err != nil {
			return "", "", "", err
		}
		campaign, cErr := s.whatsappRepo.GetCampaignForEntry(entryID)
		if cErr != nil || campaign == nil {
			log.Printf("[MessageSender] Error getting campaign for entry %s: %v", entryID, cErr)
			return "", "", "", errors.New("unable to resolve workspace for whatsapp entry")
		}
		leadRecord, err := s.leadRepo.FindByID(campaign.WorkspaceID, entry.LeadID)
		if err != nil {
			return "", "", "", err
		}
		return entry.LeadID, leadRecord.Number, campaign.BusinessPhoneID, nil

	default:
		return "", "", "", errors.New("invalid entry type")
	}
}

func (s *MessageSenderService) resolveEntryWorkspace(entryID, entryType string) (string, error) {
	switch entryType {
	case "whatsapp":
		campaign, err := s.whatsappRepo.GetCampaignForEntry(entryID)
		if err != nil || campaign == nil || strings.TrimSpace(campaign.WorkspaceID) == "" {
			return "", errors.New("unable to resolve workspace for whatsapp entry")
		}
		return campaign.WorkspaceID, nil
	default:
		return "", errors.New("invalid entry type")
	}
}

func (s *MessageSenderService) resolveContextMessageID(replyToMessageID string) string {
	if replyToMessageID == "" || s.messageRepo == nil {
		return ""
	}
	msg, err := s.messageRepo.GetByID(replyToMessageID)
	if err != nil || msg == nil {
		log.Printf("[MessageSender] Could not resolve reply-to message %s: %v", replyToMessageID, err)
		return ""
	}
	if msg.WhatsAppMessageID != nil && *msg.WhatsAppMessageID != "" {
		return *msg.WhatsAppMessageID
	}
	log.Printf("[MessageSender] Reply-to message %s has no WhatsApp message ID", replyToMessageID)
	return ""
}

type sendConversationMessageUseCase struct {
	messageRepo           conversation.MessageRepository
	leadRepo              lead.Repository
	whatsappRepo          wce.Repository
	whatsappClientFactory conversation.WhatsAppClientFactory
	hub                   conversation.EventBroadcaster
	wcCampaignRepo        wc.Repository
	aiService             ai.Service
	toolRegistry          toolsdomain.Service
	analysisRepo          analysisdomain.Repository
	stageRepo             stage.Repository
	sharedState           cache.SharedState
}

func NewSendConversationMessageUseCase(
	messageRepo conversation.MessageRepository,
	leadRepo lead.Repository,
	whatsappRepo wce.Repository,
	whatsappClientFactory conversation.WhatsAppClientFactory,
	hub conversation.EventBroadcaster,
	wcCampaignRepo wc.Repository,
	aiService ai.Service,
	toolRegistry toolsdomain.Service,
	analysisRepo analysisdomain.Repository,
	stageRepo stage.Repository,
	sharedState cache.SharedState,
) conversation.SendConversationMessageUseCase {
	return &sendConversationMessageUseCase{
		messageRepo:           messageRepo,
		leadRepo:              leadRepo,
		whatsappRepo:          whatsappRepo,
		whatsappClientFactory: whatsappClientFactory,
		hub:                   hub,
		wcCampaignRepo:        wcCampaignRepo,
		aiService:             aiService,
		toolRegistry:          toolRegistry,
		analysisRepo:          analysisRepo,
		stageRepo:             stageRepo,
		sharedState:           sharedState,
	}
}

func (uc *sendConversationMessageUseCase) Execute(input conversation.SendMessageInput) (*conversation.Message, error) {
	input.EntryID = strings.TrimSpace(input.EntryID)
	input.EntryType = strings.TrimSpace(input.EntryType)
	input.Text = strings.TrimSpace(input.Text)
	input.SenderID = strings.TrimSpace(input.SenderID)

	if input.EntryID == "" || input.EntryType == "" {
		return nil, conversation.ErrConversationNotFound
	}

	if input.Text == "" && input.MediaID == nil {
		return nil, errors.New("text or media_id is required")
	}

	leadNumber, businessPhoneID, err := uc.getEntryInfo(input.EntryID, input.EntryType)
	if err != nil {
		return nil, conversation.ErrConversationNotFound
	}

	whatsappClient, err := uc.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		log.Printf("[SendMessage] Failed to resolve WhatsApp client for business phone %s: %v", businessPhoneID, err)
		return nil, fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}

	var contextMessageID string
	if input.ReplyToMessageID != "" && uc.messageRepo != nil {
		replyMsg, err := uc.messageRepo.GetByID(input.ReplyToMessageID)
		if err == nil && replyMsg != nil && replyMsg.WhatsAppMessageID != nil {
			contextMessageID = *replyMsg.WhatsAppMessageID
		}
	}

	var whatsappMsgID string
	if input.Text != "" {
		output, err := whatsappClient.SendTextMessage(context.Background(), conversation.SendTextMessageInput{
			To:               leadNumber,
			Body:             input.Text,
			ContextMessageID: contextMessageID,
		})
		if err != nil {
			log.Printf("[SendMessage] WhatsApp send failed: %v", err)
			return nil, err
		}
		whatsappMsgID = output.MessageID
	}

	now := time.Now().UTC()
	message := &conversation.Message{
		ID:                uuid.NewString(),
		EntryID:           input.EntryID,
		EntryType:         shared.EntryType(input.EntryType),
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       conversation.MessageTypeOperator,
		From:              input.SenderID,
		To:                leadNumber,
		Text:              input.Text,
		Read:              true,
		ReadAt:            &now,
		ReadBy:            &input.SenderID,
		WhatsAppMessageID: &whatsappMsgID,
		DeliveryStatus:    conversation.DeliveryStatusSent,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if input.ReplyToMessageID != "" {
		message.ReplyToMessageID = &input.ReplyToMessageID
	}

	if input.MediaID != nil {
		message.MediaID = input.MediaID
	}
	if input.MediaType != nil {
		message.MediaType = *input.MediaType
	}

	message.Normalize()

	if err := uc.messageRepo.Create(message); err != nil {
		log.Printf("[SendMessage] Failed to save message: %v", err)
		return nil, fmt.Errorf("failed to save message: %w", err)
	}

	if uc.hub != nil {
		uc.hub.BroadcastNewMessage(input.EntryID, input.EntryType, message)
	}

	log.Printf("[SendMessage] Sent to %s, WhatsApp ID: %s", leadNumber, whatsappMsgID)

	if input.EntryType == "whatsapp" {
		go uc.maybeRunWhatsAppCampaignTools(context.Background(), input.EntryID, input.EntryType, leadNumber)
	}

	return message, nil
}

func (uc *sendConversationMessageUseCase) getEntryInfo(entryID, entryType string) (leadNumber, businessPhoneID string, err error) {
	var leadID, workspaceID string

	switch entryType {
	case "whatsapp":
		entry, err := uc.whatsappRepo.FindByID(entryID)
		if err != nil {
			return "", "", err
		}
		leadID = entry.LeadID
		campaign, cErr := uc.whatsappRepo.GetCampaignForEntry(entryID)
		if cErr != nil || campaign == nil {
			return "", "", errors.New("unable to resolve workspace for whatsapp entry")
		}
		businessPhoneID = campaign.BusinessPhoneID
		workspaceID = campaign.WorkspaceID
	default:
		return "", "", errors.New("invalid entry type")
	}

	leadRecord, err := uc.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		return "", "", err
	}

	return leadRecord.Number, businessPhoneID, nil
}

func (uc *sendConversationMessageUseCase) maybeRunWhatsAppCampaignTools(_ context.Context, entryID, _, _ string) {
	if uc.sharedState == nil {
		return
	}
	nowStr := time.Now().UTC().Format(time.RFC3339)
	if err := uc.sharedState.HSet(AnalysisDebounceRedisKey, entryID, nowStr); err != nil {
		log.Printf("[SendMessage] failed to stamp analysis debounce for entry %s: %v", entryID, err)
	}
}

type uploadConversationMediaUseCase struct {
	mediaRepo   conversation.ConversationMediaRepository
	fileStorage media.FileStorage
}

func NewUploadConversationMediaUseCase(
	mediaRepo conversation.ConversationMediaRepository,
	fileStorage media.FileStorage,
) conversation.UploadConversationMediaUseCase {
	return &uploadConversationMediaUseCase{
		mediaRepo:   mediaRepo,
		fileStorage: fileStorage,
	}
}

func (uc *uploadConversationMediaUseCase) Execute(input conversation.UploadMediaInput) (*conversation.ConversationMedia, error) {
	input.EntryID = strings.TrimSpace(input.EntryID)
	input.EntryType = strings.TrimSpace(input.EntryType)
	input.Filename = strings.TrimSpace(input.Filename)

	if input.EntryID == "" || input.EntryType == "" {
		return nil, conversation.ErrConversationNotFound
	}

	if len(input.Data) == 0 {
		return nil, conversation.ErrMediaRequired
	}

	if !input.MediaType.Valid() {
		return nil, conversation.ErrMediaTypeInvalid
	}

	mediaID := uuid.NewString()
	key := fmt.Sprintf("conversations/%s/%s/%s", input.EntryType, input.EntryID, mediaID)

	if err := uc.fileStorage.UploadFile(key, input.Data); err != nil {
		return nil, err
	}

	url := uc.fileStorage.GetFileURL(key)

	mediaRecord := &conversation.ConversationMedia{
		ID:               mediaID,
		EntryID:          input.EntryID,
		EntryType:        shared.EntryType(input.EntryType),
		Type:             input.MediaType,
		MimeType:         input.MimeType,
		URL:              url,
		OriginalFilename: input.Filename,
		SizeBytes:        int64(len(input.Data)),
		CreatedAt:        time.Now().UTC(),
	}
	mediaRecord.Normalize()

	if err := uc.mediaRepo.Create(mediaRecord); err != nil {
		return nil, err
	}

	return mediaRecord, nil
}

type getConversationMediaUseCase struct {
	mediaRepo conversation.ConversationMediaRepository
}

func NewGetConversationMediaUseCase(
	mediaRepo conversation.ConversationMediaRepository,
) conversation.GetConversationMediaUseCase {
	return &getConversationMediaUseCase{
		mediaRepo: mediaRepo,
	}
}

func (uc *getConversationMediaUseCase) Execute(mediaID string) (*conversation.ConversationMedia, error) {
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return nil, conversation.ErrMediaNotFound
	}

	mediaRecord, err := uc.mediaRepo.GetByID(mediaID)
	if err != nil {
		return nil, conversation.ErrMediaNotFound
	}

	if mediaRecord == nil {
		return nil, conversation.ErrMediaNotFound
	}

	return mediaRecord, nil
}

func convertAudioToOGGOpus(audioData []byte) ([]byte, error) {
	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-map", "0:a:0",
		"-c:a", "libopus",
		"-b:a", "48k",
		"-ar", "48000",
		"-ac", "1",
		"-application", "voip",
		"-frame_duration", "20",
		"-f", "ogg",
		"pipe:1",
	)

	cmd.Stdin = bytes.NewReader(audioData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w, stderr: %s", err, stderr.String())
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty output")
	}

	return stdout.Bytes(), nil
}

type TemplateSenderService struct {
	whatsappClientFactory   conversation.WhatsAppClientFactory
	templateRepo            whatsappTemplate.Repository
	messageRepo             conversation.MessageRepository
	leadRepo                lead.Repository
	whatsappRepo            wce.Repository
	hub                     conversation.EventBroadcaster
	consumeWhatsappTemplate balance_domain.ConsumeWhatsappTemplateUseCase
}

func NewTemplateSenderService(
	whatsappClientFactory conversation.WhatsAppClientFactory,
	templateRepo whatsappTemplate.Repository,
	messageRepo conversation.MessageRepository,
	leadRepo lead.Repository,
	whatsappRepo wce.Repository,
	hub conversation.EventBroadcaster,
	consumeWhatsappTemplate balance_domain.ConsumeWhatsappTemplateUseCase,
) *TemplateSenderService {
	return &TemplateSenderService{
		whatsappClientFactory:   whatsappClientFactory,
		templateRepo:            templateRepo,
		messageRepo:             messageRepo,
		leadRepo:                leadRepo,
		whatsappRepo:            whatsappRepo,
		hub:                     hub,
		consumeWhatsappTemplate: consumeWhatsappTemplate,
	}
}

func (s *TemplateSenderService) SendTemplate(entryID, entryType, templateID string, parameters []string, userID string, workspaceID string) (string, error) {
	tmpl, err := s.templateRepo.FindByID(templateID)
	if err != nil {
		return "", fmt.Errorf("error finding template: %w", err)
	}
	if tmpl == nil {
		return "", fmt.Errorf("template not found: %s", templateID)
	}
	if tmpl.Status != "APPROVED" {
		return "", fmt.Errorf("template %s is not approved (status: %s)", tmpl.Name, tmpl.Status)
	}

	var leadID, businessPhoneID string
	switch shared.EntryType(entryType) {
	case shared.EntryTypeWhatsApp:
		entry, err := s.whatsappRepo.FindByID(entryID)
		if err != nil {
			return "", fmt.Errorf("error finding whatsapp entry: %w", err)
		}
		if entry == nil {
			return "", fmt.Errorf("whatsapp entry not found: %s", entryID)
		}
		leadID = entry.LeadID
		campaign, err := s.whatsappRepo.GetCampaignForEntry(entryID)
		if err == nil && campaign != nil {
			businessPhoneID = campaign.BusinessPhoneID
		}
	default:
		return "", fmt.Errorf("invalid entry type: %s", entryType)
	}

	leadRecord, err := s.leadRepo.FindByID(workspaceID, leadID)
	if err != nil || leadRecord == nil {
		return "", fmt.Errorf("lead not found for entry %s", entryID)
	}

	phoneNumber := leadRecord.Number
	if phoneNumber == "" {
		return "", fmt.Errorf("lead %s has no phone number", leadID)
	}

	if businessPhoneID == "" {
		return "", fmt.Errorf("no business phone associated with entry %s", entryID)
	}

	client, err := s.whatsappClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		return "", fmt.Errorf("error creating WhatsApp client: %w", err)
	}

	sendInput := conversation.SendTemplateMessageInput{
		To:           phoneNumber,
		TemplateName: tmpl.Name,
		Language:     tmpl.Language,
		Parameters:   parameters,
	}

	if tmpl.ParameterFormat == "named" {
		sendInput.IsNamedParameterFormat = true
		sendInput.ParameterNames = tmpl.GetParameterNames()
	}

	if tmpl.HeaderMediaURL != nil && *tmpl.HeaderMediaURL != "" {
		headerType := tmpl.GetHeaderFormat()
		if headerType != "" {
			sendInput.HeaderType = strings.ToLower(headerType)
			sendInput.HeaderMediaURL = *tmpl.HeaderMediaURL
		}
	}

	templateCategory, err := tmpl.BillingCategory()
	if err != nil {
		return "", fmt.Errorf("template billing category unavailable: %w", err)
	}
	_, consumeErr := s.consumeWhatsappTemplate.Execute(workspaceID, entryID, templateCategory)
	if consumeErr != nil {
		return "", fmt.Errorf("insufficient balance to send WhatsApp template: %w", consumeErr)
	}

	result, err := client.SendTemplateMessage(context.Background(), sendInput)
	if err != nil {

		if refundErr := s.consumeWhatsappTemplate.Refund(workspaceID, entryID, templateCategory); refundErr != nil {
			log.Printf("[TemplateSender] WARNING: failed to refund balance for workspace %s after send error: %v", workspaceID, refundErr)
		}
		return "", fmt.Errorf("error sending template: %w", err)
	}

	log.Printf("[TemplateSender] Sent template '%s' to %s for entry %s (%s), messageID=%s",
		tmpl.Name, phoneNumber, entryID, entryType, result.MessageID)

	bodyText := strings.TrimSpace(tmpl.GetBodyText())
	paramNames := tmpl.GetParameterNames()
	if bodyText != "" && len(parameters) > 0 {
		for i, p := range parameters {
			positional := fmt.Sprintf("{{%d}}", i+1)
			bodyText = strings.ReplaceAll(bodyText, positional, p)
			if i < len(paramNames) {
				named := "{{" + paramNames[i] + "}}"
				bodyText = strings.ReplaceAll(bodyText, named, p)
			}
		}
	}
	if bodyText == "" {
		bodyText = fmt.Sprintf("[Template: %s]", tmpl.Name)
	}

	type metadataButton struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		URL         string `json:"url,omitempty"`
		PhoneNumber string `json:"phoneNumber,omitempty"`
	}
	type metadataComponent struct {
		Type    string           `json:"type"`
		Format  string           `json:"format,omitempty"`
		Text    string           `json:"text,omitempty"`
		Buttons []metadataButton `json:"buttons,omitempty"`
	}
	type templateMetadata struct {
		TemplateName   string              `json:"template_name"`
		Language       string              `json:"language"`
		Category       string              `json:"category"`
		Components     []metadataComponent `json:"components"`
		HeaderMediaURL string              `json:"header_media_url,omitempty"`
	}

	var metaComponents []metadataComponent
	metaParamIdx := 0
	for _, comp := range tmpl.Components {
		mc := metadataComponent{
			Type:   comp.Type,
			Format: comp.Format,
			Text:   comp.Text,
		}

		if (comp.Type == "BODY" || (comp.Type == "HEADER" && comp.Format == "TEXT")) && mc.Text != "" && len(parameters) > 0 {
			for i := metaParamIdx; i < len(parameters); i++ {
				positional := fmt.Sprintf("{{%d}}", i+1)
				if strings.Contains(mc.Text, positional) {
					mc.Text = strings.ReplaceAll(mc.Text, positional, parameters[i])
					metaParamIdx = i + 1
					continue
				}
				if i < len(paramNames) {
					named := "{{" + paramNames[i] + "}}"
					if strings.Contains(mc.Text, named) {
						mc.Text = strings.ReplaceAll(mc.Text, named, parameters[i])
						metaParamIdx = i + 1
					}
				}
			}
		}

		if len(comp.Buttons) > 0 {
			for _, btn := range comp.Buttons {
				mc.Buttons = append(mc.Buttons, metadataButton{
					Type:        btn.Type,
					Text:        btn.Text,
					URL:         btn.URL,
					PhoneNumber: btn.PhoneNumber,
				})
			}
		}

		metaComponents = append(metaComponents, mc)
	}

	meta := templateMetadata{
		TemplateName: tmpl.Name,
		Language:     tmpl.Language,
		Category:     string(tmpl.Category),
		Components:   metaComponents,
	}
	if tmpl.HeaderMediaURL != nil {
		meta.HeaderMediaURL = *tmpl.HeaderMediaURL
	}

	var metadataJSON json.RawMessage
	if metaBytes, jsonErr := json.Marshal(meta); jsonErr == nil {
		metadataJSON = metaBytes
	}

	msgID := uuid.New().String()
	if result.MessageID != "" {
		msgID = result.MessageID
	}

	msg := &conversation.Message{
		ID:                msgID,
		EntryID:           entryID,
		EntryType:         shared.EntryType(entryType),
		Channel:           "whatsapp",
		MessageType:       conversation.MessageTypeTemplate,
		From:              userID,
		To:                phoneNumber,
		Text:              bodyText,
		WhatsAppMessageID: &result.MessageID,
		Metadata:          metadataJSON,
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.messageRepo.Create(msg); err != nil {
		log.Printf("[TemplateSender] Error recording template message: %v", err)
		return "", fmt.Errorf("failed to save template message: %w", err)
	}

	if s.hub != nil {
		s.hub.BroadcastNewMessage(entryID, entryType, msg)
	}

	return result.MessageID, nil
}
