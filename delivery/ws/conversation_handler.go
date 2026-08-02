package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"vozko/domain/shared"
	"vozko/domain/user"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

const (
	conversationWriteWait      = 10 * time.Second
	conversationPongWait       = 60 * time.Second
	conversationPingPeriod     = (conversationPongWait * 9) / 10
	conversationMaxMessageSize = 512 * 1024
)

type ConversationWSHandler struct {
	hub    *ConversationHub
	logger *log.Logger
}

func NewConversationWSHandler(hub *ConversationHub, logger *log.Logger) *ConversationWSHandler {
	if hub == nil {
		return nil
	}
	if logger == nil {
		logger = log.Default()
	}
	return &ConversationWSHandler{
		hub:    hub,
		logger: logger,
	}
}

func resolveConnectionDepartmentID(r *http.Request) string {
	if r == nil {
		return ""
	}

	if departmentID := strings.TrimSpace(r.URL.Query().Get("departmentId")); departmentID != "" {
		return departmentID
	}

	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return ""
	}

	departmentID, err := filter.DepartmentIDForCreation()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(departmentID)
}

func (h *ConversationWSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.hub == nil {
		http.Error(w, "Conversation WebSocket not configured", http.StatusNotImplemented)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := r.URL.Query()
	campaignID := query.Get("campaignId")
	campaignType := query.Get("campaignType")

	// Empty means the global (all-channel) inbox. Otherwise the selector must
	// name a channel the inbox can be scoped to; the message is generated from
	// the domain set so it cannot drift from what is actually accepted.
	if campaignType != "" && !shared.EntryType(campaignType).SupportsInboxScope() {
		http.Error(w, "campaignType must be "+shared.FormatEntryTypes(shared.InboxScopableEntryTypes()), http.StatusBadRequest)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if qsWS := query.Get("workspaceId"); qsWS != "" {
		workspaceID = qsWS
	}
	if workspaceID == "" {
		http.Error(w, "workspace is required", http.StatusForbidden)
		return
	}

	departmentID := resolveConnectionDepartmentID(r)

	isSystemAdmin := claims.Role == string(user.RoleAdmin)
	if !isSystemAdmin && h.hub.authorizer != nil {
		isSystemAdmin := claims.Role == string(user.RoleAdmin)
		allowed := h.hub.authorizer.HasWorkspacePermission(
			claims.UserID,
			workspaceID,
			string(workspace_domain.ResourceConversations),
			string(workspace_domain.ActionRead),
			isSystemAdmin,
		)
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	viewMode := "global"
	if campaignID != "" && campaignType != "" {
		viewMode = "campaign"
		if h.hub.workspaceResolver != nil {
			campaignWorkspaceID, err := h.hub.workspaceResolver.GetCampaignWorkspaceID(campaignID, campaignType)
			if err != nil || campaignWorkspaceID == "" {
				http.Error(w, "campaign not found", http.StatusNotFound)
				return
			}
			if campaignWorkspaceID != workspaceID {
				http.Error(w, "campaign does not belong to workspace", http.StatusForbidden)
				return
			}
		}
		if h.hub.authorizer != nil && !h.hub.authorizer.CanAccessCampaign(claims.UserID, workspaceID, campaignID, campaignType, isSystemAdmin) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	} else {
		campaignID = ""
		campaignType = ""
	}

	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Printf("[ConversationWS] Upgrade error: %v", err)
		return
	}

	conn := &WSConnection{
		ID:           uuid.New().String(),
		UserID:       claims.UserID,
		WorkspaceID:  workspaceID,
		DepartmentID: departmentID,
		IsAdmin:      claims.Role == string(user.RoleAdmin),
		Conn:         ws,
		Send:         make(chan []byte, 256),
		CampaignID:   campaignID,
		CampaignType: campaignType,
		ViewMode:     viewMode,
		Done:         make(chan struct{}),
		connectedAt:  time.Now(),
	}

	h.hub.RegisterConnection(conn)

	go h.writePump(conn)
	go h.readPump(conn)
}

func (h *ConversationWSHandler) readPump(conn *WSConnection) {
	defer func() {
		h.hub.UnregisterConnection(conn)
		conn.Conn.Close()
	}()

	conn.Conn.SetReadLimit(conversationMaxMessageSize)
	conn.Conn.SetReadDeadline(time.Now().Add(conversationPongWait))
	conn.Conn.SetPongHandler(func(string) error {
		conn.Conn.SetReadDeadline(time.Now().Add(conversationPongWait))
		return nil
	})

	for {
		_, message, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				h.logger.Printf("[ConversationWS] User %s connection closed: %v", conn.UserID, err)
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Printf("[ConversationWS] User %s unexpected error: %v", conn.UserID, err)
			} else {
				h.logger.Printf("[ConversationWS] User %s read error: %v", conn.UserID, err)
			}
			break
		}

		var incoming WSIncomingMessage
		if err := json.Unmarshal(message, &incoming); err != nil {
			h.logger.Printf("[ConversationWS] Invalid message from user %s: %v", conn.UserID, err)
			continue
		}

		h.hub.HandleMessage(conn, &incoming)
	}
}

func (h *ConversationWSHandler) writePump(conn *WSConnection) {
	ticker := time.NewTicker(conversationPingPeriod)
	defer func() {
		ticker.Stop()
		conn.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-conn.Send:
			conn.Conn.SetWriteDeadline(time.Now().Add(conversationWriteWait))
			if !ok {
				h.logger.Printf("[ConversationWS] User %s send channel closed", conn.UserID)
				conn.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := conn.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				h.logger.Printf("[ConversationWS] User %s write error: %v", conn.UserID, err)
				return
			}

			n := len(conn.Send)
			for i := 0; i < n; i++ {
				queued := <-conn.Send
				if err := conn.Conn.WriteMessage(websocket.TextMessage, queued); err != nil {
					h.logger.Printf("[ConversationWS] User %s write error (batch): %v", conn.UserID, err)
					return
				}
			}

		case <-ticker.C:
			conn.Conn.SetWriteDeadline(time.Now().Add(conversationWriteWait))
			if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.logger.Printf("[ConversationWS] User %s ping error: %v", conn.UserID, err)
				return
			}
		}
	}
}
