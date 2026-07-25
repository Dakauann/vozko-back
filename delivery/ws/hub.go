package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	inbox_assignment "vozko/domain/inbox_assignment"
	"vozko/domain/messaging"
	"vozko/domain/metrics"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/domain/whatsapp_campaign"
	workspace_department "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

type workspaceDepartmentMemberLister interface {
	ListMembers(departmentID string) ([]workspace_department.DepartmentMember, error)
}

// memberVisibilityChecker reports whether a caller may assign a conversation to
// a target member, honouring department scoping. Satisfied by the workspace
// MemberVisibilityUseCase.
type memberVisibilityChecker interface {
	CanView(callerUserID, targetUserID, workspaceID string, isPlatformAdmin bool) (bool, error)
}

// conversationAssigner is the choke point for ownership mutations + history/telemetry.
// Implemented by usecases/inbox_assignment.AssignmentService.
type conversationAssigner interface {
	AssignOnOpen(entryID, entryType, businessPhoneID, workspaceID, userID string) (bool, error)
	AssignManual(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger string) error
}

// presenceRecorder records human attendant online/offline intervals (optional).
type presenceRecorder interface {
	Transition(workspaceID, userID string, state string, source string) error
}

// aiSessionEnder closes open AI attendance sessions on human takeover (optional).
type aiSessionEnder interface {
	EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string)
}

type entrySubscription struct {
	entryID   string
	entryType string
}

type incomingMessage struct {
	conn    *WSConnection
	message *WSIncomingMessage
}

type broadcastMessage struct {
	entryID       string
	entryType     string
	event         *WSOutgoingMessage
	excludeUserID string
	fromRedis     bool
}

const maxSentIDsPerSubscription = 1000

type ConversationHub struct {
	authorizer           conversation.ConversationAuthorizer
	userRepo             user.UserRepository
	waCampaignRepo       whatsapp_campaign.Repository
	workspaceResolver    conversation.CampaignWorkspaceResolver
	messageSender        conversation.MessageSender
	historyProvider      conversation.HistoryProvider
	messageMarker        conversation.MessageMarker
	StageProvider        conversation.StageProvider
	labelProvider        conversation.LabelProvider
	InitialStageAssigner conversation.InitialStageAssigner
	templateSender       conversation.TemplateSender
	callSource           conversation.CallSource
	billingPub           messaging.MessageQueuePub
	inboxService         conversation.InboxService
	messageRepo          conversation.MessageRepository
	assignmentRepo       inbox_assignment.Repository
	assignmentService    conversationAssigner
	presence             presenceRecorder
	aiSessions           aiSessionEnder
	analysisProvider     conversation.AnalysisProvider
	eventLogger          ce.Logger
	workspaceConfigRepo  wsc.Repository
	departmentRepo       workspaceDepartmentMemberLister
	memberVisibility     memberVisibilityChecker
	statusUpdater        conversation.ConversationStatusUpdater
	wsMetrics            metrics.WSMetricsRecorder

	connections map[string]*WSConnection
	connMu      sync.RWMutex

	userConnections map[string]map[string]bool

	userSubscriptions map[string]map[entrySubscription]bool
	entrySubscribers  map[entrySubscription]map[string]bool
	subMu             sync.RWMutex

	sentMessageIDs map[string]map[entrySubscription]map[string]bool

	register   chan *WSConnection
	unregister chan *WSConnection
	incoming   chan *incomingMessage
	broadcast  chan *broadcastMessage
	done       chan struct{}

	connUserDebounce   map[string]*time.Timer
	connUserDebounceMu sync.Mutex

	sharedState   cache.SharedState
	replicaID     string
	publicAddress string
}

func NewConversationHub(authorizer conversation.ConversationAuthorizer, userRepo user.UserRepository, sharedState cache.SharedState, replicaID, publicAddress string) *ConversationHub {
	hub := &ConversationHub{
		authorizer:        authorizer,
		userRepo:          userRepo,
		connections:       make(map[string]*WSConnection),
		userConnections:   make(map[string]map[string]bool),
		userSubscriptions: make(map[string]map[entrySubscription]bool),
		entrySubscribers:  make(map[entrySubscription]map[string]bool),
		sentMessageIDs:    make(map[string]map[entrySubscription]map[string]bool),
		register:          make(chan *WSConnection, 1024),
		unregister:        make(chan *WSConnection, 1024),
		incoming:          make(chan *incomingMessage, 8192),
		broadcast:         make(chan *broadcastMessage, 4096),
		done:              make(chan struct{}),
		connUserDebounce:  make(map[string]*time.Timer),
		sharedState:       sharedState,
		replicaID:         replicaID,
		publicAddress:     publicAddress,
	}
	return hub
}

func (h *ConversationHub) SetMessageSender(sender conversation.MessageSender) {
	h.messageSender = sender
}
func (h *ConversationHub) SetHistoryProvider(provider conversation.HistoryProvider) {
	h.historyProvider = provider
}
func (h *ConversationHub) SetMessageMarker(marker conversation.MessageMarker) {
	h.messageMarker = marker
}
func (h *ConversationHub) SetStageProvider(provider conversation.StageProvider) {
	h.StageProvider = provider
}
func (h *ConversationHub) SetLabelProvider(provider conversation.LabelProvider) {
	h.labelProvider = provider
}
func (h *ConversationHub) SetAnalysisProvider(provider conversation.AnalysisProvider) {
	h.analysisProvider = provider
}
func (h *ConversationHub) SetInitialStageAssigner(assigner conversation.InitialStageAssigner) {
	h.InitialStageAssigner = assigner
}
func (h *ConversationHub) SetTemplateSender(sender conversation.TemplateSender) {
	h.templateSender = sender
}
func (h *ConversationHub) SetCallSource(cs conversation.CallSource)    { h.callSource = cs }
func (h *ConversationHub) SetBillingPub(pub messaging.MessageQueuePub) { h.billingPub = pub }

func (h *ConversationHub) SetCampaignWorkspaceResolver(resolver conversation.CampaignWorkspaceResolver) {
	h.workspaceResolver = resolver
}
func (h *ConversationHub) SetInboxService(svc conversation.InboxService)      { h.inboxService = svc }
func (h *ConversationHub) SetMessageRepo(repo conversation.MessageRepository) { h.messageRepo = repo }
func (h *ConversationHub) SetWACampaignRepo(repo whatsapp_campaign.Repository) {
	h.waCampaignRepo = repo
}
func (h *ConversationHub) SetAssignmentRepo(repo inbox_assignment.Repository) {
	h.assignmentRepo = repo
}

func (h *ConversationHub) SetAssignmentService(svc conversationAssigner) {
	h.assignmentService = svc
}

func (h *ConversationHub) SetPresenceRecorder(p presenceRecorder) {
	h.presence = p
}

func (h *ConversationHub) SetAISessionEnder(e aiSessionEnder) {
	h.aiSessions = e
}

func (h *ConversationHub) SetMemberVisibility(checker memberVisibilityChecker) {
	h.memberVisibility = checker
}

func (h *ConversationHub) SetEventLogger(logger ce.Logger) {
	h.eventLogger = logger
}

func (h *ConversationHub) SetWorkspaceConfigRepo(repo wsc.Repository) {
	h.workspaceConfigRepo = repo
}
func (h *ConversationHub) SetWorkspaceDepartmentRepo(repo workspaceDepartmentMemberLister) {
	h.departmentRepo = repo
}
func (h *ConversationHub) SetConversationStatusUpdater(u conversation.ConversationStatusUpdater) {
	h.statusUpdater = u
}

func (h *ConversationHub) SetWSMetrics(rec metrics.WSMetricsRecorder) { h.wsMetrics = rec }

func (h *ConversationHub) GetEligibleUsersForWorkspace(workspaceID string, skipAdmins bool) []string {
	return h.collectEligibleUsers(workspaceID, nil, skipAdmins)
}

func (h *ConversationHub) GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID string, skipAdmins bool) []string {
	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" {
		return h.GetEligibleUsersForWorkspace(workspaceID, skipAdmins)
	}
	if h.departmentRepo == nil {
		log.Printf("[ConversationHub] missing department repository for workspace %s department %s", workspaceID, departmentID)
		return nil
	}

	members, err := h.departmentRepo.ListMembers(departmentID)
	if err != nil {
		log.Printf("[ConversationHub] error listing department members for %s: %v", departmentID, err)
		return nil
	}

	allowedUsers := make(map[string]bool, len(members))
	for _, member := range members {
		if member.UserID != "" {
			allowedUsers[member.UserID] = true
		}
	}

	return h.collectEligibleUsers(workspaceID, allowedUsers, skipAdmins)
}

func (h *ConversationHub) collectEligibleUsers(workspaceID string, allowedUsers map[string]bool, skipAdmins bool) []string {
	eligible := make(map[string]bool)

	h.connMu.RLock()
	for _, conn := range h.connections {
		if conn.WorkspaceID != workspaceID {
			continue
		}
		if eligible[conn.UserID] {
			continue
		}
		if allowedUsers != nil && !allowedUsers[conn.UserID] {
			continue
		}
		if h.authorizer == nil {
			continue
		}

		if skipAdmins && h.authorizer.IsWorkspaceOwnerOrAdmin(conn.UserID, workspaceID) {
			continue
		}

		if conn.IsAdmin && !h.authorizer.IsWorkspaceMember(conn.UserID, workspaceID) {
			continue
		}

		if h.authorizer.HasWorkspacePermission(conn.UserID, workspaceID, "conversations", "roulette", false) {
			eligible[conn.UserID] = true
		}
	}
	h.connMu.RUnlock()

	key := "hub:connected_users:" + workspaceID
	if members, err := h.sharedState.SMembers(key); err == nil {
		for _, m := range members {
			uid := m
			if idx := strings.Index(m, "|"); idx >= 0 {
				uid = m[:idx]
			}
			if allowedUsers != nil && !allowedUsers[uid] {
				continue
			}
			if eligible[uid] || h.authorizer == nil {
				continue
			}
			if skipAdmins && h.authorizer.IsWorkspaceOwnerOrAdmin(uid, workspaceID) {
				continue
			}
			if h.authorizer.HasWorkspacePermission(uid, workspaceID, "conversations", "roulette", false) {
				eligible[uid] = true
			}
		}
	}

	result := make([]string, 0, len(eligible))
	for uid := range eligible {
		result = append(result, uid)
	}
	return result
}

func (h *ConversationHub) Run() {

	go h.runRedisBroadcastSubscriber()
	go h.runRedisWorkspaceBroadcastSubscriber()
	go h.runReplicaHeartbeat()
	go h.runStaleReplicaCleanup()

	for {
		select {
		case conn := <-h.register:
			h.handleRegister(conn)
		case conn := <-h.unregister:
			h.handleUnregister(conn)
		case msg := <-h.incoming:
			h.handleIncoming(msg)
		case msg := <-h.broadcast:
			h.handleBroadcast(msg)
		case <-h.done:
			return
		}
	}
}

func (h *ConversationHub) Stop() {
	close(h.done)
}

func (h *ConversationHub) RegisterConnection(conn *WSConnection) {
	h.register <- conn
}

func (h *ConversationHub) UnregisterConnection(conn *WSConnection) {
	h.unregister <- conn
}

func (h *ConversationHub) HandleMessage(conn *WSConnection, msg *WSIncomingMessage) {
	h.incoming <- &incomingMessage{conn: conn, message: msg}
}

func (h *ConversationHub) isUserAdmin(userID string) bool {
	if connIDs, ok := h.userConnections[userID]; ok {
		for connID := range connIDs {
			if conn, exists := h.connections[connID]; exists {
				return conn.IsAdmin
			}
		}
	}
	return false
}

func (h *ConversationHub) ensureInitialTag(entryID, entryType string) {
	if h.workspaceResolver == nil || h.InitialStageAssigner == nil {
		return
	}
	workspaceID, err := h.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil {
		log.Printf("[ConversationHub] ensureInitialTag: error resolving workspace for %s (%s): %v", entryID, entryType, err)
		return
	}
	if workspaceID == "" {
		log.Printf("[ConversationHub] ensureInitialTag: empty workspaceID for %s (%s)", entryID, entryType)
		return
	}
	campaignID, _ := h.workspaceResolver.GetEntryCampaignID(entryID, entryType)
	if campaignID == "" {
		log.Printf("[ConversationHub] ensureInitialTag: empty campaignID for %s (%s)", entryID, entryType)
		return
	}
	h.InitialStageAssigner.AutoAssignInitialStage(workspaceID, campaignID, entryType, entryID, entryType)
}

func (h *ConversationHub) BroadcastNewMessage(entryID, entryType string, message *conversation.Message) {
	h.ensureInitialTag(entryID, entryType)

	if h.statusUpdater != nil && message != nil {
		if err := h.statusUpdater.TransitionOnMessage(entryID, entryType, message.MessageType); err != nil {
			log.Printf("[ConversationHub] Error transitioning conversation status for %s (%s): %v", entryID, entryType, err)
		}
	}

	h.broadcast <- &broadcastMessage{
		entryID:   entryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type: WSEventMessage,
			Payload: MessagePayload{
				EntryID:   entryID,
				EntryType: entryType,
				Message:   message,
			},
		},
	}

	go h.BroadcastEntryUpdate(entryID, entryType, message)
}

func (h *ConversationHub) BroadcastMessageSent(userID, requestID, entryID, entryType string, message *conversation.Message) {
	h.broadcast <- &broadcastMessage{
		entryID:   entryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type: WSEventMessageSent,
			Payload: MessageSentPayload{
				RequestID: requestID,
				EntryID:   entryID,
				EntryType: entryType,
				Message:   message,
			},
		},
	}
}

func (h *ConversationHub) BroadcastMessageError(userID, requestID, entryID, entryType, errorMsg, errorCode string) {
	msg := &WSOutgoingMessage{
		Type: WSEventMessageError,
		Payload: MessageErrorPayload{
			RequestID: requestID,
			EntryID:   entryID,
			EntryType: entryType,
			Error:     errorMsg,
			Code:      errorCode,
		},
	}

	h.connMu.RLock()
	defer h.connMu.RUnlock()

	connIDs := h.userConnections[userID]
	for connID := range connIDs {
		if conn, exists := h.connections[connID]; exists {
			h.sendToConnection(conn, msg)
		}
	}
}

func (h *ConversationHub) BroadcastRead(entryID, entryType string, messageIDs []string, readBy string, readAt time.Time) {
	h.broadcast <- &broadcastMessage{
		entryID:   entryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type: WSEventRead,
			Payload: ReadPayload{
				EntryID:    entryID,
				EntryType:  entryType,
				MessageIDs: messageIDs,
				ReadBy:     readBy,
				ReadAt:     readAt,
			},
		},
	}
}

func (h *ConversationHub) BroadcastUnreadCount(entryID, entryType string, count int64) {
	h.broadcast <- &broadcastMessage{
		entryID:   entryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type: WSEventUnreadCount,
			Payload: UnreadCountPayload{
				EntryID:     entryID,
				EntryType:   entryType,
				UnreadCount: count,
			},
		},
	}
}

func (h *ConversationHub) BroadcastTyping(entryID, entryType, fromUserID string, isTyping bool) {
	h.broadcast <- &broadcastMessage{
		entryID:       entryID,
		entryType:     entryType,
		excludeUserID: fromUserID,
		event: &WSOutgoingMessage{
			Type: WSEventTypingRemote,
			Payload: TypingPayload{
				EntryID:   entryID,
				EntryType: entryType,
				UserID:    fromUserID,
				IsTyping:  isTyping,
			},
		},
	}
}

func (h *ConversationHub) BroadcastStageUpdate(workspaceID, entryID, entryType string) {
	h.broadcastStageUpdateLocal(workspaceID, entryID, entryType)
	h.publishWorkspaceBroadcast("stage_update", entryID, entryType, workspaceID, "", "")
}

func (h *ConversationHub) broadcastStageUpdateLocal(workspaceID, entryID, entryType string) {
	if h.StageProvider == nil || h.authorizer == nil {
		return
	}

	stage, err := h.StageProvider.GetEntryStage(entryID, entryType, workspaceID)
	if err != nil {
		log.Printf("[ConversationHub] Error getting stage for entry %s: %v", entryID, err)
		return
	}

	event := &WSOutgoingMessage{
		Type: WSEventStageUpdate,
		Payload: StageUpdatePayload{
			EntryID:   entryID,
			EntryType: entryType,
			Stage:     stage,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling tag update: %v", err)
		return
	}

	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for _, connIDs := range h.userConnections {
		for connID := range connIDs {
			if conn, exists := h.connections[connID]; exists {
				if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, entryID, entryType, conn.IsAdmin) {
					continue
				}
				select {
				case conn.Send <- data:
				default:
					log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", conn.UserID, connID)
				}
			}
		}
	}
}

func (h *ConversationHub) BroadcastLabelUpdate(workspaceID, entryID, entryType string) {
	h.broadcastLabelUpdateLocal(workspaceID, entryID, entryType)
	h.publishWorkspaceBroadcast("label_update", entryID, entryType, workspaceID, "", "")
}

func (h *ConversationHub) broadcastLabelUpdateLocal(workspaceID, entryID, entryType string) {
	if h.labelProvider == nil || h.authorizer == nil {
		return
	}

	labels, err := h.labelProvider.GetEntryLabels(entryID, entryType, workspaceID)
	if err != nil {
		log.Printf("[ConversationHub] Error getting labels for entry %s: %v", entryID, err)
		return
	}

	event := &WSOutgoingMessage{
		Type: WSEventLabelUpdate,
		Payload: LabelUpdatePayload{
			EntryID:   entryID,
			EntryType: entryType,
			Labels:    labels,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling label update: %v", err)
		return
	}

	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for _, connIDs := range h.userConnections {
		for connID := range connIDs {
			if conn, exists := h.connections[connID]; exists {
				if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, entryID, entryType, conn.IsAdmin) {
					continue
				}
				select {
				case conn.Send <- data:
				default:
					log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", conn.UserID, connID)
				}
			}
		}
	}
}

func (h *ConversationHub) BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message) {
	h.broadcastEntryUpdateLocal(entryID, entryType, message)
	h.publishWorkspaceBroadcast("entry_update", entryID, entryType, "", "", "")
}

func (h *ConversationHub) broadcastEntryUpdateLocal(entryID, entryType string, message *conversation.Message) {
	if h.authorizer == nil {
		return
	}

	// The inbox service builds a fully-enriched entry (lead/sender/window +
	// assignee + stage + labels + analysis) in one place, so the delivery layer
	// just broadcasts it. Fall back to the base history provider when the inbox
	// service isn't wired (e.g. focused unit tests).
	var entry *conversation.InboxEntry
	var err error
	switch {
	case h.inboxService != nil:
		entry, err = h.inboxService.BuildInboxEntry(entryID, entryType)
	case h.historyProvider != nil:
		entry, err = h.historyProvider.GetInboxEntry(entryID, entryType)
	default:
		return
	}
	if err != nil {
		log.Printf("[ConversationHub] Error building inbox entry %s (%s): %v", entryID, entryType, err)
		return
	}
	if entry == nil {
		log.Printf("[ConversationHub] BroadcastEntryUpdate: entry %s (%s) not found", entryID, entryType)
		return
	}

	// Workspace/campaign are still needed for the connection filtering below
	// (not for enrichment — the inbox service already handled that).
	var workspaceID string
	if h.workspaceResolver != nil {
		workspaceID, _ = h.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	}
	var entryCampaignID string
	if h.workspaceResolver != nil {
		entryCampaignID, _ = h.workspaceResolver.GetEntryCampaignID(entryID, entryType)
	}

	assignedUserID := entry.AssignedUserID

	event := &WSOutgoingMessage{
		Type:    WSEventEntryUpdate,
		Payload: EntryUpdatePayload{Entry: *entry},
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling entry update: %v", err)
		return
	}

	h.connMu.RLock()

	for _, connIDs := range h.userConnections {
		for connID := range connIDs {
			if conn, exists := h.connections[connID]; exists {
				if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, entryID, entryType, conn.IsAdmin) {
					continue
				}

				if workspaceID != "" && conn.WorkspaceID != "" && conn.WorkspaceID != workspaceID {
					continue
				}

				if entryCampaignID != "" && conn.CampaignID != "" && conn.CampaignID != entryCampaignID {
					continue
				}
				if conn.CampaignType != "" && conn.CampaignType != entryType {
					continue
				}

				if conn.ConversationStatus != "" && string(entry.ConversationStatus) != conn.ConversationStatus {
					continue
				}

				if assignedUserID != "" && conn.UserID != assignedUserID && !conn.IsAdmin {
					// TODO: fix this check to be more efficient, doing it for every connection is not optimal, we should check the permissions once and cache it for the duration of the broadcast.
					if !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "conversations", "view_others", conn.IsAdmin) {
						continue
					}
				}
				select {
				case conn.Send <- data:
				default:
					log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", conn.UserID, connID)
				}
			}
		}
	}

	h.connMu.RUnlock()
	h.broadcastConversationStatusCountsUpdateLocal(workspaceID, entryCampaignID, entryType)
}

func (h *ConversationHub) broadcastConversationStatusCountsUpdateLocal(workspaceID, entryCampaignID, entryType string) {
	if h.inboxService == nil {
		return
	}

	type scopeKey struct {
		workspaceID string
		campaignID  string
		entryType   string
	}

	countsCache := make(map[scopeKey]map[string]int64)

	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for _, connIDs := range h.userConnections {
		for connID := range connIDs {
			conn, exists := h.connections[connID]
			if !exists {
				continue
			}
			if workspaceID != "" && conn.WorkspaceID != "" && conn.WorkspaceID != workspaceID {
				continue
			}
			if entryCampaignID != "" && conn.ViewMode == "campaign" && conn.CampaignID != "" && conn.CampaignID != entryCampaignID {
				continue
			}
			if entryType != "" && conn.CampaignType != "" && conn.CampaignType != entryType {
				continue
			}

			key := scopeKey{entryType: conn.CampaignType}
			if conn.ViewMode == "campaign" && conn.CampaignID != "" {
				key.campaignID = conn.CampaignID
			} else {
				key.workspaceID = conn.WorkspaceID
			}

			counts, ok := countsCache[key]
			if !ok {
				var err error
				counts, err = h.inboxService.GetConversationStatusCounts(key.workspaceID, key.campaignID, key.entryType)
				if err != nil {
					log.Printf("[ConversationHub] Error loading conversation status counts for workspace=%s campaign=%s type=%s: %v", key.workspaceID, key.campaignID, key.entryType, err)
					continue
				}
				countsCache[key] = counts
			}

			h.sendToConnection(conn, &WSOutgoingMessage{
				Type: WSEventConversationStatusCountsUpdate,
				Payload: ConversationStatusCountsUpdatePayload{
					Counts: counts,
				},
			})
		}
	}
}

func (h *ConversationHub) tryAssignOnOpen(conn *WSConnection, entryID, entryType string) {
	if h.workspaceResolver == nil {
		return
	}
	// Prefer assignment service (history + timeline). Fall back to raw repo for tests.
	if h.assignmentService == nil && h.assignmentRepo == nil {
		return
	}

	workspaceID, err := h.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || workspaceID == "" {
		return
	}

	if h.assignmentRepo != nil {
		existing, err := h.assignmentRepo.FindByEntry(workspaceID, entryID, entryType)
		if err != nil || existing != nil {
			return
		}
	}

	if conn.IsAdmin && h.authorizer != nil && !h.authorizer.IsWorkspaceMember(conn.UserID, workspaceID) {
		return
	}

	if h.workspaceConfigRepo != nil && h.authorizer != nil && h.authorizer.IsWorkspaceOwnerOrAdmin(conn.UserID, workspaceID) {
		if cfg, err := h.workspaceConfigRepo.GetByWorkspaceID(context.Background(), workspaceID); err == nil && cfg != nil && cfg.SkipAdminAssignment {
			return
		}
	}

	if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(conn.UserID, workspaceID, "conversations", "roulette", false) {
		return
	}

	businessPhoneID := ""
	if entryType == "whatsapp" && h.waCampaignRepo != nil {
		campaignID, _ := h.workspaceResolver.GetEntryCampaignID(entryID, entryType)
		if campaignID != "" {
			if wc, err := h.waCampaignRepo.FindByID(campaignID); err == nil && wc != nil {
				businessPhoneID = wc.BusinessPhoneID
			}
		}
	}

	if h.assignmentService != nil {
		ok, err := h.assignmentService.AssignOnOpen(entryID, entryType, businessPhoneID, workspaceID, conn.UserID)
		if err != nil {
			log.Printf("[ConversationHub] Error assigning entry %s to user %s on open: %v", entryID, conn.UserID, err)
			return
		}
		if !ok {
			return
		}
	} else {
		assignment := &inbox_assignment.InboxAssignment{
			WorkspaceID:     workspaceID,
			BusinessPhoneID: businessPhoneID,
			EntryID:         entryID,
			EntryType:       entryType,
			AssignedUserID:  conn.UserID,
		}
		if err := h.assignmentRepo.Assign(assignment); err != nil {
			log.Printf("[ConversationHub] Error assigning entry %s to user %s on open: %v", entryID, conn.UserID, err)
			return
		}
		if h.eventLogger != nil {
			h.eventLogger.Log(ce.New(workspaceID, entryID, entryType, ce.EventAutoAssigned).
				WithActorHuman(conn.UserID).
				WithDetails(map[string]string{"to_user_id": conn.UserID, "trigger": "open"}).
				Build())
		}
	}

	log.Printf("[ConversationHub] Auto-assigned entry %s (%s) → user %s (opened)", entryID, entryType, conn.UserID)

	go h.BroadcastEntryRemoved(entryID, entryType, workspaceID, conn.UserID)
}

func (h *ConversationHub) BroadcastEntryRemoved(entryID, entryType, workspaceID, excludeUserID string) {
	h.broadcastEntryRemovedLocal(entryID, entryType, workspaceID, excludeUserID)
	h.publishWorkspaceBroadcast("entry_removed", entryID, entryType, "", workspaceID, excludeUserID)
}

func (h *ConversationHub) broadcastEntryRemovedLocal(entryID, entryType, workspaceID, excludeUserID string) {
	if h.authorizer == nil {
		return
	}

	event := &WSOutgoingMessage{
		Type: WSEventEntryRemoved,
		Payload: EntryRemovedPayload{
			EntryID:   entryID,
			EntryType: entryType,
			Reason:    "assigned",
		},
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for _, connIDs := range h.userConnections {
		for connID := range connIDs {
			if conn, exists := h.connections[connID]; exists {

				if conn.UserID == excludeUserID {
					continue
				}

				if conn.WorkspaceID != workspaceID {
					continue
				}
				if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, entryID, entryType, conn.IsAdmin) {
					continue
				}
				if h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "conversations", "view_others", conn.IsAdmin) {
					continue
				}
				select {
				case conn.Send <- data:
				default:
				}
			}
		}
	}
}

func (h *ConversationHub) BroadcastMessageStatus(entryID, entryType, messageID string, status conversation.DeliveryStatus) {
	h.broadcast <- &broadcastMessage{
		entryID:   entryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type: WSEventMessageStatus,
			Payload: MessageStatusPayload{
				EntryID:   entryID,
				EntryType: entryType,
				MessageID: messageID,
				Status:    string(status),
			},
		},
	}
}

func (h *ConversationHub) BroadcastAnalysisUpdate(entryID, entryType string, analysis interface{}) {
	h.broadcast <- &broadcastMessage{
		entryID:   entryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type: WSEventAnalysisUpdate,
			Payload: AnalysisUpdatePayload{
				EntryID:   entryID,
				EntryType: entryType,
				Analysis:  analysis,
			},
		},
	}
}

func (h *ConversationHub) handleRegister(conn *WSConnection) {
	if conn.CampaignID != "" && conn.CampaignWorkspaceID == "" && h.workspaceResolver != nil {
		if wsID, err := h.workspaceResolver.GetCampaignWorkspaceID(conn.CampaignID, conn.CampaignType); err == nil && wsID != "" {
			conn.CampaignWorkspaceID = wsID
		}
	}
	if conn.CampaignWorkspaceID == "" {
		conn.CampaignWorkspaceID = conn.WorkspaceID
	}

	conn.WorkspaceID = conn.CampaignWorkspaceID

	if h.authorizer != nil && !conn.IsAdmin {
		if !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "conversations", "read", false) {
			log.Printf("[ConversationHub] User %s denied access to workspace %s conversations", conn.UserID, conn.WorkspaceID)
			h.sendToConnection(conn, &WSOutgoingMessage{
				Type: WSEventError,
				Payload: map[string]string{
					"code":    "forbidden",
					"message": "You don't have permission to view conversations in this workspace",
				},
			})

			go func() {
				time.Sleep(500 * time.Millisecond)
				conn.Conn.Close()
			}()
			return
		}
	}
	if h.authorizer != nil && conn.CampaignID != "" && conn.CampaignType != "" {
		if !h.authorizer.CanAccessCampaign(conn.UserID, conn.WorkspaceID, conn.CampaignID, conn.CampaignType, conn.IsAdmin) {
			log.Printf("[ConversationHub] User %s denied access to campaign %s (%s)", conn.UserID, conn.CampaignID, conn.CampaignType)
			h.sendToConnection(conn, &WSOutgoingMessage{
				Type: WSEventError,
				Payload: map[string]string{
					"code":    "forbidden",
					"message": "You don't have access to this inbox",
				},
			})
			go func() {
				time.Sleep(500 * time.Millisecond)
				conn.Conn.Close()
			}()
			return
		}
	}

	h.connMu.Lock()
	h.connections[conn.ID] = conn

	if h.userConnections[conn.UserID] == nil {
		h.userConnections[conn.UserID] = make(map[string]bool)
	}
	firstConn := len(h.userConnections[conn.UserID]) == 0
	h.userConnections[conn.UserID][conn.ID] = true
	h.connMu.Unlock()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointConversations)

	member := conn.UserID + "|" + h.replicaID
	_ = h.sharedState.SAdd("hub:connected_users:"+conn.WorkspaceID, member)
	_ = h.sharedState.SAdd("hub:workspaces", conn.WorkspaceID)

	// First connection for this user → durable online presence (no UX change).
	if firstConn && h.presence != nil {
		_ = h.presence.Transition(conn.WorkspaceID, conn.UserID, "online", "ws_hub")
	}

	h.subMu.Lock()
	if h.userSubscriptions[conn.UserID] == nil {
		h.userSubscriptions[conn.UserID] = make(map[entrySubscription]bool)
	}
	h.subMu.Unlock()

	h.sendToConnection(conn, &WSOutgoingMessage{
		Type:    WSEventConnected,
		Payload: map[string]string{"user_id": conn.UserID, "connection_id": conn.ID},
	})

	if h.inboxService != nil {

		viewMode := conn.ViewMode
		userID := conn.UserID
		workspaceID := conn.WorkspaceID
		departmentID := conn.DepartmentID
		campaignType := conn.CampaignType
		whatsAppCampaignType := conn.WhatsAppCampaignType
		campaignID := conn.CampaignID
		campaignWorkspaceID := conn.CampaignWorkspaceID
		conversationStatus := conn.ConversationStatus
		isAdmin := conn.IsAdmin
		seq := conn.viewSeq

		go func() {
			var entries []conversation.InboxEntry
			var totalItems int64
			var err error

			if conversationStatus != "" {
				searchInput := conversation.SearchInboxInput{
					UserID:               userID,
					WorkspaceID:          workspaceID,
					SelectedDepartmentID: departmentID,
					CampaignID:           campaignID,
					CampaignType:         campaignType,
					WhatsAppCampaignType: whatsAppCampaignType,
					ConversationStatus:   conversation.ConversationStatus(conversationStatus),
					AssignedUserID:       userID,
					IsAdmin:              isAdmin,
					Page:                 1,
					PageSize:             conversation.DefaultInboxPageSize,
				}
				if viewMode != "global" {
					searchInput.WorkspaceID = ""
				} else {
					searchInput.CampaignID = ""
				}
				entries, totalItems, err = h.inboxService.SearchInbox(userID, searchInput)
			} else if viewMode == "global" {
				entries, totalItems, err = h.inboxService.SearchInbox(
					userID, conversation.SearchInboxInput{
						CampaignType:         campaignType,
						WhatsAppCampaignType: whatsAppCampaignType,
						WorkspaceID:          workspaceID,
						SelectedDepartmentID: departmentID,
						AssignedUserID:       userID,
						IsAdmin:              isAdmin,
						Page:                 1,
						PageSize:             conversation.DefaultInboxPageSize,
					},
				)
			} else {
				entries, totalItems, err = h.inboxService.SearchInbox(
					userID, conversation.SearchInboxInput{
						CampaignID:           campaignID,
						CampaignType:         campaignType,
						WorkspaceID:          campaignWorkspaceID,
						SelectedDepartmentID: departmentID,
						AssignedUserID:       userID,
						IsAdmin:              isAdmin,
						Page:                 1,
						PageSize:             conversation.DefaultInboxPageSize,
					},
				)
			}
			if err != nil {
				log.Printf("[ConversationHub] Error getting inbox for user %s: %v", userID, err)
				return
			}

			totalPages := 0
			if totalItems > 0 {
				totalPages = int((totalItems + int64(conversation.DefaultInboxPageSize) - 1) / int64(conversation.DefaultInboxPageSize))
			}

			var StageCounts map[string]int64
			if viewMode == "global" {
				StageCounts, _ = h.inboxService.GetInboxStageCounts(workspaceID, "", campaignType)
			} else {
				StageCounts, _ = h.inboxService.GetInboxStageCounts(campaignWorkspaceID, campaignID, campaignType)
			}

			availableLabels, _ := h.inboxService.GetAvailableLabels(workspaceID)

			var convStatusCounts map[string]int64
			if viewMode == "global" {
				convStatusCounts, _ = h.inboxService.GetConversationStatusCounts(workspaceID, "", campaignType)
			} else {
				convStatusCounts, _ = h.inboxService.GetConversationStatusCounts("", campaignID, campaignType)
			}

			if conn.viewSeq != seq {
				log.Printf("[ConversationHub] Discarding stale initial inbox for connection %s (seq %d < %d)", conn.ID, seq, conn.viewSeq)
				return
			}

			h.connMu.RLock()
			_, stillExists := h.connections[conn.ID]
			h.connMu.RUnlock()
			if !stillExists {
				log.Printf("[ConversationHub] Skipping inbox send for connection %s (already closed)", conn.ID)
				return
			}

			h.sendToConnection(conn, &WSOutgoingMessage{
				Type: WSEventInbox,
				Payload: InboxPayload{
					Entries:                  entries,
					Page:                     1,
					PageSize:                 conversation.DefaultInboxPageSize,
					TotalItems:               totalItems,
					TotalPages:               totalPages,
					StageCounts:              StageCounts,
					ConversationStatusCounts: convStatusCounts,
					AvailableLabels:          availableLabels,
				},
			})
		}()
	}

	h.sendConnectedUsersUpdate(conn)
	h.scheduleBroadcastConnectedUsers(conn.WorkspaceID)

	log.Printf("[ConversationHub] User %s connected (connection %s, workspace %s)", conn.UserID, conn.ID, conn.WorkspaceID)
}

const broadcastDebounceDelay = 150 * time.Millisecond

func (h *ConversationHub) scheduleBroadcastConnectedUsers(workspaceID string) {
	h.connUserDebounceMu.Lock()
	if t, ok := h.connUserDebounce[workspaceID]; ok {
		t.Stop()
	}
	h.connUserDebounce[workspaceID] = time.AfterFunc(broadcastDebounceDelay, func() {
		h.broadcastConnectedUsersToWorkspace(workspaceID)
		h.connUserDebounceMu.Lock()
		delete(h.connUserDebounce, workspaceID)
		h.connUserDebounceMu.Unlock()
	})
	h.connUserDebounceMu.Unlock()
}

type connectedUserSnapshot struct {
	UserID       string
	WorkspaceID  string
	DepartmentID string
	CampaignID   string
	CampaignType string
	CampaignName string
	ViewMode     string
	Username     string
	Email        string
	ConnectedAt  string
	IsAdmin      bool
}

type connectedUsersScope struct {
	key           string
	allowed       bool
	fullWorkspace bool
	campaignID    string
	campaignType  string
	departmentIDs []string
}

func (h *ConversationHub) buildConnectedUsersScope(conn *WSConnection) connectedUsersScope {
	if conn == nil {
		return connectedUsersScope{key: "invalid", allowed: false}
	}

	isGlobal := conn.ViewMode == "global" || conn.CampaignID == ""
	if !isGlobal {
		return connectedUsersScope{
			key:          "campaign:" + conn.CampaignType + ":" + conn.CampaignID,
			allowed:      true,
			campaignID:   conn.CampaignID,
			campaignType: conn.CampaignType,
		}
	}

	if h.authorizer == nil {
		return connectedUsersScope{key: "global:workspace", allowed: true, fullWorkspace: true}
	}

	if conn.IsAdmin || h.authorizer.IsWorkspaceOwnerOrAdmin(conn.UserID, conn.WorkspaceID) {
		return connectedUsersScope{key: "global:workspace", allowed: true, fullWorkspace: true}
	}

	scope, allowed := h.authorizer.GetDepartmentScope(conn.UserID, conn.WorkspaceID, conn.IsAdmin)
	if !allowed {
		return connectedUsersScope{key: "global:denied", allowed: false}
	}
	if !scope.Restrict {
		return connectedUsersScope{key: "global:workspace", allowed: true, fullWorkspace: true}
	}

	departmentIDs := append([]string(nil), scope.DepartmentIDs...)
	sort.Strings(departmentIDs)

	return connectedUsersScope{
		key:           "global:departments:" + strings.Join(departmentIDs, ","),
		allowed:       true,
		departmentIDs: departmentIDs,
	}
}

func (h *ConversationHub) connectedUserCanAccessCampaign(candidate connectedUserSnapshot, workspaceID, campaignID, campaignType string) bool {
	if candidate.WorkspaceID != workspaceID || campaignID == "" || campaignType == "" {
		return false
	}
	if candidate.CampaignID == campaignID && candidate.CampaignType == campaignType {
		return true
	}
	if h.authorizer == nil {
		return false
	}
	return h.authorizer.CanAccessCampaign(candidate.UserID, workspaceID, campaignID, campaignType, candidate.IsAdmin)
}

func (h *ConversationHub) connectedUserMatchesDepartmentScope(candidate connectedUserSnapshot, workspaceID string, departmentIDs []string) bool {
	if candidate.WorkspaceID != workspaceID {
		return false
	}
	if len(departmentIDs) == 0 {
		return false
	}
	if h.authorizer == nil {
		return containsStringValue(departmentIDs, candidate.DepartmentID)
	}
	if candidate.IsAdmin || h.authorizer.IsWorkspaceOwnerOrAdmin(candidate.UserID, workspaceID) {
		return true
	}
	if candidate.DepartmentID != "" {
		return containsStringValue(departmentIDs, candidate.DepartmentID)
	}

	scope, allowed := h.authorizer.GetDepartmentScope(candidate.UserID, workspaceID, candidate.IsAdmin)
	if !allowed {
		return false
	}
	if !scope.Restrict {
		return true
	}

	return slicesOverlap(departmentIDs, scope.DepartmentIDs)
}

func (h *ConversationHub) canConnectionSeeConnectedUser(conn *WSConnection, scope connectedUsersScope, candidate connectedUserSnapshot) bool {
	if conn == nil || !scope.allowed {
		return false
	}
	if candidate.WorkspaceID != conn.WorkspaceID {
		return false
	}
	if scope.fullWorkspace {
		return true
	}
	if scope.campaignID != "" && scope.campaignType != "" {
		return h.connectedUserCanAccessCampaign(candidate, conn.WorkspaceID, scope.campaignID, scope.campaignType)
	}
	if candidate.CampaignID != "" && candidate.CampaignType != "" {
		if h.authorizer == nil {
			return false
		}
		return h.authorizer.CanAccessCampaign(conn.UserID, conn.WorkspaceID, candidate.CampaignID, candidate.CampaignType, conn.IsAdmin)
	}
	return h.connectedUserMatchesDepartmentScope(candidate, conn.WorkspaceID, scope.departmentIDs)
}

func (h *ConversationHub) filterConnectedUsersForConnection(conn *WSConnection, scope connectedUsersScope, snapshots []connectedUserSnapshot) []connectedUserSnapshot {
	if !scope.allowed {
		return []connectedUserSnapshot{}
	}

	filtered := make([]connectedUserSnapshot, 0, len(snapshots))
	seenUsers := make(map[string]bool)
	for _, snapshot := range snapshots {
		if seenUsers[snapshot.UserID] {
			continue
		}
		if !h.canConnectionSeeConnectedUser(conn, scope, snapshot) {
			continue
		}
		seenUsers[snapshot.UserID] = true
		filtered = append(filtered, snapshot)
	}

	return filtered
}

func marshalConnectedUsersPayload(users []connectedUserSnapshot) ([]byte, error) {
	type ConnectedUser struct {
		UserID       string `json:"user_id"`
		WorkspaceID  string `json:"workspace_id"`
		CampaignID   string `json:"campaign_id"`
		CampaignType string `json:"campaign_type"`
		CampaignName string `json:"campaign_name,omitempty"`
		ViewMode     string `json:"view_mode"`
		Username     string `json:"username,omitempty"`
		Email        string `json:"email,omitempty"`
		ConnectedAt  string `json:"connected_at"`
	}

	payloadUsers := make([]ConnectedUser, 0, len(users))
	for _, user := range users {
		payloadUsers = append(payloadUsers, ConnectedUser{
			UserID:       user.UserID,
			WorkspaceID:  user.WorkspaceID,
			CampaignID:   user.CampaignID,
			CampaignType: user.CampaignType,
			CampaignName: user.CampaignName,
			ViewMode:     user.ViewMode,
			Username:     user.Username,
			Email:        user.Email,
			ConnectedAt:  user.ConnectedAt,
		})
	}

	return json.Marshal(&WSOutgoingMessage{
		Type: WSEventConnectedUsers,
		Payload: struct {
			Users []ConnectedUser `json:"users"`
		}{Users: payloadUsers},
	})
}

func (h *ConversationHub) buildWorkspaceConnectedUserSnapshots(workspaceID string) []connectedUserSnapshot {
	type localPresence struct {
		UserID       string
		WorkspaceID  string
		DepartmentID string
		CampaignID   string
		CampaignType string
		ViewMode     string
		ConnectedAt  string
		IsAdmin      bool
	}

	h.connMu.RLock()
	presences := make([]localPresence, 0)
	allUserIDs := make(map[string]bool)
	for _, conn := range h.connections {
		if conn.WorkspaceID != workspaceID {
			continue
		}
		presences = append(presences, localPresence{
			UserID:       conn.UserID,
			WorkspaceID:  conn.WorkspaceID,
			DepartmentID: conn.DepartmentID,
			CampaignID:   conn.CampaignID,
			CampaignType: conn.CampaignType,
			ViewMode:     conn.ViewMode,
			ConnectedAt:  conn.connectedAt.UTC().Format(time.RFC3339),
			IsAdmin:      conn.IsAdmin,
		})
		allUserIDs[conn.UserID] = true
	}
	h.connMu.RUnlock()

	if h.sharedState != nil {
		if members, err := h.sharedState.SMembers("hub:connected_users:" + workspaceID); err == nil {
			for _, member := range members {
				parts := strings.SplitN(member, "|", 2)
				if len(parts) != 2 || parts[1] == h.replicaID {
					continue
				}
				remoteUserID := parts[0]
				presences = append(presences, localPresence{
					UserID:      remoteUserID,
					WorkspaceID: workspaceID,
					ViewMode:    "global",
					ConnectedAt: time.Now().UTC().Format(time.RFC3339),
				})
				allUserIDs[remoteUserID] = true
			}
		}
	}

	userMap := make(map[string]*user.User)
	if h.userRepo != nil && len(allUserIDs) > 0 {
		ids := make([]string, 0, len(allUserIDs))
		for id := range allUserIDs {
			ids = append(ids, id)
		}
		if users, err := h.userRepo.FindByIDs(ids); err == nil {
			for _, resolved := range users {
				userMap[resolved.ID] = resolved
			}
		}
	}

	campaignNameCache := make(map[string]string)
	resolveCampaignName := func(campaignID, campaignType string) string {
		if campaignID == "" {
			return ""
		}
		cacheKey := campaignType + ":" + campaignID
		if name, ok := campaignNameCache[cacheKey]; ok {
			return name
		}
		name := h.resolveCampaignName(campaignID, campaignType)
		campaignNameCache[cacheKey] = name
		return name
	}

	snapshots := make([]connectedUserSnapshot, 0, len(presences))
	for _, presence := range presences {
		snapshot := connectedUserSnapshot{
			UserID:       presence.UserID,
			WorkspaceID:  presence.WorkspaceID,
			DepartmentID: presence.DepartmentID,
			CampaignID:   presence.CampaignID,
			CampaignType: presence.CampaignType,
			CampaignName: resolveCampaignName(presence.CampaignID, presence.CampaignType),
			ViewMode:     presence.ViewMode,
			ConnectedAt:  presence.ConnectedAt,
			IsAdmin:      presence.IsAdmin,
		}
		if resolvedUser, ok := userMap[presence.UserID]; ok {
			snapshot.Username = resolvedUser.Username
			snapshot.Email = resolvedUser.Email
		}
		snapshots = append(snapshots, snapshot)
	}

	return snapshots
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func slicesOverlap(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}

func (h *ConversationHub) broadcastConnectedUsersToWorkspace(workspaceID string) {
	h.connMu.RLock()
	var wsConns []*WSConnection

	for _, c := range h.connections {
		if c.WorkspaceID != workspaceID {
			continue
		}
		wsConns = append(wsConns, c)
	}
	h.connMu.RUnlock()

	if len(wsConns) == 0 {
		return
	}

	snapshots := h.buildWorkspaceConnectedUserSnapshots(workspaceID)
	payloadCache := make(map[string][]byte)

	for _, conn := range wsConns {
		scope := h.buildConnectedUsersScope(conn)
		data, ok := payloadCache[scope.key]
		if !ok {
			filtered := h.filterConnectedUsersForConnection(conn, scope, snapshots)
			marshaled, err := marshalConnectedUsersPayload(filtered)
			if err != nil {
				log.Printf("[ConversationHub] Error marshaling connected users for scope %s: %v", scope.key, err)
				continue
			}
			data = marshaled
			payloadCache[scope.key] = data
		}
		h.sendRawToConnection(conn, data)
	}
}

func (h *ConversationHub) handleUnregister(conn *WSConnection) {
	select {
	case <-conn.Done:

	default:
		close(conn.Done)
	}

	h.connMu.Lock()
	if _, exists := h.connections[conn.ID]; exists {
		delete(h.connections, conn.ID)
		close(conn.Send)

		if userConns, ok := h.userConnections[conn.UserID]; ok {
			delete(userConns, conn.ID)
			if len(userConns) == 0 {
				delete(h.userConnections, conn.UserID)
			}
		}
	}
	hasOtherConnections := len(h.userConnections[conn.UserID]) > 0
	h.connMu.Unlock()

	h.wsMetrics.DecWSConnections(metrics.WSEndpointConversations)

	if !hasOtherConnections {
		member := conn.UserID + "|" + h.replicaID
		_ = h.sharedState.SRem("hub:connected_users:"+conn.WorkspaceID, member)
		if h.presence != nil && conn.WorkspaceID != "" {
			_ = h.presence.Transition(conn.WorkspaceID, conn.UserID, "offline", "ws_hub")
		}
	}

	if !hasOtherConnections {
		h.subMu.Lock()
		if subs, exists := h.userSubscriptions[conn.UserID]; exists {
			for sub := range subs {
				if subscribers, ok := h.entrySubscribers[sub]; ok {
					delete(subscribers, conn.UserID)
					if len(subscribers) == 0 {
						delete(h.entrySubscribers, sub)
					}
				}
			}
			delete(h.userSubscriptions, conn.UserID)
		}
		h.subMu.Unlock()

		delete(h.sentMessageIDs, conn.UserID)
	}

	delete(h.sentMessageIDs, conn.ID)

	log.Printf("[ConversationHub] User %s disconnected (connection %s, %d remaining)", conn.UserID, conn.ID, len(h.userConnections[conn.UserID]))

	h.scheduleBroadcastConnectedUsers(conn.WorkspaceID)
}

func (h *ConversationHub) checkPermission(conn *WSConnection, resource, action string) bool {
	if conn.IsAdmin {
		return true
	}
	if h.authorizer == nil {
		return true
	}
	if !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, resource, action, false) {
		h.sendError(conn, "forbidden", fmt.Sprintf("You don't have %s:%s permission in this workspace", resource, action))
		return false
	}
	return true
}

func (h *ConversationHub) handleIncoming(msg *incomingMessage) {
	switch msg.message.Type {
	case WSEventSubscribe:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleSubscribe(msg.conn, msg.message.Payload)
		}
	case WSEventUnsubscribe:
		h.handleUnsubscribe(msg.conn, msg.message.Payload)
	case WSEventSend:
		if h.checkPermission(msg.conn, "conversations", "send") {
			h.handleSend(msg.conn, msg.message.Payload)
		}
	case WSEventSendButton:
		if h.checkPermission(msg.conn, "conversations", "send") {
			h.handleSendButton(msg.conn, msg.message.Payload)
		}
	case WSEventMarkRead:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleMarkRead(msg.conn, msg.message.Payload)
		}
	case WSEventRequestConnectedUsers:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleRequestConnectedUsers(msg.conn, msg.message.Payload)
		}
	case WSEventTyping:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleTyping(msg.conn, msg.message.Payload)
		}
	case WSEventLoadHistory:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleLoadHistory(msg.conn, msg.message.Payload)
		}
	case WSEventLoadAround:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleLoadAround(msg.conn, msg.message.Payload)
		}
	case WSEventRequestInbox:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleRequestInboxPage(msg.conn, msg.message.Payload)
		}
	case WSEventSearchInbox:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleSearchInbox(msg.conn, msg.message.Payload)
		}
	case WSEventSearchMessages:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleSearchMessages(msg.conn, msg.message.Payload)
		}
	case WSEventLoadEntryMatches:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleLoadEntryMatches(msg.conn, msg.message.Payload)
		}
	case WSEventReopenWindow:
		if h.checkPermission(msg.conn, "conversations", "reopen") {
			h.handleReopenWindow(msg.conn, msg.message.Payload)
		}
	case WSEventAssignTo:
		if h.checkPermission(msg.conn, "conversations", "assign") {
			h.handleAssignTo(msg.conn, msg.message.Payload)
		}
	case WSEventRequestFunnelColumn:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleRequestFunnelColumn(msg.conn, msg.message.Payload)
		}
	case WSEventRequestFunnelSummary:
		if h.checkPermission(msg.conn, "conversations", "read") {
			h.handleRequestFunnelSummary(msg.conn, msg.message.Payload)
		}
	case WSEventSwitchView:
		h.handleSwitchView(msg.conn, msg.message.Payload)
	case WSEventSetConversationStatus:
		if h.checkPermission(msg.conn, "conversations", "send") {
			h.handleSetConversationStatus(msg.conn, msg.message.Payload)
		}
	default:
		h.sendError(msg.conn, "unknown_event", "Unknown event type: "+string(msg.message.Type))
	}
}

func (h *ConversationHub) handleSubscribe(conn *WSConnection, payload json.RawMessage) {
	var p SubscribePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid subscribe payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" {
		log.Printf("[ConversationHub] Subscribe missing fields - EntryID=%q EntryType=%q payload=%s", p.EntryID, p.EntryType, string(payload))
		h.sendError(conn, "missing_fields", "entry_id and entry_type are required")
		return
	}

	p.EntryType = strings.ToLower(strings.TrimSpace(p.EntryType))

	if p.EntryType != "voice" && p.EntryType != "whatsapp" {
		h.sendError(conn, "invalid_entry_type", "entry_type must be 'voice' or 'whatsapp'")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	sub := entrySubscription{entryID: p.EntryID, entryType: p.EntryType}

	h.subMu.Lock()
	if h.userSubscriptions[conn.UserID] == nil {
		h.userSubscriptions[conn.UserID] = make(map[entrySubscription]bool)
	}
	h.userSubscriptions[conn.UserID][sub] = true

	if h.entrySubscribers[sub] == nil {
		h.entrySubscribers[sub] = make(map[string]bool)
	}
	h.entrySubscribers[sub][conn.UserID] = true
	h.subMu.Unlock()

	var leadName, leadNumber, leadPicture string
	var leadMetadata map[string]interface{}
	var entryVariables []string
	var automationEnabled = true
	var unreadCount int64
	var windowOpen bool
	var windowExpiresAt *time.Time
	var messages []*conversation.Message
	var hasMore bool
	var total int64

	if h.historyProvider != nil {
		leadName, leadNumber, leadPicture, leadMetadata, entryVariables, automationEnabled, _ = h.historyProvider.GetEntryInfo(p.EntryID, p.EntryType)
		unreadCount, _ = h.historyProvider.GetUnreadCount(p.EntryID, shared.EntryType(p.EntryType))
		windowOpen, windowExpiresAt = h.historyProvider.GetWindowStatusForEntry(p.EntryID, p.EntryType)

		pageSize := p.PageSize
		if pageSize <= 0 {
			pageSize = conversation.DefaultHistoryPageSize
		}
		if pageSize > conversation.MaxHistoryPageSize {
			pageSize = conversation.MaxHistoryPageSize
		}
		var histErr error
		messages, hasMore, total, histErr = h.historyProvider.GetHistory(p.EntryID, shared.EntryType(p.EntryType), pageSize)
		if histErr != nil {
			log.Printf("[ConversationHub] Error loading history for %s:%s user %s: %v", p.EntryType, p.EntryID, conn.UserID, histErr)
		}
	}

	alreadySent := h.sentMessageIDs[conn.ID][sub]
	var filteredMessages []*conversation.Message
	if len(alreadySent) > 0 && len(messages) > 0 {
		filteredMessages = make([]*conversation.Message, 0, len(messages))
		for _, m := range messages {
			if !alreadySent[m.ID] {
				filteredMessages = append(filteredMessages, m)
			}
		}
	} else {
		filteredMessages = messages
	}

	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventSubscribed,
		Payload: SubscribedPayload{
			EntryID:           p.EntryID,
			EntryType:         p.EntryType,
			LeadName:          leadName,
			LeadNumber:        leadNumber,
			LeadPicture:       leadPicture,
			LeadMetadata:      leadMetadata,
			EntryVariables:    entryVariables,
			UnreadCount:       unreadCount,
			AutomationEnabled: automationEnabled,
			WindowOpen:        windowOpen,
			WindowExpiresAt:   windowExpiresAt,
		},
	})

	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventHistory,
		Payload: HistoryPayload{
			EntryID:   p.EntryID,
			EntryType: p.EntryType,
			Messages:  filteredMessages,
			HasMore:   hasMore,
			Total:     total,
			PageSize:  len(filteredMessages),
		},
	})

	newSentIDs := make(map[string]bool, len(messages))
	for _, m := range messages {
		newSentIDs[m.ID] = true
	}
	if h.sentMessageIDs[conn.ID] == nil {
		h.sentMessageIDs[conn.ID] = make(map[entrySubscription]map[string]bool)
	}
	h.sentMessageIDs[conn.ID][sub] = newSentIDs

	log.Printf("[ConversationHub] User %s subscribed to %s:%s", conn.UserID, p.EntryType, p.EntryID)

	go h.tryAssignOnOpen(conn, p.EntryID, p.EntryType)
}

func (h *ConversationHub) handleLoadHistory(conn *WSConnection, payload json.RawMessage) {
	var p LoadHistoryPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid load_history payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.Before == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and before are required")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	before, err := time.Parse(time.RFC3339Nano, p.Before)
	if err != nil {
		before, err = time.Parse(time.RFC3339, p.Before)
		if err != nil {
			h.sendError(conn, "invalid_timestamp", "before must be a valid ISO 8601 timestamp")
			return
		}
	}

	if h.historyProvider == nil {
		h.sendError(conn, "not_configured", "History provider not configured")
		return
	}

	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = conversation.DefaultHistoryPageSize
	}
	if pageSize > conversation.MaxHistoryPageSize {
		pageSize = conversation.MaxHistoryPageSize
	}

	go func() {
		messages, hasMore, err := h.historyProvider.GetHistoryBefore(p.EntryID, shared.EntryType(p.EntryType), before, pageSize)
		if err != nil {
			log.Printf("[ConversationHub] Error loading history for %s:%s: %v", p.EntryType, p.EntryID, err)
			h.sendError(conn, "history_error", "Failed to load message history")
			return
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventHistory,
			Payload: HistoryPayload{
				EntryID:   p.EntryID,
				EntryType: p.EntryType,
				Messages:  messages,
				HasMore:   hasMore,
				PageSize:  len(messages),
			},
		})
	}()
}

func (h *ConversationHub) handleLoadAround(conn *WSConnection, payload json.RawMessage) {
	var p LoadAroundPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid load_around payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.Timestamp == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and timestamp are required")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	around, err := time.Parse(time.RFC3339Nano, p.Timestamp)
	if err != nil {
		around, err = time.Parse(time.RFC3339, p.Timestamp)
		if err != nil {
			h.sendError(conn, "invalid_timestamp", "timestamp must be a valid ISO 8601 timestamp")
			return
		}
	}

	if h.historyProvider == nil {
		h.sendError(conn, "not_configured", "History provider not configured")
		return
	}

	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = conversation.DefaultHistoryPageSize
	}
	if pageSize > conversation.MaxHistoryPageSize {
		pageSize = conversation.MaxHistoryPageSize
	}

	go func() {
		messages, hasBefore, hasAfter, total, err := h.historyProvider.GetHistoryAround(p.EntryID, shared.EntryType(p.EntryType), around, pageSize)
		if err != nil {
			log.Printf("[ConversationHub] Error loading history around for %s:%s: %v", p.EntryType, p.EntryID, err)
			h.sendError(conn, "history_error", "Failed to load message history")
			return
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventHistory,
			Payload: HistoryPayload{
				EntryID:   p.EntryID,
				EntryType: p.EntryType,
				Messages:  messages,
				HasMore:   hasBefore,
				Total:     total,
				PageSize:  len(messages),
			},
		})
		_ = hasAfter
	}()
}

func (h *ConversationHub) handleRequestInboxPage(conn *WSConnection, payload json.RawMessage) {
	var p RequestInboxPagePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid request_inbox_page payload")
		return
	}

	if p.Page <= 0 {
		p.Page = 1
	}

	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = conversation.DefaultInboxPageSize
	}
	if pageSize > conversation.MaxInboxPageSize {
		pageSize = conversation.MaxInboxPageSize
	}

	if h.inboxService == nil {
		h.sendError(conn, "not_configured", "Inbox service not configured")
		return
	}

	viewMode := conn.ViewMode
	userID := conn.UserID
	workspaceID := conn.WorkspaceID
	departmentID := conn.DepartmentID
	campaignType := conn.CampaignType
	whatsAppCampaignType := conn.WhatsAppCampaignType
	campaignID := conn.CampaignID
	campaignWorkspaceID := conn.CampaignWorkspaceID
	conversationStatus := conn.ConversationStatus
	isAdmin := conn.IsAdmin
	seq := conn.viewSeq

	go func() {
		var entries []conversation.InboxEntry
		var totalItems int64
		var err error

		if conversationStatus != "" {
			searchInput := conversation.SearchInboxInput{
				UserID:               userID,
				WorkspaceID:          workspaceID,
				SelectedDepartmentID: departmentID,
				CampaignID:           campaignID,
				CampaignType:         campaignType,
				WhatsAppCampaignType: whatsAppCampaignType,
				ConversationStatus:   conversation.ConversationStatus(conversationStatus),
				AssignedUserID:       userID,
				IsAdmin:              isAdmin,
				Page:                 p.Page,
				PageSize:             pageSize,
			}
			if viewMode != "global" {
				searchInput.WorkspaceID = ""
			} else {
				searchInput.CampaignID = ""
			}
			entries, totalItems, err = h.inboxService.SearchInbox(userID, searchInput)
		} else if viewMode == "global" {
			entries, totalItems, err = h.inboxService.SearchInbox(
				userID, conversation.SearchInboxInput{
					CampaignType:         campaignType,
					WhatsAppCampaignType: whatsAppCampaignType,
					WorkspaceID:          workspaceID,
					SelectedDepartmentID: departmentID,
					AssignedUserID:       userID,
					IsAdmin:              isAdmin,
					Page:                 p.Page,
					PageSize:             pageSize,
				},
			)
		} else {
			entries, totalItems, err = h.inboxService.SearchInbox(
				userID, conversation.SearchInboxInput{
					CampaignID:           campaignID,
					CampaignType:         campaignType,
					WorkspaceID:          campaignWorkspaceID,
					SelectedDepartmentID: departmentID,
					AssignedUserID:       userID,
					IsAdmin:              isAdmin,
					Page:                 p.Page,
					PageSize:             pageSize,
				},
			)
		}
		if err != nil {
			log.Printf("[ConversationHub] Error getting inbox page %d for user %s: %v", p.Page, userID, err)
			h.sendError(conn, "inbox_error", "Failed to load inbox page")
			return
		}

		if conn.viewSeq != seq {
			return
		}

		totalPages := 0
		if totalItems > 0 {
			totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
		}

		var StageCounts map[string]int64
		if viewMode == "global" {
			StageCounts, _ = h.inboxService.GetInboxStageCounts(workspaceID, "", campaignType)
		} else {
			StageCounts, _ = h.inboxService.GetInboxStageCounts(campaignWorkspaceID, campaignID, campaignType)
		}

		availableLabels, _ := h.inboxService.GetAvailableLabels(workspaceID)

		var convStatusCounts map[string]int64
		if viewMode == "global" {
			convStatusCounts, _ = h.inboxService.GetConversationStatusCounts(workspaceID, "", campaignType)
		} else {
			convStatusCounts, _ = h.inboxService.GetConversationStatusCounts("", campaignID, campaignType)
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventInbox,
			Payload: InboxPayload{
				Entries:                  entries,
				Page:                     p.Page,
				PageSize:                 pageSize,
				TotalItems:               totalItems,
				TotalPages:               totalPages,
				StageCounts:              StageCounts,
				ConversationStatusCounts: convStatusCounts,
				AvailableLabels:          availableLabels,
			},
		})
	}()
}

func (h *ConversationHub) handleUnsubscribe(conn *WSConnection, payload json.RawMessage) {
	var p SubscribePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid unsubscribe payload")
		return
	}

	sub := entrySubscription{entryID: p.EntryID, entryType: p.EntryType}

	h.subMu.Lock()
	if subs, exists := h.userSubscriptions[conn.UserID]; exists {
		delete(subs, sub)
	}
	if subscribers, exists := h.entrySubscribers[sub]; exists {
		delete(subscribers, conn.UserID)
		if len(subscribers) == 0 {
			delete(h.entrySubscribers, sub)
		}
	}
	h.subMu.Unlock()

	if connSubs, ok := h.sentMessageIDs[conn.ID]; ok {
		delete(connSubs, sub)
		if len(connSubs) == 0 {
			delete(h.sentMessageIDs, conn.ID)
		}
	}

	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventUnsubscribed,
		Payload: SubscribePayload{
			EntryID:   p.EntryID,
			EntryType: p.EntryType,
		},
	})

	log.Printf("[ConversationHub] User %s unsubscribed from %s:%s", conn.UserID, p.EntryType, p.EntryID)
}

func (h *ConversationHub) handleSend(conn *WSConnection, payload json.RawMessage) {
	var p SendMessagePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid send payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" {
		h.sendError(conn, "missing_fields", "entry_id and entry_type are required")
		return
	}

	if p.Text == "" && p.MediaID == nil {
		h.sendError(conn, "missing_content", "text or media_id is required")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.BroadcastMessageError(conn.UserID, p.RequestID, p.EntryID, p.EntryType, "You don't have access to this conversation", "unauthorized")
		return
	}

	if h.messageSender == nil {
		h.BroadcastMessageError(conn.UserID, p.RequestID, p.EntryID, p.EntryType, "Message sending not configured", "not_configured")
		return
	}

	go func() {
		var message *conversation.Message
		var err error

		replyTo := ""
		if p.ReplyToMessageID != nil {
			replyTo = *p.ReplyToMessageID
		}

		user, err := h.userRepo.FindByID(conn.UserID)
		if err != nil {
			log.Printf("[ConversationHub] Error resolving user for sending message: %v", err)
		}
		userUsername := user.Username
		text := strings.TrimSpace(p.Text)
		if p.Signed {
			text = fmt.Sprintf("*%s*:\n%s", userUsername, text)
		}

		if p.MediaID != nil && p.MediaType != nil {
			message, err = h.messageSender.SendMediaMessage(p.EntryID, p.EntryType, *p.MediaID, *p.MediaType, conn.UserID, replyTo, text)
		} else {
			message, err = h.messageSender.SendTextMessage(p.EntryID, p.EntryType, text, conn.UserID, replyTo)
		}

		if err != nil {
			h.BroadcastMessageError(conn.UserID, p.RequestID, p.EntryID, p.EntryType, err.Error(), "send_failed")
			return
		}

		h.BroadcastMessageSent(conn.UserID, p.RequestID, p.EntryID, p.EntryType, message)
		h.afterOperatorSend(conn, p.EntryID, p.EntryType, message)
	}()
}

func (h *ConversationHub) afterOperatorSend(conn *WSConnection, entryID, entryType string, message *conversation.Message) {
	if h.statusUpdater != nil && message != nil {
		if err := h.statusUpdater.TransitionOnMessage(entryID, entryType, message.MessageType); err != nil {
			log.Printf("[ConversationHub] Error transitioning conversation status for %s (%s): %v", entryID, entryType, err)
		}
	}

	wsID := conn.WorkspaceID
	if h.workspaceResolver != nil {
		if resolved, err := h.workspaceResolver.GetEntryWorkspaceID(entryID, entryType); err == nil && resolved != "" {
			wsID = resolved
		}
	}

	if h.eventLogger != nil {
		details := map[string]string{}
		if message != nil && message.ID != "" {
			details["message_id"] = message.ID
		}
		channel := "whatsapp"
		switch entryType {
		case "voice":
			channel = "voice"
		case "support":
			channel = "support"
		}
		h.eventLogger.Log(ce.New(wsID, entryID, entryType, ce.EventReplied).
			WithActorHuman(conn.UserID).
			WithChannel(channel).
			WithDetails(details).
			Build())
	}

	if h.aiSessions != nil && wsID != "" {
		h.aiSessions.EndOpenRaw(wsID, entryID, entryType, "handed_off", "human_reply", conn.UserID)
	}

	h.ensureInitialTag(entryID, entryType)
	go h.BroadcastEntryUpdate(entryID, entryType, message)
}

func (h *ConversationHub) handleSendButton(conn *WSConnection, payload json.RawMessage) {
	var p SendButtonPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid send_button payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" {
		h.sendError(conn, "missing_fields", "entry_id and entry_type are required")
		return
	}

	if p.BodyText == "" {
		h.sendError(conn, "missing_content", "body_text is required")
		return
	}

	if len(p.Buttons) == 0 || len(p.Buttons) > 3 {
		h.sendError(conn, "invalid_buttons", "1-3 buttons are required")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.BroadcastMessageError(conn.UserID, p.RequestID, p.EntryID, p.EntryType, "You don't have access to this conversation", "unauthorized")
		return
	}

	if h.messageSender == nil {
		h.BroadcastMessageError(conn.UserID, p.RequestID, p.EntryID, p.EntryType, "Message sending not configured", "not_configured")
		return
	}

	go func() {
		replyTo := ""
		if p.ReplyToMessageID != nil {
			replyTo = *p.ReplyToMessageID
		}

		message, err := h.messageSender.SendButtonMessage(p.EntryID, p.EntryType, conn.UserID, replyTo, conversation.SendButtonInput{
			HeaderType: p.HeaderType,
			HeaderText: p.HeaderText,
			BodyText:   p.BodyText,
			FooterText: p.FooterText,
			Buttons:    p.Buttons,
		})
		if err != nil {
			h.BroadcastMessageError(conn.UserID, p.RequestID, p.EntryID, p.EntryType, err.Error(), "send_failed")
			return
		}

		h.BroadcastMessageSent(conn.UserID, p.RequestID, p.EntryID, p.EntryType, message)
		h.afterOperatorSend(conn, p.EntryID, p.EntryType, message)
	}()
}

func (h *ConversationHub) handleRequestConnectedUsers(conn *WSConnection, payload json.RawMessage) {
	h.sendConnectedUsersUpdate(conn)
}

func (h *ConversationHub) sendConnectedUsersUpdate(conn *WSConnection) {
	snapshots := h.buildWorkspaceConnectedUserSnapshots(conn.WorkspaceID)
	scope := h.buildConnectedUsersScope(conn)
	filtered := h.filterConnectedUsersForConnection(conn, scope, snapshots)

	fmt.Printf("[ConversationHub] Connected users for workspace %s campaign %s:%s scope=%s count=%d\n", conn.WorkspaceID, conn.CampaignType, conn.CampaignID, scope.key, len(filtered))

	data, err := marshalConnectedUsersPayload(filtered)
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling connected users for %s: %v", conn.UserID, err)
		return
	}

	h.sendRawToConnection(conn, data)
}

func (h *ConversationHub) resolveCampaignName(campaignID, campaignType string) string {
	switch campaignType {
	case "whatsapp":
		if h.waCampaignRepo != nil {
			if c, err := h.waCampaignRepo.FindByID(campaignID); err == nil {
				return c.Name
			}
		}
	}
	return ""
}

func (h *ConversationHub) handleMarkRead(conn *WSConnection, payload json.RawMessage) {
	var p MarkReadPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid mark_read payload")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	if h.messageMarker == nil {
		return
	}

	go func() {
		if err := h.messageMarker.MarkAsRead(p.EntryID, shared.EntryType(p.EntryType), p.MessageIDs, conn.UserID); err != nil {
			log.Printf("[ConversationHub] Error marking messages as read: %v", err)
			return
		}

		h.BroadcastRead(p.EntryID, p.EntryType, p.MessageIDs, conn.UserID, time.Now().UTC())
	}()
}

func (h *ConversationHub) handleTyping(conn *WSConnection, payload json.RawMessage) {
	var p struct {
		EntryID   string `json:"entry_id"`
		EntryType string `json:"entry_type"`
		IsTyping  *bool  `json:"is_typing"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	p.EntryType = strings.ToLower(strings.TrimSpace(p.EntryType))

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		return
	}

	isTyping := true
	if p.IsTyping != nil {
		isTyping = *p.IsTyping
	}

	h.BroadcastTyping(p.EntryID, p.EntryType, conn.UserID, isTyping)

	if !isTyping || h.messageMarker == nil || p.EntryType != string(shared.EntryTypeWhatsApp) {
		return
	}

	go func(entryID string) {
		if err := h.messageMarker.SendTypingIndicator(entryID, shared.EntryTypeWhatsApp); err != nil {
			log.Printf("[ConversationHub] Error sending WhatsApp typing indicator for %s:%s: %v", p.EntryType, entryID, err)
		}
	}(p.EntryID)
}

func (h *ConversationHub) handleSearchInbox(conn *WSConnection, payload json.RawMessage) {
	var p SearchInboxPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid search_inbox payload")
		return
	}

	if h.inboxService == nil {
		h.sendError(conn, "not_configured", "Inbox service not configured")
		return
	}

	if p.Page <= 0 {
		p.Page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = conversation.DefaultInboxPageSize
	}
	if pageSize > conversation.MaxInboxPageSize {
		pageSize = conversation.MaxInboxPageSize
	}

	if p.ConversationStatus != "" {
		status := conversation.ConversationStatus(p.ConversationStatus)
		if !status.Valid() {
			h.sendError(conn, "invalid_status", "conversation_status must be 'new', 'ongoing', or 'finished'")
			return
		}
	}

	go func() {
		var dateFrom, dateTo *time.Time
		if p.DateFrom != "" {
			if t, err := time.Parse(time.RFC3339, p.DateFrom); err == nil {
				dateFrom = &t
			}
		}
		if p.DateTo != "" {
			if t, err := time.Parse(time.RFC3339, p.DateTo); err == nil {
				dateTo = &t
			}
		}

		input := conversation.SearchInboxInput{
			UserID:                conn.UserID,
			CampaignID:            conn.CampaignID,
			CampaignType:          conn.CampaignType,
			SelectedDepartmentID:  conn.DepartmentID,
			Query:                 p.Query,
			StageID:               p.StageID,
			StageName:             p.StageName,
			MinMessageCount:       p.MinMessageCount,
			MaxMessageCount:       p.MaxMessageCount,
			MessageSearch:         p.MessageSearch,
			WindowOpen:            p.WindowOpen,
			HasUnread:             p.HasUnread,
			Channel:               p.Channel,
			DateFrom:              dateFrom,
			DateTo:                dateTo,
			ConversationStatus:    conversation.ConversationStatus(p.ConversationStatus),
			AssignedUserID:        conn.UserID,
			ResponsibleUserID:     p.ResponsibleUserID,
			ResponsibleUnassigned: p.ResponsibleUnassigned,
			IsAdmin:               conn.IsAdmin,
			Page:                  p.Page,
			PageSize:              pageSize,
		}

		if conn.ViewMode == "global" {
			input.WorkspaceID = conn.WorkspaceID
		}

		entries, totalItems, err := h.inboxService.SearchInbox(conn.UserID, input)
		if err != nil {
			log.Printf("[ConversationHub] Error searching inbox for user %s: %v", conn.UserID, err)
			h.sendError(conn, "search_error", "Failed to search inbox: "+err.Error())
			return
		}

		totalPages := 0
		if totalItems > 0 {
			totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventSearchResults,
			Payload: SearchResultsPayload{
				Query: p.Query,
				Filters: conversation.SearchFilters{
					StageID:            p.StageID,
					StageName:          p.StageName,
					MinMessageCount:    p.MinMessageCount,
					MaxMessageCount:    p.MaxMessageCount,
					MessageSearch:      p.MessageSearch,
					WindowOpen:         p.WindowOpen,
					HasUnread:          p.HasUnread,
					Channel:            p.Channel,
					DateFrom:           p.DateFrom,
					DateTo:             p.DateTo,
					ConversationStatus: p.ConversationStatus,
				},
				Entries:    entries,
				Page:       p.Page,
				PageSize:   pageSize,
				TotalItems: totalItems,
				TotalPages: totalPages,
			},
		})
	}()
}

func (h *ConversationHub) handleSwitchView(conn *WSConnection, payload json.RawMessage) {
	var p SwitchViewPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid switch_view payload")
		return
	}

	if p.CampaignType != "" && p.CampaignType != "voice" && p.CampaignType != "whatsapp" {
		h.sendError(conn, "invalid_campaign_type", "campaign_type must be 'voice' or 'whatsapp'")
		return
	}

	if p.WhatsAppCampaignType != "" && p.WhatsAppCampaignType != "standard" && p.WhatsAppCampaignType != "organic" {
		h.sendError(conn, "invalid_whatsapp_campaign_type", "whatsapp_campaign_type must be 'standard' or 'organic'")
		return
	}

	if p.ConversationStatus != "" {
		status := conversation.ConversationStatus(p.ConversationStatus)
		if !status.Valid() {
			h.sendError(conn, "invalid_status", "conversation_status must be 'new', 'ongoing', or 'finished'")
			return
		}
	}

	if !conn.IsAdmin {
		if p.CampaignType == "voice" && !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "campaigns", "read", false) {
			h.sendError(conn, "forbidden", "You don't have permission to view voice campaigns")
			return
		}
		if p.CampaignType == "whatsapp" && !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "whatsapp_campaigns", "read", false) {
			h.sendError(conn, "forbidden", "You don't have permission to view WhatsApp campaigns")
			return
		}
	}

	h.subMu.Lock()
	if subs, exists := h.userSubscriptions[conn.UserID]; exists {
		for sub := range subs {
			if subscribers, ok := h.entrySubscribers[sub]; ok {
				delete(subscribers, conn.UserID)
				if len(subscribers) == 0 {
					delete(h.entrySubscribers, sub)
				}
			}
		}
		delete(h.userSubscriptions, conn.UserID)
	}
	h.subMu.Unlock()

	delete(h.sentMessageIDs, conn.ID)

	conn.viewSeq++

	if p.CampaignID != "" && p.CampaignType != "" {
		resolvedWorkspaceID := conn.WorkspaceID
		if h.workspaceResolver != nil {
			wsID, err := h.workspaceResolver.GetCampaignWorkspaceID(p.CampaignID, p.CampaignType)
			if err != nil || wsID == "" {
				h.sendError(conn, "not_found", "Campaign not found")
				return
			}
			if wsID != conn.WorkspaceID {
				h.sendError(conn, "forbidden", "Campaign does not belong to this workspace")
				return
			}
			resolvedWorkspaceID = wsID
		}
		if h.authorizer != nil && !h.authorizer.CanAccessCampaign(conn.UserID, resolvedWorkspaceID, p.CampaignID, p.CampaignType, conn.IsAdmin) {
			h.sendError(conn, "forbidden", "You don't have access to this inbox")
			return
		}

		conn.CampaignID = p.CampaignID
		conn.CampaignType = p.CampaignType
		conn.WhatsAppCampaignType = p.WhatsAppCampaignType
		conn.ViewMode = "campaign"

		if h.workspaceResolver != nil {
			if wsID, err := h.workspaceResolver.GetCampaignWorkspaceID(p.CampaignID, p.CampaignType); err == nil && wsID != "" {
				conn.CampaignWorkspaceID = wsID
				conn.WorkspaceID = wsID
			}
		}
	} else {
		conn.CampaignID = ""
		conn.CampaignType = p.CampaignType
		conn.WhatsAppCampaignType = p.WhatsAppCampaignType
		conn.ViewMode = "global"
	}

	conn.ConversationStatus = p.ConversationStatus

	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventViewSwitched,
		Payload: ViewSwitchedPayload{
			CampaignID:           conn.CampaignID,
			CampaignType:         conn.CampaignType,
			WhatsAppCampaignType: conn.WhatsAppCampaignType,
			ViewMode:             conn.ViewMode,
			ConversationStatus:   conn.ConversationStatus,
		},
	})

	h.scheduleBroadcastConnectedUsers(conn.WorkspaceID)

	if h.inboxService != nil {

		viewMode := conn.ViewMode
		userID := conn.UserID
		workspaceID := conn.WorkspaceID
		departmentID := conn.DepartmentID
		campaignType := conn.CampaignType
		whatsAppCampaignType := conn.WhatsAppCampaignType
		campaignID := conn.CampaignID
		campaignWorkspaceID := conn.CampaignWorkspaceID
		conversationStatus := conn.ConversationStatus
		isAdmin := conn.IsAdmin
		seq := conn.viewSeq

		go func() {
			var entries []conversation.InboxEntry
			var totalItems int64
			var err error

			if conversationStatus != "" {
				searchInput := conversation.SearchInboxInput{
					UserID:               userID,
					WorkspaceID:          workspaceID,
					SelectedDepartmentID: departmentID,
					CampaignID:           campaignID,
					CampaignType:         campaignType,
					WhatsAppCampaignType: whatsAppCampaignType,
					ConversationStatus:   conversation.ConversationStatus(conversationStatus),
					AssignedUserID:       userID,
					IsAdmin:              isAdmin,
					Page:                 1,
					PageSize:             conversation.DefaultInboxPageSize,
				}
				if viewMode != "global" {
					searchInput.WorkspaceID = ""
				} else {
					searchInput.CampaignID = ""
				}
				entries, totalItems, err = h.inboxService.SearchInbox(userID, searchInput)
			} else if viewMode == "global" {
				entries, totalItems, err = h.inboxService.SearchInbox(
					userID, conversation.SearchInboxInput{
						CampaignType:         campaignType,
						WhatsAppCampaignType: whatsAppCampaignType,
						WorkspaceID:          workspaceID,
						SelectedDepartmentID: departmentID,
						AssignedUserID:       userID,
						IsAdmin:              isAdmin,
						Page:                 1,
						PageSize:             conversation.DefaultInboxPageSize,
					},
				)
			} else {
				entries, totalItems, err = h.inboxService.SearchInbox(
					userID, conversation.SearchInboxInput{
						CampaignID:           campaignID,
						CampaignType:         campaignType,
						WorkspaceID:          campaignWorkspaceID,
						SelectedDepartmentID: departmentID,
						AssignedUserID:       userID,
						IsAdmin:              isAdmin,
						Page:                 1,
						PageSize:             conversation.DefaultInboxPageSize,
					},
				)
			}
			if err != nil {
				log.Printf("[ConversationHub] Error loading inbox after view switch for user %s: %v", userID, err)
				return
			}

			if conn.viewSeq != seq {
				log.Printf("[ConversationHub] Discarding stale view-switch inbox for connection %s (seq %d < %d)", conn.ID, seq, conn.viewSeq)
				return
			}

			totalPages := 0
			if totalItems > 0 {
				totalPages = int((totalItems + int64(conversation.DefaultInboxPageSize) - 1) / int64(conversation.DefaultInboxPageSize))
			}

			var StageCounts map[string]int64
			if viewMode == "global" {
				StageCounts, _ = h.inboxService.GetInboxStageCounts(workspaceID, "", campaignType)
			} else {
				StageCounts, _ = h.inboxService.GetInboxStageCounts(campaignWorkspaceID, campaignID, campaignType)
			}

			availableLabels, _ := h.inboxService.GetAvailableLabels(workspaceID)

			var convStatusCounts map[string]int64
			if viewMode == "global" {
				convStatusCounts, _ = h.inboxService.GetConversationStatusCounts(workspaceID, "", campaignType)
			} else {
				convStatusCounts, _ = h.inboxService.GetConversationStatusCounts("", campaignID, campaignType)
			}

			h.sendToConnection(conn, &WSOutgoingMessage{
				Type: WSEventInbox,
				Payload: InboxPayload{
					Entries:                  entries,
					Page:                     1,
					PageSize:                 conversation.DefaultInboxPageSize,
					TotalItems:               totalItems,
					TotalPages:               totalPages,
					StageCounts:              StageCounts,
					ConversationStatusCounts: convStatusCounts,
					AvailableLabels:          availableLabels,
				},
			})
		}()
	}
}

func (h *ConversationHub) handleSetConversationStatus(conn *WSConnection, payload json.RawMessage) {
	var p SetConversationStatusPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid set_conversation_status payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.Status == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and status are required")
		return
	}

	status := conversation.ConversationStatus(p.Status)
	if !status.Valid() {
		h.sendError(conn, "invalid_status", "status must be 'new', 'ongoing', or 'finished'")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "conversations", "send", false) {
		h.sendError(conn, "forbidden", "You don't have permission to change the conversation status")
		return
	}

	if h.statusUpdater == nil {
		h.sendError(conn, "not_configured", "Conversation status not configured")
		return
	}

	currentStatus := h.statusUpdater.GetConversationStatus(p.EntryID, p.EntryType)

	if status == conversation.ConversationStatusNew {
		h.sendConversationStatusError(
			conn,
			"forbidden",
			"Conversations return to 'new' only when the customer sends a new message",
			p,
			currentStatus,
		)
		return
	}

	if currentStatus == conversation.ConversationStatusFinished && status != conversation.ConversationStatusFinished {
		h.sendConversationStatusError(
			conn,
			"forbidden",
			"Finished conversations can only reopen when the customer sends a new message",
			p,
			currentStatus,
		)
		return
	}

	if status == conversation.ConversationStatusFinished {
		if err := h.statusUpdater.Finish(p.EntryID, p.EntryType, conversation.FinishOptions{
			Source: conversation.CloseSourceHuman,
			Reason: conversation.CloseReasonManual,
		}); err != nil {
			h.sendConversationStatusError(
				conn,
				"internal_error",
				"Failed to update conversation status",
				p,
				currentStatus,
			)
			log.Printf("[ConversationHub] Error finishing conversation status for %s (%s): %v", p.EntryID, p.EntryType, err)
			return
		}
	} else if err := h.statusUpdater.SetConversationStatus(p.EntryID, p.EntryType, status); err != nil {
		h.sendConversationStatusError(
			conn,
			"internal_error",
			"Failed to update conversation status",
			p,
			currentStatus,
		)
		log.Printf("[ConversationHub] Error setting conversation status for %s (%s): %v", p.EntryID, p.EntryType, err)
		return
	}

	statusPayload := ConversationStatusUpdatePayload{
		EntryID:   p.EntryID,
		EntryType: p.EntryType,
		Status:    p.Status,
	}
	if status == conversation.ConversationStatusFinished {
		statusPayload.CloseSource = string(conversation.CloseSourceHuman)
		statusPayload.CloseReason = string(conversation.CloseReasonManual)
		now := time.Now().UTC().Format(time.RFC3339)
		statusPayload.ClosedAt = &now
	}

	h.broadcast <- &broadcastMessage{
		entryID:   p.EntryID,
		entryType: p.EntryType,
		event: &WSOutgoingMessage{
			Type:    WSEventConversationStatusUpdate,
			Payload: statusPayload,
		},
	}

	go h.BroadcastEntryUpdate(p.EntryID, p.EntryType, nil)
}

func (h *ConversationHub) handleSearchMessages(conn *WSConnection, payload json.RawMessage) {
	var p SearchMessagesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid search_messages payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.Query == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and query are required")
		return
	}

	if p.EntryType != "voice" && p.EntryType != "whatsapp" {
		h.sendError(conn, "invalid_entry_type", "entry_type must be 'voice' or 'whatsapp'")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	if h.messageRepo == nil {
		h.sendError(conn, "not_configured", "Message repository not configured")
		return
	}

	if p.Page <= 0 {
		p.Page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	go func() {
		input := conversation.SearchMessagesByEntryInput{
			EntryID:   p.EntryID,
			EntryType: shared.EntryType(p.EntryType),
			Query:     p.Query,
			Page:      p.Page,
			PageSize:  pageSize,
		}

		messages, totalItems, err := h.messageRepo.SearchMessagesByEntry(input)
		if err != nil {
			log.Printf("[ConversationHub] Error searching messages for %s:%s query=%q: %v", p.EntryType, p.EntryID, p.Query, err)
			h.sendError(conn, "search_error", "Failed to search messages: "+err.Error())
			return
		}

		if h.historyProvider != nil {
			leadName, leadNumber, leadPicture, _, _, _, _ := h.historyProvider.GetEntryInfo(p.EntryID, p.EntryType)
			_ = leadNumber
			_ = leadPicture
			for _, msg := range messages {
				if msg.MessageType == conversation.MessageTypeUserMessage {
					msg.SenderName = leadName
					msg.SenderAvatar = leadPicture
				}
			}
		}

		totalPages := 0
		if totalItems > 0 {
			totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventSearchMessagesResults,
			Payload: SearchMessagesResultsPayload{
				EntryID:    p.EntryID,
				EntryType:  p.EntryType,
				Query:      p.Query,
				Messages:   messages,
				Page:       p.Page,
				PageSize:   pageSize,
				TotalItems: totalItems,
				TotalPages: totalPages,
			},
		})
	}()
}

func (h *ConversationHub) handleLoadEntryMatches(conn *WSConnection, payload json.RawMessage) {
	var p LoadEntryMatchesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid load_entry_matches payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.Query == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and query are required")
		return
	}

	if p.EntryType != "voice" && p.EntryType != "whatsapp" {
		h.sendError(conn, "invalid_entry_type", "entry_type must be 'voice' or 'whatsapp'")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this conversation")
		return
	}

	if h.messageRepo == nil {
		h.sendError(conn, "not_configured", "Message repository not configured")
		return
	}

	if p.Page <= 0 {
		p.Page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	go func() {
		input := conversation.SearchMessagesByEntryInput{
			EntryID:   p.EntryID,
			EntryType: shared.EntryType(p.EntryType),
			Query:     p.Query,
			Page:      p.Page,
			PageSize:  pageSize,
		}

		messages, totalItems, err := h.messageRepo.SearchMessagesByEntry(input)
		if err != nil {
			log.Printf("[ConversationHub] Error loading entry matches for %s:%s query=%q: %v", p.EntryType, p.EntryID, p.Query, err)
			h.sendError(conn, "search_error", "Failed to load entry matches: "+err.Error())
			return
		}

		matches := make([]conversation.MatchedMessage, 0, len(messages))
		for _, msg := range messages {
			fromName := msg.From
			if msg.SenderName != "" {
				fromName = msg.SenderName
			}
			matches = append(matches, conversation.MatchedMessage{
				MessageID:   msg.ID,
				Text:        msg.Text,
				From:        fromName,
				MessageType: string(msg.MessageType),
				Channel:     string(msg.Channel),
				CreatedAt:   msg.CreatedAt,
			})
		}

		totalPages := 0
		totalItemsInt := int(totalItems)
		if totalItems > 0 {
			totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventEntryMatchesResults,
			Payload: EntryMatchesResultsPayload{
				EntryID:    p.EntryID,
				EntryType:  p.EntryType,
				Query:      p.Query,
				Matches:    matches,
				Page:       p.Page,
				PageSize:   pageSize,
				TotalItems: totalItemsInt,
				TotalPages: totalPages,
			},
		})
	}()
}

func (h *ConversationHub) handleRequestFunnelColumn(conn *WSConnection, payload json.RawMessage) {
	var p RequestFunnelColumnPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid request_kanban_column payload")
		return
	}

	if p.StageID == "" {
		h.sendError(conn, "missing_fields", "tag_id is required")
		return
	}

	if h.inboxService == nil {
		h.sendError(conn, "not_configured", "Inbox service not configured")
		return
	}

	if p.Page <= 0 {
		p.Page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}

	userID := conn.UserID
	departmentID := conn.DepartmentID
	campaignID := conn.CampaignID
	campaignType := conn.CampaignType
	whatsAppCampaignType := conn.WhatsAppCampaignType
	viewMode := conn.ViewMode
	workspaceID := conn.WorkspaceID
	isAdmin := conn.IsAdmin
	seq := conn.viewSeq

	go func() {
		input := conversation.SearchInboxInput{
			UserID:               userID,
			SelectedDepartmentID: departmentID,
			CampaignID:           campaignID,
			CampaignType:         campaignType,
			WhatsAppCampaignType: whatsAppCampaignType,
			StageID:              p.StageID,
			Page:                 p.Page,
			PageSize:             pageSize,
			SortOrder:            "desc",
			AssignedUserID:       userID,
			IsAdmin:              isAdmin,
		}
		if viewMode == "global" {
			input.WorkspaceID = workspaceID
		}
		entries, totalItems, err := h.inboxService.SearchInbox(userID, input)

		if err != nil {
			log.Printf("[ConversationHub] Error fetching kanban column %s: %v", p.StageID, err)
			h.sendError(conn, "fetch_error", "Failed to fetch kanban column: "+err.Error())
			return
		}

		if conn.viewSeq != seq {
			log.Printf("[ConversationHub] Discarding stale kanban column %s (seq %d < %d)", p.StageID, seq, conn.viewSeq)
			return
		}

		totalPages := 0
		if totalItems > 0 {
			totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventFunnelColumn,
			Payload: FunnelColumnPayload{
				StageID:    p.StageID,
				Entries:    entries,
				Page:       p.Page,
				PageSize:   pageSize,
				TotalItems: totalItems,
				TotalPages: totalPages,
			},
		})
	}()
}

func (h *ConversationHub) handleRequestFunnelSummary(conn *WSConnection, payload json.RawMessage) {
	var p RequestFunnelSummaryPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid request_kanban_summary payload")
		return
	}

	if len(p.StageIDs) == 0 {
		h.sendError(conn, "missing_fields", "tag_ids is required")
		return
	}

	if h.inboxService == nil {
		h.sendError(conn, "not_configured", "Inbox service not configured")
		return
	}

	userID := conn.UserID
	departmentID := conn.DepartmentID
	campaignID := conn.CampaignID
	campaignType := conn.CampaignType
	whatsAppCampaignType := conn.WhatsAppCampaignType
	viewMode := conn.ViewMode
	workspaceID := conn.WorkspaceID
	isAdmin := conn.IsAdmin
	seq := conn.viewSeq

	go func() {
		type tagResult struct {
			idx     int
			StageID string
			count   int64
		}

		results := make(chan tagResult, len(p.StageIDs))
		for i, StageID := range p.StageIDs {
			go func(idx int, tid string) {
				input := conversation.SearchInboxInput{
					UserID:               userID,
					SelectedDepartmentID: departmentID,
					CampaignID:           campaignID,
					CampaignType:         campaignType,
					WhatsAppCampaignType: whatsAppCampaignType,
					StageID:              tid,
					Page:                 1,
					PageSize:             1,
					AssignedUserID:       userID,
					IsAdmin:              isAdmin,
				}
				if viewMode == "global" {
					input.WorkspaceID = workspaceID
				}
				_, totalItems, err := h.inboxService.SearchInbox(userID, input)
				if err != nil {
					log.Printf("[ConversationHub] Error getting count for tag %s: %v", tid, err)
					totalItems = 0
				}
				results <- tagResult{idx: idx, StageID: tid, count: totalItems}
			}(i, StageID)
		}

		columns := make([]FunnelColumnSummary, len(p.StageIDs))
		for range p.StageIDs {
			r := <-results
			columns[r.idx] = FunnelColumnSummary{
				StageID:    r.StageID,
				TotalItems: r.count,
			}
		}

		if conn.viewSeq != seq {
			log.Printf("[ConversationHub] Discarding stale kanban summary (seq %d < %d)", seq, conn.viewSeq)
			return
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventFunnelSummary,
			Payload: FunnelSummaryPayload{
				Columns: columns,
			},
		})
	}()
}

func (h *ConversationHub) handleReopenWindow(conn *WSConnection, payload json.RawMessage) {
	var p ReopenWindowPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid reopen_window payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.TemplateID == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and template_id are required")
		return
	}

	if !h.authorizer.CanAccessEntry(conn.UserID, conn.WorkspaceID, p.EntryID, p.EntryType, conn.IsAdmin) {
		h.sendError(conn, "unauthorized", "You don't have access to this entry")
		return
	}

	if h.templateSender == nil {
		h.sendError(conn, "not_configured", "Template sender not configured")
		return
	}

	go func() {
		messageID, err := h.templateSender.SendTemplate(p.EntryID, p.EntryType, p.TemplateID, p.Parameters, conn.UserID, conn.WorkspaceID)
		if err != nil {
			log.Printf("[ConversationHub] Error sending template for reopen window: %v", err)
			h.sendError(conn, "template_send_failed", "Failed to send template: "+err.Error())
			return
		}

		log.Printf("[ConversationHub] Template sent to reopen window for entry %s (%s), messageID=%s", p.EntryID, p.EntryType, messageID)

		if h.eventLogger != nil {
			h.eventLogger.Log(&ce.ConversationEvent{
				WorkspaceID: conn.WorkspaceID,
				EntryID:     p.EntryID,
				EntryType:   p.EntryType,
				EventType:   ce.EventReopened,
				ActorID:     conn.UserID,
				Details:     ce.DetailsJSON(map[string]string{"template_id": p.TemplateID, "message_id": messageID}),
			})
		}

		h.sendToConnection(conn, &WSOutgoingMessage{
			Type: WSEventWindowReopened,
			Payload: WindowReopenedPayload{
				EntryID:    p.EntryID,
				EntryType:  p.EntryType,
				RequestID:  p.RequestID,
				TemplateID: p.TemplateID,
				MessageID:  messageID,
			},
		})
	}()
}

func (h *ConversationHub) handleAssignTo(conn *WSConnection, payload json.RawMessage) {
	var p AssignToPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendError(conn, "invalid_payload", "Invalid assign_to payload")
		return
	}

	if p.EntryID == "" || p.EntryType == "" || p.UserID == "" {
		h.sendError(conn, "missing_fields", "entry_id, entry_type, and user_id are required")
		return
	}

	if (h.assignmentService == nil && h.assignmentRepo == nil) || h.workspaceResolver == nil {
		h.sendError(conn, "not_configured", "Assignment service not configured")
		return
	}

	workspaceID, err := h.workspaceResolver.GetEntryWorkspaceID(p.EntryID, p.EntryType)
	if err != nil || workspaceID == "" {
		h.sendError(conn, "resolve_failed", "Could not resolve workspace for entry")
		return
	}

	if workspaceID != conn.WorkspaceID && !conn.IsAdmin {
		h.sendError(conn, "unauthorized", "Cannot assign entry from a different workspace")
		return
	}

	if h.authorizer != nil {
		if _, allowed := h.authorizer.GetDepartmentScope(p.UserID, workspaceID, false); !allowed {
			h.sendError(conn, "unauthorized", "Target user is not a workspace member with conversation access")
			return
		}
	}

	// Department-scoped guard: the caller may only hand a conversation to members
	// they are allowed to see (their own department(s), unless they are an
	// owner/admin or hold members:view_others). Enforced on the mutation itself
	// so a crafted payload cannot target a member outside the caller's scope.
	if h.memberVisibility != nil {
		canView, err := h.memberVisibility.CanView(conn.UserID, p.UserID, workspaceID, conn.IsAdmin)
		if err != nil {
			log.Printf("[ConversationHub] handleAssignTo: visibility check failed for caller %s → target %s: %v", conn.UserID, p.UserID, err)
			h.sendError(conn, "assign_failed", "Failed to validate assignment target")
			return
		}
		if !canView {
			h.sendError(conn, "unauthorized", "You cannot assign conversations to members outside your departments")
			return
		}
	}

	businessPhoneID := ""
	if p.EntryType == "whatsapp" && h.waCampaignRepo != nil {
		campaignID, _ := h.workspaceResolver.GetEntryCampaignID(p.EntryID, p.EntryType)
		if campaignID != "" {
			if wc, err := h.waCampaignRepo.FindByID(campaignID); err == nil && wc != nil {
				businessPhoneID = wc.BusinessPhoneID
			}
		}
	}

	if h.assignmentService != nil {
		if err := h.assignmentService.AssignManual(p.EntryID, p.EntryType, businessPhoneID, workspaceID, p.UserID, conn.UserID, inbox_assignment.TriggerManual); err != nil {
			log.Printf("[ConversationHub] handleAssignTo: error assigning entry %s → user %s: %v", p.EntryID, p.UserID, err)
			h.sendError(conn, "assign_failed", "Failed to assign entry")
			return
		}
	} else {
		assignment := &inbox_assignment.InboxAssignment{
			WorkspaceID:     workspaceID,
			BusinessPhoneID: businessPhoneID,
			EntryID:         p.EntryID,
			EntryType:       p.EntryType,
			AssignedUserID:  p.UserID,
		}
		if err := h.assignmentRepo.Assign(assignment); err != nil {
			log.Printf("[ConversationHub] handleAssignTo: error assigning entry %s → user %s: %v", p.EntryID, p.UserID, err)
			h.sendError(conn, "assign_failed", "Failed to assign entry")
			return
		}
		if h.eventLogger != nil {
			h.eventLogger.Log(ce.New(workspaceID, p.EntryID, p.EntryType, ce.EventAssigned).
				WithActorHuman(conn.UserID).
				WithDetails(map[string]string{"to_user_id": p.UserID, "trigger": "manual"}).
				Build())
		}
	}

	log.Printf("[ConversationHub] Entry %s (%s) manually assigned to user %s by %s", p.EntryID, p.EntryType, p.UserID, conn.UserID)

	go h.BroadcastEntryUpdate(p.EntryID, p.EntryType, nil)

	go h.BroadcastEntryRemoved(p.EntryID, p.EntryType, workspaceID, p.UserID)
}

func (h *ConversationHub) handleBroadcast(msg *broadcastMessage) {

	if !msg.fromRedis {
		h.publishBroadcastToRedis(msg)
	}

	sub := entrySubscription{entryID: msg.entryID, entryType: msg.entryType}

	h.subMu.RLock()
	subscribers := h.entrySubscribers[sub]
	h.subMu.RUnlock()

	if len(subscribers) == 0 {
		return
	}

	var broadcastMsgID string
	if msg.event != nil {
		switch p := msg.event.Payload.(type) {
		case MessagePayload:
			if p.Message != nil {
				broadcastMsgID = p.Message.ID
			}
		case MessageSentPayload:
			if p.Message != nil {
				broadcastMsgID = p.Message.ID
			}
		}
	}

	data, err := json.Marshal(msg.event)
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling event: %v", err)
		return
	}

	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for userID := range subscribers {
		if msg.excludeUserID != "" && userID == msg.excludeUserID {
			continue
		}

		connIDs, ok := h.userConnections[userID]
		if !ok {
			continue
		}

		for connID := range connIDs {

			if broadcastMsgID != "" {
				if connSubs, ok := h.sentMessageIDs[connID]; ok {
					if ids, ok := connSubs[sub]; ok && ids[broadcastMsgID] {
						delete(ids, broadcastMsgID)
						continue
					}
				}
			}

			if conn, exists := h.connections[connID]; exists {
				select {
				case conn.Send <- data:
				default:
					log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", userID, connID)
				}
			}

			if broadcastMsgID != "" {
				if h.sentMessageIDs[connID] == nil {
					h.sentMessageIDs[connID] = make(map[entrySubscription]map[string]bool)
				}
				if h.sentMessageIDs[connID][sub] == nil {
					h.sentMessageIDs[connID][sub] = make(map[string]bool)
				}
				ids := h.sentMessageIDs[connID][sub]
				ids[broadcastMsgID] = true

				if len(ids) > maxSentIDsPerSubscription {
					count := 0
					for k := range ids {
						delete(ids, k)
						count++
						if len(ids) <= maxSentIDsPerSubscription {
							break
						}
					}
				}
			}
		}
	}
}

func (h *ConversationHub) sendToConnection(conn *WSConnection, msg *WSOutgoingMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling message: %v", err)
		return
	}
	h.sendRawToConnection(conn, data)
}

func (h *ConversationHub) sendRawToConnection(conn *WSConnection, data []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ConversationHub] Recovered from send panic for user %s: %v", conn.UserID, r)
		}
	}()

	select {
	case conn.Send <- data:
	default:
		log.Printf("[ConversationHub] Send buffer full for user %s", conn.UserID)
	}
}

func (h *ConversationHub) sendError(conn *WSConnection, code, message string) {
	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventError,
		Payload: ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

func (h *ConversationHub) sendConversationStatusError(
	conn *WSConnection,
	code, message string,
	p SetConversationStatusPayload,
	previousStatus conversation.ConversationStatus,
) {
	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventError,
		Payload: ErrorPayload{
			Code:           code,
			Message:        message,
			EntryID:        p.EntryID,
			EntryType:      p.EntryType,
			Status:         p.Status,
			PreviousStatus: previousStatus.String(),
		},
	})
}

func (h *ConversationHub) SendTemplateForEntry(entryID, entryType, templateID string, parameters []string, userID, workspaceID string, isAdmin bool) (string, error) {
	if h.templateSender == nil {
		return "", fmt.Errorf("template sender not configured")
	}
	if h.authorizer != nil && !h.authorizer.CanAccessEntry(userID, workspaceID, entryID, entryType, isAdmin) {
		return "", fmt.Errorf("unauthorized: you don't have access to this entry")
	}
	return h.templateSender.SendTemplate(entryID, entryType, templateID, parameters, userID, workspaceID)
}

type redisBroadcastPayload struct {
	ReplicaID     string             `json:"r"`
	EntryID       string             `json:"e"`
	EntryType     string             `json:"t"`
	ExcludeUserID string             `json:"x,omitempty"`
	Event         *WSOutgoingMessage `json:"ev"`
}

func (h *ConversationHub) publishBroadcastToRedis(msg *broadcastMessage) {
	data, err := json.Marshal(redisBroadcastPayload{
		ReplicaID:     h.replicaID,
		EntryID:       msg.entryID,
		EntryType:     msg.entryType,
		ExcludeUserID: msg.excludeUserID,
		Event:         msg.event,
	})
	if err != nil {
		return
	}
	_ = h.sharedState.Publish("hub:broadcast", data)
}

func (h *ConversationHub) runRedisBroadcastSubscriber() {
	ctx := context.Background()
	h.sharedState.Subscribe(ctx, "hub:broadcast", func(raw []byte) {
		var p redisBroadcastPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		if p.ReplicaID == h.replicaID {
			return
		}
		h.broadcast <- &broadcastMessage{
			entryID:       p.EntryID,
			entryType:     p.EntryType,
			event:         p.Event,
			excludeUserID: p.ExcludeUserID,
			fromRedis:     true,
		}
	})
}

type redisWorkspaceBroadcast struct {
	Type             string `json:"t"`
	EntryID          string `json:"e,omitempty"`
	EntryType        string `json:"et,omitempty"`
	StageWorkspaceID string `json:"u,omitempty"`
	WorkspaceID      string `json:"w,omitempty"`
	ExcludeUserID    string `json:"x,omitempty"`
	ReplicaID        string `json:"r"`
}

func (h *ConversationHub) publishWorkspaceBroadcast(bType, entryID, entryType, stageWorkspaceID, workspaceID, excludeUserID string) {
	if h.sharedState == nil {
		return
	}
	data, err := json.Marshal(redisWorkspaceBroadcast{
		Type:             bType,
		EntryID:          entryID,
		EntryType:        entryType,
		StageWorkspaceID: stageWorkspaceID,
		WorkspaceID:      workspaceID,
		ExcludeUserID:    excludeUserID,
		ReplicaID:        h.replicaID,
	})
	if err != nil {
		return
	}
	_ = h.sharedState.Publish("hub:workspace_broadcast", data)
}

func (h *ConversationHub) runRedisWorkspaceBroadcastSubscriber() {
	ctx := context.Background()
	h.sharedState.Subscribe(ctx, "hub:workspace_broadcast", func(raw []byte) {
		var p redisWorkspaceBroadcast
		if err := json.Unmarshal(raw, &p); err != nil {
			return
		}
		if p.ReplicaID == h.replicaID {
			return
		}
		switch p.Type {
		case "stage_update":
			h.broadcastStageUpdateLocal(p.StageWorkspaceID, p.EntryID, p.EntryType)
		case "label_update":
			h.broadcastLabelUpdateLocal(p.StageWorkspaceID, p.EntryID, p.EntryType)
		case "entry_update":
			h.broadcastEntryUpdateLocal(p.EntryID, p.EntryType, nil)
		case "entry_removed":
			h.broadcastEntryRemovedLocal(p.EntryID, p.EntryType, p.WorkspaceID, p.ExcludeUserID)
		}
	})
}

func (h *ConversationHub) runReplicaHeartbeat() {
	key := "replica:heartbeat:" + h.replicaID
	const heartbeatTTL = 30 * time.Second
	const hubKeyTTL = 45 * time.Second

	refresh := func() {
		value := h.publicAddress
		if value == "" {
			value = "1"
		}
		_ = h.sharedState.SetString(key, value, heartbeatTTL)

		_ = h.sharedState.SAdd("cluster:replicas", h.replicaID)

		h.sharedState.Expire("hub:workspaces", hubKeyTTL)
		h.connMu.RLock()
		workspaces := make(map[string]struct{})
		for _, conn := range h.connections {
			workspaces[conn.WorkspaceID] = struct{}{}
		}
		h.connMu.RUnlock()
		for wsID := range workspaces {
			h.sharedState.Expire("hub:connected_users:"+wsID, hubKeyTTL)
			h.sharedState.Expire("replica:active_calls:"+wsID, hubKeyTTL)
		}
	}

	refresh()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		refresh()
	}
}

func (h *ConversationHub) runStaleReplicaCleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		workspaces, err := h.sharedState.SMembers("hub:workspaces")
		if err != nil {
			continue
		}
		for _, wsID := range workspaces {
			members, err := h.sharedState.SMembers("hub:connected_users:" + wsID)
			if err != nil {
				continue
			}
			for _, member := range members {
				parts := strings.SplitN(member, "|", 2)
				if len(parts) != 2 {
					continue
				}
				replicaID := parts[1]
				if replicaID == h.replicaID {
					continue
				}
				exists, _ := h.sharedState.Exists("replica:heartbeat:" + replicaID)
				if !exists {
					_ = h.sharedState.SRem("hub:connected_users:"+wsID, member)
				}
			}

			calls, err := h.sharedState.HGetAll("replica:active_calls:" + wsID)
			if err != nil {
				continue
			}
			for key := range calls {
				parts := strings.SplitN(key, "|", 2)
				if len(parts) != 2 {
					continue
				}
				replicaID := parts[1]
				if replicaID == h.replicaID {
					continue
				}
				exists, _ := h.sharedState.Exists("replica:heartbeat:" + replicaID)
				if !exists {
					_ = h.sharedState.HDel("replica:active_calls:"+wsID, key)
				}
			}

			remaining, _ := h.sharedState.SMembers("hub:connected_users:" + wsID)
			if len(remaining) == 0 {
				_ = h.sharedState.SRem("hub:workspaces", wsID)
				_ = h.sharedState.Del("hub:connected_users:" + wsID)
			}
		}
	}
}
