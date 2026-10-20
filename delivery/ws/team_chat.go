package ws

import (
	"context"
	"encoding/json"
	"log"
	"runtime/debug"
	"strings"

	"vozko/delivery/teamchatview"
	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

const teamChatBroadcastType = "team_chat"

type teamChatTyping interface {
	Typing(ctx context.Context, by shared.Person, workspaceID, threadID string) error
}

type TeamChatMessageEvent struct {
	ThreadID string               `json:"thread_id"`
	Message  teamchatview.Message `json:"message"`
}

type TeamChatThreadEvent struct {
	ThreadID string `json:"thread_id"`
}

type TeamChatTypingEvent struct {
	ThreadID string `json:"thread_id"`
	UserID   string `json:"user_id"`
}

type TeamChatScheduledEvent struct {
	ThreadID  string                 `json:"thread_id"`
	Scheduled teamchatview.Scheduled `json:"scheduled"`
}

type TeamChatTypingPayload struct {
	ThreadID string `json:"thread_id"`
}

func (h *ConversationHub) SetTeamChat(chat teamChatTyping) {
	h.teamChat = chat
}

func (h *ConversationHub) AnnounceTeamMessage(workspaceID string, recipients []string, m teamchat.AuthoredMessage) {
	h.pushTeamChat(workspaceID, recipients, WSEventTeamChatMessage, TeamChatMessageEvent{ThreadID: m.ThreadID, Message: teamchatview.MessageOf(m)})
}

func (h *ConversationHub) AnnounceTeamMessageChanged(workspaceID string, recipients []string, m teamchat.AuthoredMessage) {
	h.pushTeamChat(workspaceID, recipients, WSEventTeamChatMessageUpdated, TeamChatMessageEvent{ThreadID: m.ThreadID, Message: teamchatview.MessageOf(m)})
}

func (h *ConversationHub) AnnounceTeamRead(workspaceID string, recipients []string, mark teamchat.ReadMark) {
	h.pushTeamChat(workspaceID, recipients, WSEventTeamChatRead, teamchatview.ReadMarkOf(mark))
}

func (h *ConversationHub) AnnounceTeamThread(workspaceID string, recipients []string, threadID string) {
	h.pushTeamChat(workspaceID, recipients, WSEventTeamChatThread, TeamChatThreadEvent{ThreadID: threadID})
}

func (h *ConversationHub) AnnounceTeamThreadRemoved(workspaceID string, recipients []string, threadID string) {
	h.pushTeamChat(workspaceID, recipients, WSEventTeamChatThreadRemoved, TeamChatThreadEvent{ThreadID: threadID})
}

func (h *ConversationHub) AnnounceTeamTyping(workspaceID string, recipients []string, threadID, userID string) {
	h.pushTeamChat(workspaceID, recipients, WSEventTeamChatTypingRemote, TeamChatTypingEvent{ThreadID: threadID, UserID: userID})
}

func (h *ConversationHub) AnnounceTeamScheduled(workspaceID, userID string, s teamchat.Scheduled) {
	h.pushTeamChat(workspaceID, []string{userID}, WSEventTeamChatScheduled, TeamChatScheduledEvent{ThreadID: s.ThreadID, Scheduled: teamchatview.ScheduledOf(s)})
}

func (h *ConversationHub) pushTeamChat(workspaceID string, recipients []string, kind WSEventType, payload interface{}) {
	if h == nil || workspaceID == "" || len(recipients) == 0 {
		return
	}
	data, err := json.Marshal(&WSOutgoingMessage{Type: kind, Payload: payload})
	if err != nil {
		log.Printf("[ConversationHub] team chat push: %v", err)
		return
	}
	h.sendToUsers(workspaceID, recipients, data)
	h.publishUserPayload(teamChatBroadcastType, workspaceID, recipients, data)
}

func (h *ConversationHub) handleTeamChatTyping(conn *WSConnection, payload json.RawMessage) {
	var p TeamChatTypingPayload
	if h.teamChat == nil || json.Unmarshal(payload, &p) != nil {
		return
	}
	threadID := strings.TrimSpace(p.ThreadID)
	if threadID == "" {
		return
	}
	by, workspaceID := connPerson(conn), conn.WorkspaceID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ConversationHub] PANIC in team chat typing (thread=%s user=%s): %v\n%s", threadID, conn.UserID, r, debug.Stack())
			}
		}()
		_ = h.teamChat.Typing(context.Background(), by, workspaceID, threadID)
	}()
}
