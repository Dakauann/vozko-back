package ws

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"

	"vozko/domain/conversation"
)

type WSEventType string

const (
	WSEventSubscribe             WSEventType = "subscribe"
	WSEventUnsubscribe           WSEventType = "unsubscribe"
	WSEventSend                  WSEventType = "send"
	WSEventSendButton            WSEventType = "send_button"
	WSEventMarkRead              WSEventType = "mark_read"
	WSEventTyping                WSEventType = "typing"
	WSEventRequestConnectedUsers WSEventType = "request_connected_users"
	WSEventLoadHistory           WSEventType = "load_history"
	WSEventLoadAround            WSEventType = "load_around"
	WSEventRequestInbox          WSEventType = "request_inbox_page"
	WSEventSearchInbox           WSEventType = "search_inbox"
	WSEventSearchMessages        WSEventType = "search_messages"
	WSEventLoadEntryMatches      WSEventType = "load_entry_matches"
	WSEventReopenWindow          WSEventType = "reopen_window"
	WSEventAssignTo              WSEventType = "assign_to"
	WSEventRequestFunnelColumn   WSEventType = "request_funnel_column"
	WSEventRequestFunnelSummary  WSEventType = "request_funnel_summary"
	WSEventSwitchView            WSEventType = "switch_view"

	WSEventHistory                        WSEventType = "conversation:history"
	WSEventMessage                        WSEventType = "conversation:message"
	WSEventMessageSent                    WSEventType = "conversation:message_sent"
	WSEventMessageError                   WSEventType = "conversation:message_error"
	WSEventRead                           WSEventType = "conversation:read"
	WSEventUnreadCount                    WSEventType = "conversation:unread"
	WSEventTypingRemote                   WSEventType = "conversation:typing"
	WSEventSubscribed                     WSEventType = "conversation:subscribed"
	WSEventUnsubscribed                   WSEventType = "conversation:unsubscribed"
	WSEventError                          WSEventType = "conversation:error"
	WSEventMediaReady                     WSEventType = "conversation:media_ready"
	WSEventConnected                      WSEventType = "conversation:connected"
	WSEventInbox                          WSEventType = "conversation:inbox"
	WSEventEntryUpdate                    WSEventType = "conversation:entry_update"
	WSEventStageUpdate                    WSEventType = "conversation:stage_update"
	WSEventLabelUpdate                    WSEventType = "conversation:label_update"
	WSEventSearchResults                  WSEventType = "conversation:search_results"
	WSEventConnectedUsers                 WSEventType = "conversation:connected_users"
	WSEventSearchMessagesResults          WSEventType = "conversation:search_messages_results"
	WSEventEntryMatchesResults            WSEventType = "conversation:entry_matches_results"
	WSEventWindowReopened                 WSEventType = "conversation:window_reopened"
	WSEventMessageStatus                  WSEventType = "conversation:message_status"
	WSEventFunnelColumn                   WSEventType = "conversation:funnel_column"
	WSEventFunnelSummary                  WSEventType = "conversation:funnel_summary"
	WSEventViewSwitched                   WSEventType = "conversation:view_switched"
	WSEventEntryRemoved                   WSEventType = "conversation:entry_removed"
	WSEventAnalysisUpdate                 WSEventType = "conversation:analysis_update"
	WSEventConversationStatusUpdate       WSEventType = "conversation:conversation_status_update"
	WSEventConversationStatusCountsUpdate WSEventType = "conversation:conversation_status_counts_update"

	WSEventStartCall WSEventType = "start_call"
	WSEventEndCall   WSEventType = "end_call"
	WSEventCallAudio WSEventType = "call_audio"

	WSEventInboundCallAccept  WSEventType = "call:incoming_accept"
	WSEventInboundCallDecline WSEventType = "call:incoming_decline"

	WSEventTransferInitiate    WSEventType = "transfer:initiate"
	WSEventTransferAccept      WSEventType = "transfer:accept"
	WSEventTransferDecline     WSEventType = "transfer:decline"
	WSEventTransferComplete    WSEventType = "transfer:complete"
	WSEventTransferCancel      WSEventType = "transfer:cancel"
	WSEventTransferListTargets WSEventType = "transfer:list_targets"

	WSEventTransferOffer      WSEventType = "transfer:offer"
	WSEventTransferStarted    WSEventType = "transfer:started"
	WSEventTransferCompleted  WSEventType = "transfer:completed"
	WSEventTransferDeclined   WSEventType = "transfer:declined"
	WSEventTransferCancelled  WSEventType = "transfer:cancelled"
	WSEventTransferConsulting WSEventType = "transfer:consulting"
	WSEventTransferTimedOut   WSEventType = "transfer:timed_out"
	WSEventTransferError      WSEventType = "transfer:error"
	WSEventTransferTargets    WSEventType = "transfer:targets"

	WSEventSetConversationStatus WSEventType = "set_conversation_status"

	WSEventCallStatus      WSEventType = "call:status"
	WSEventCallAudioS      WSEventType = "call:audio"
	WSEventCallEnded       WSEventType = "call:ended"
	WSEventInboundCall     WSEventType = "call:incoming"
	WSEventCallTrunkBusy   WSEventType = "call:trunk_busy"
	WSEventWaitingCallSlot WSEventType = "call:waiting_slot"

	WSEventDialerPresence WSEventType = "dialer:presence"
	// Supervisor live concurrency board (humans + AI seats + capacity).
	WSEventTelephonyBoard WSEventType = "telephony:board"
)

type WSIncomingMessage struct {
	Type    WSEventType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type WSOutgoingMessage struct {
	Type    WSEventType `json:"type"`
	Payload interface{} `json:"payload"`
}

type WSConnection struct {
	ID                   string
	UserID               string
	WorkspaceID          string
	DepartmentID         string
	IsAdmin              bool
	Conn                 *websocket.Conn
	Send                 chan []byte
	CampaignID           string
	CampaignType         string
	WhatsAppCampaignType string
	CampaignWorkspaceID  string
	ViewMode             string
	ConversationStatus   string
	Done                 chan struct{}
	viewSeq              uint64
	connectedAt          time.Time
}

type SubscribePayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	PageSize  int    `json:"page_size,omitempty"`
}

type LoadHistoryPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Before    string `json:"before"`
	PageSize  int    `json:"page_size,omitempty"`
}

type LoadAroundPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Timestamp string `json:"timestamp"`
	PageSize  int    `json:"page_size,omitempty"`
}

type RequestInboxPagePayload struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size,omitempty"`
}

type SearchInboxPayload struct {
	Query string `json:"query,omitempty"`

	StageID            string `json:"stage_id,omitempty"`
	StageName          string `json:"stage_name,omitempty"`
	MinMessageCount    *int   `json:"min_message_count,omitempty"`
	MaxMessageCount    *int   `json:"max_message_count,omitempty"`
	MessageSearch      string `json:"message_search,omitempty"`
	WindowOpen         *bool  `json:"window_open,omitempty"`
	HasUnread          *bool  `json:"has_unread,omitempty"`
	Channel            string `json:"channel,omitempty"`
	DateFrom           string `json:"date_from,omitempty"`
	DateTo             string `json:"date_to,omitempty"`
	ConversationStatus string `json:"conversation_status,omitempty"`

	// User-facing "filter by responsible": a member id, or Unassigned for the
	// no-responsible pool. Kept separate from the permission-scope AssignedUserID.
	ResponsibleUserID     string `json:"responsible_user_id,omitempty"`
	ResponsibleUnassigned bool   `json:"responsible_unassigned,omitempty"`

	Page     int `json:"page,omitempty"`
	PageSize int `json:"page_size,omitempty"`
}

type SearchMessagesPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Query     string `json:"query"`
	Page      int    `json:"page,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
}

type SearchMessagesResultsPayload struct {
	EntryID    string                  `json:"entry_id"`
	EntryType  string                  `json:"entry_type"`
	Query      string                  `json:"query"`
	Messages   []*conversation.Message `json:"messages"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"page_size"`
	TotalItems int64                   `json:"total_items"`
	TotalPages int                     `json:"total_pages"`
}

type LoadEntryMatchesPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Query     string `json:"query"`
	Page      int    `json:"page,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
}

type EntryMatchesResultsPayload struct {
	EntryID    string                        `json:"entry_id"`
	EntryType  string                        `json:"entry_type"`
	Query      string                        `json:"query"`
	Matches    []conversation.MatchedMessage `json:"matches"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalItems int                           `json:"total_items"`
	TotalPages int                           `json:"total_pages"`
}

type ReopenWindowPayload struct {
	EntryID    string   `json:"entry_id"`
	EntryType  string   `json:"entry_type"`
	TemplateID string   `json:"template_id"`
	Parameters []string `json:"parameters,omitempty"`
	RequestID  string   `json:"request_id,omitempty"`
}

type AssignToPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	UserID    string `json:"user_id"`
}

type AssignToResultPayload struct {
	EntryID        string `json:"entry_id"`
	EntryType      string `json:"entry_type"`
	AssignedUserID string `json:"assigned_user_id"`
	Ok             bool   `json:"ok"`
}

type SearchResultsPayload struct {
	Query      string                     `json:"query,omitempty"`
	Filters    conversation.SearchFilters `json:"filters,omitempty"`
	Entries    []conversation.InboxEntry  `json:"entries"`
	Page       int                        `json:"page"`
	PageSize   int                        `json:"page_size"`
	TotalItems int64                      `json:"total_items"`
	TotalPages int                        `json:"total_pages"`
}

type WindowReopenedPayload struct {
	EntryID         string     `json:"entry_id"`
	EntryType       string     `json:"entry_type"`
	RequestID       string     `json:"request_id,omitempty"`
	TemplateID      string     `json:"template_id"`
	TemplateName    string     `json:"template_name"`
	MessageID       string     `json:"message_id"`
	WindowExpiresAt *time.Time `json:"window_expires_at,omitempty"`
}

type SendMessagePayload struct {
	EntryID          string  `json:"entry_id"`
	Signed           bool    `json:"signed"`
	EntryType        string  `json:"entry_type"`
	Text             string  `json:"text"`
	MediaID          *string `json:"media_id,omitempty"`
	MediaType        *string `json:"media_type,omitempty"`
	RequestID        string  `json:"request_id"`
	ReplyToMessageID *string `json:"reply_to_message_id,omitempty"`
}

type SendButtonPayload struct {
	EntryID          string                       `json:"entry_id"`
	EntryType        string                       `json:"entry_type"`
	HeaderType       string                       `json:"header_type,omitempty"`
	HeaderText       string                       `json:"header_text,omitempty"`
	BodyText         string                       `json:"body_text"`
	FooterText       string                       `json:"footer_text,omitempty"`
	Buttons          []conversation.ButtonPayload `json:"buttons"`
	RequestID        string                       `json:"request_id"`
	ReplyToMessageID *string                      `json:"reply_to_message_id,omitempty"`
}

type MarkReadPayload struct {
	EntryID    string   `json:"entry_id"`
	EntryType  string   `json:"entry_type"`
	MessageIDs []string `json:"message_ids"`
}

type RequestConnectedPayload struct {
}

type TypingPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	UserID    string `json:"user_id,omitempty"`
	IsTyping  bool   `json:"is_typing"`
}

type HistoryPayload struct {
	EntryID   string                  `json:"entry_id"`
	EntryType string                  `json:"entry_type"`
	Messages  []*conversation.Message `json:"messages"`
	HasMore   bool                    `json:"has_more"`
	Total     int64                   `json:"total"`
	PageSize  int                     `json:"page_size"`
}

type MessagePayload struct {
	EntryID   string                `json:"entry_id"`
	EntryType string                `json:"entry_type"`
	Message   *conversation.Message `json:"message"`
}

type MessageSentPayload struct {
	RequestID string                `json:"request_id"`
	EntryID   string                `json:"entry_id"`
	EntryType string                `json:"entry_type"`
	Message   *conversation.Message `json:"message"`
}

type MessageErrorPayload struct {
	RequestID string `json:"request_id"`
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
}

type ReadPayload struct {
	EntryID    string    `json:"entry_id"`
	EntryType  string    `json:"entry_type"`
	MessageIDs []string  `json:"message_ids"`
	ReadBy     string    `json:"read_by"`
	ReadAt     time.Time `json:"read_at"`
}

type MessageStatusPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type AnalysisUpdatePayload struct {
	EntryID   string      `json:"entry_id"`
	EntryType string      `json:"entry_type"`
	Analysis  interface{} `json:"analysis"`
}

type UnreadCountPayload struct {
	EntryID     string `json:"entry_id"`
	EntryType   string `json:"entry_type"`
	UnreadCount int64  `json:"unread_count"`
}

type SubscribedPayload struct {
	EntryID           string                 `json:"entry_id"`
	EntryType         string                 `json:"entry_type"`
	LeadName          string                 `json:"lead_name,omitempty"`
	LeadNumber        string                 `json:"lead_number,omitempty"`
	LeadPicture       string                 `json:"lead_picture,omitempty"`
	LeadMetadata      map[string]interface{} `json:"lead_metadata,omitempty"`
	EntryVariables    []string               `json:"entry_variables,omitempty"`
	UnreadCount       int64                  `json:"unread_count"`
	AutomationEnabled bool                   `json:"automation_enabled"`
	WindowOpen        bool                   `json:"window_open"`
	WindowExpiresAt   *time.Time             `json:"window_expires_at,omitempty"`
}

type ErrorPayload struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	EntryID        string `json:"entry_id,omitempty"`
	EntryType      string `json:"entry_type,omitempty"`
	Status         string `json:"status,omitempty"`
	PreviousStatus string `json:"previous_status,omitempty"`
}

type InboxPayload struct {
	Entries                  []conversation.InboxEntry      `json:"entries"`
	Page                     int                            `json:"page"`
	PageSize                 int                            `json:"page_size"`
	TotalItems               int64                          `json:"total_items"`
	TotalPages               int                            `json:"total_pages"`
	StageCounts              map[string]int64               `json:"stage_counts,omitempty"`
	ConversationStatusCounts map[string]int64               `json:"conversation_status_counts,omitempty"`
	AvailableLabels          []conversation.InboxEntryLabel `json:"available_labels,omitempty"`
}

type EntryUpdatePayload struct {
	Entry conversation.InboxEntry `json:"entry"`
}

type EntryRemovedPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Reason    string `json:"reason,omitempty"`
}

type StageUpdatePayload struct {
	EntryID   string                        `json:"entry_id"`
	EntryType string                        `json:"entry_type"`
	Stage     *conversation.InboxEntryStage `json:"stage"`
}

type LabelUpdatePayload struct {
	EntryID   string                         `json:"entry_id"`
	EntryType string                         `json:"entry_type"`
	Labels    []conversation.InboxEntryLabel `json:"labels"`
}

type StartCallPayload struct {
	EntryID     string `json:"entry_id"`
	EntryType   string `json:"entry_type"`
	PhoneNumber string `json:"phone_number,omitempty"`
	SIPTrunkID  string `json:"sip_trunk_id,omitempty"`

	WhatsAppPhoneID string `json:"whatsapp_phone_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
}

type EndCallPayload struct {
	RequestID string `json:"request_id,omitempty"`
}

type CallAudioPayload struct {
	Audio      string `json:"audio"`
	SampleRate int    `json:"sample_rate"`
}

type InboundCallActionPayload struct {
	OfferID string `json:"offer_id"`
	Reason  string `json:"reason,omitempty"`
}

type TransferInitiatePayload struct {
	CallID       string `json:"call_id"`
	TargetUserID string `json:"target_user_id"`
	Kind         string `json:"kind,omitempty"`
	Note         string `json:"note,omitempty"`
}

type TransferActionPayload struct {
	TransferID string `json:"transfer_id"`
	Reason     string `json:"reason,omitempty"`
}

type TransferErrorPayload struct {
	TransferID string `json:"transfer_id,omitempty"`
	CallID     string `json:"call_id,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type TransferTargetUser struct {
	UserID   string `json:"user_id"`
	Username string `json:"username,omitempty"`
}

type TransferTargetsPayload struct {
	Users []TransferTargetUser `json:"users"`
}

type DialerPresenceUser struct {
	UserID   string `json:"user_id"`
	Username string `json:"username,omitempty"`
	Busy     bool   `json:"busy"`
	OnCall   bool   `json:"on_call,omitempty"`
	Ringing  bool   `json:"ringing,omitempty"`
	// Endpoint kinds the member currently holds. A member can have both (a browser
	// softphone AND a registered branch). The panel shows a device badge from these;
	// offline members are the workspace roster minus this list.
	HasBrowser bool `json:"has_browser"`
	HasBranch  bool `json:"has_branch"`
}

type DialerPresencePayload struct {
	Users []DialerPresenceUser `json:"users"`
}

type CallStatusPayload struct {
	Status      conversation.CallEventType `json:"status"`
	Reason      string                     `json:"reason,omitempty"`
	CallID      string                     `json:"call_id,omitempty"`
	EntryID     string                     `json:"entry_id"`
	EntryType   string                     `json:"entry_type"`
	PhoneNumber string                     `json:"phone_number,omitempty"`
	RequestID   string                     `json:"request_id,omitempty"`
}

type CallAudioOutPayload struct {
	Audio      string `json:"audio"`
	SampleRate int    `json:"sample_rate"`
}

type CallEndedPayload struct {
	CallID          string  `json:"call_id"`
	EntryID         string  `json:"entry_id"`
	EntryType       string  `json:"entry_type"`
	PhoneNumber     string  `json:"phone_number,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	RequestID       string  `json:"request_id,omitempty"`
}

type WaitingCallSlotPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Reason    string `json:"reason,omitempty"`
}

type CallTrunkBusyPayload struct {
	TrunkID      string `json:"trunk_id"`
	RetryAfterMs int    `json:"retry_after_ms"`
	Reason       string `json:"reason"`
	EntryID      string `json:"entry_id,omitempty"`
	EntryType    string `json:"entry_type,omitempty"`
	Phone        string `json:"phone_number,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
}

type SetConversationStatusPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	Status    string `json:"status"`
}

type ConversationStatusUpdatePayload struct {
	EntryID     string  `json:"entry_id"`
	EntryType   string  `json:"entry_type"`
	Status      string  `json:"status"`
	CloseSource string  `json:"close_source,omitempty"`
	CloseReason string  `json:"close_reason,omitempty"`
	ClosedAt    *string `json:"closed_at,omitempty"`
}

type ConversationStatusCountsUpdatePayload struct {
	Counts map[string]int64 `json:"counts"`
}

type RequestFunnelColumnPayload struct {
	StageID  string `json:"stage_id"`
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
}

type RequestFunnelSummaryPayload struct {
	StageIDs []string `json:"stage_ids"`
}

type FunnelColumnPayload struct {
	StageID    string                    `json:"stage_id"`
	Entries    []conversation.InboxEntry `json:"entries"`
	Page       int                       `json:"page"`
	PageSize   int                       `json:"page_size"`
	TotalItems int64                     `json:"total_items"`
	TotalPages int                       `json:"total_pages"`
}

type FunnelColumnSummary struct {
	StageID    string `json:"stage_id"`
	TotalItems int64  `json:"total_items"`
}

type FunnelSummaryPayload struct {
	Columns []FunnelColumnSummary `json:"columns"`
}

type SwitchViewPayload struct {
	CampaignID           string `json:"campaign_id,omitempty"`
	CampaignType         string `json:"campaign_type,omitempty"`
	WhatsAppCampaignType string `json:"whatsapp_campaign_type,omitempty"`
	ConversationStatus   string `json:"conversation_status,omitempty"`
}

type ViewSwitchedPayload struct {
	CampaignID           string `json:"campaign_id,omitempty"`
	CampaignType         string `json:"campaign_type,omitempty"`
	WhatsAppCampaignType string `json:"whatsapp_campaign_type,omitempty"`
	ViewMode             string `json:"view_mode"`
	ConversationStatus   string `json:"conversation_status,omitempty"`
}
