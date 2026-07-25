package coexistence

import "time"

type PhoneCoexistenceStatus struct {
	IsOnBizApp   bool   `json:"is_on_biz_app"`
	PlatformType string `json:"platform_type"`
	PhoneID      string `json:"id"`
}

func (s PhoneCoexistenceStatus) IsCoexistence() bool {
	return s.IsOnBizApp && s.PlatformType == "CLOUD_API"
}

type SyncType string

const (
	SyncTypeContacts SyncType = "smb_app_state_sync"
	SyncTypeHistory  SyncType = "history"
)

type SyncRequest struct {
	MessagingProduct string   `json:"messaging_product"`
	SyncType         SyncType `json:"sync_type"`
}

type SyncResponse struct {
	MessagingProduct string `json:"messaging_product"`
	RequestID        string `json:"request_id"`
}

const EventFinishCoexistence = "FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING"

type HistoryWebhookPayload struct {
	Object string                `json:"object"`
	Entry  []HistoryWebhookEntry `json:"entry"`
}

type HistoryWebhookEntry struct {
	ID      string                 `json:"id"`
	Changes []HistoryWebhookChange `json:"changes"`
}

type HistoryWebhookChange struct {
	Value HistoryWebhookValue `json:"value"`
	Field string              `json:"field"`
}

type HistoryWebhookValue struct {
	MessagingProduct string              `json:"messaging_product"`
	Metadata         HistoryMetadata     `json:"metadata"`
	History          []HistoryChunk      `json:"history,omitempty"`
	Messages         []HistoryMediaAsset `json:"messages,omitempty"`
}

type HistoryMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type HistoryChunk struct {
	Metadata HistoryChunkMetadata `json:"metadata"`
	Threads  []HistoryThread      `json:"threads,omitempty"`
	Errors   []HistoryError       `json:"errors,omitempty"`
}

type HistoryChunkMetadata struct {
	Phase      int `json:"phase"`
	ChunkOrder int `json:"chunk_order"`
	Progress   int `json:"progress"`
}

type HistoryThread struct {
	ID       string           `json:"id"`
	Messages []HistoryMessage `json:"messages"`
}

type HistoryMessage struct {
	From      string `json:"from"`
	To        string `json:"to,omitempty"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Status    string `json:"status"`

	Text     *HistoryTextContent  `json:"text,omitempty"`
	Image    *HistoryMediaContent `json:"image,omitempty"`
	Video    *HistoryMediaContent `json:"video,omitempty"`
	Audio    *HistoryMediaContent `json:"audio,omitempty"`
	Document *HistoryMediaContent `json:"document,omitempty"`
	Sticker  *HistoryMediaContent `json:"sticker,omitempty"`
	Location *HistoryLocation     `json:"location,omitempty"`
	Contacts []HistoryContact     `json:"contacts,omitempty"`
}

type HistoryTextContent struct {
	Body string `json:"body"`
}

type HistoryMediaContent struct {
	ID       string `json:"id,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Caption  string `json:"caption,omitempty"`
	Link     string `json:"link,omitempty"`
}

type HistoryLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

type HistoryContact struct {
	Name   HistoryContactName    `json:"name"`
	Phones []HistoryContactPhone `json:"phones,omitempty"`
}

type HistoryContactName struct {
	FormattedName string `json:"formatted_name"`
	FirstName     string `json:"first_name,omitempty"`
}

type HistoryContactPhone struct {
	Phone string `json:"phone"`
	Type  string `json:"type,omitempty"`
}

type HistoryMediaAsset struct {
	From      string               `json:"from"`
	ID        string               `json:"id"`
	Timestamp string               `json:"timestamp"`
	Type      string               `json:"type"`
	Image     *HistoryMediaContent `json:"image,omitempty"`
	Video     *HistoryMediaContent `json:"video,omitempty"`
	Audio     *HistoryMediaContent `json:"audio,omitempty"`
	Document  *HistoryMediaContent `json:"document,omitempty"`
	Sticker   *HistoryMediaContent `json:"sticker,omitempty"`
}

type HistoryError struct {
	Code      int    `json:"code"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	ErrorData struct {
		Details string `json:"details"`
	} `json:"error_data"`
}

const HistorySharingDeclinedCode = 2593109

type SMBStateSyncWebhookPayload struct {
	Object string              `json:"object"`
	Entry  []SMBStateSyncEntry `json:"entry"`
}

type SMBStateSyncEntry struct {
	ID      string               `json:"id"`
	Changes []SMBStateSyncChange `json:"changes"`
}

type SMBStateSyncChange struct {
	Value SMBStateSyncValue `json:"value"`
	Field string            `json:"field"`
}

type SMBStateSyncValue struct {
	MessagingProduct string             `json:"messaging_product"`
	Metadata         HistoryMetadata    `json:"metadata"`
	StateSync        []SMBStateSyncItem `json:"state_sync"`
}

type SMBStateSyncItem struct {
	Type     string          `json:"type"`
	Contact  SMBContact      `json:"contact"`
	Action   string          `json:"action"`
	Metadata SMBSyncMetadata `json:"metadata"`
}

type SMBContact struct {
	FullName    string `json:"full_name"`
	FirstName   string `json:"first_name"`
	PhoneNumber string `json:"phone_number"`
}

type SMBSyncMetadata struct {
	Timestamp string `json:"timestamp"`
}

type SMBMessageEchoesWebhookPayload struct {
	Object string                  `json:"object"`
	Entry  []SMBMessageEchoesEntry `json:"entry"`
}

type SMBMessageEchoesEntry struct {
	ID      string                   `json:"id"`
	Changes []SMBMessageEchoesChange `json:"changes"`
}

type SMBMessageEchoesChange struct {
	Value SMBMessageEchoesValue `json:"value"`
	Field string                `json:"field"`
}

type SMBMessageEchoesValue struct {
	MessagingProduct string           `json:"messaging_product"`
	Metadata         HistoryMetadata  `json:"metadata"`
	MessageEchoes    []SMBMessageEcho `json:"message_echoes"`
}

type SMBMessageEcho struct {
	From      string               `json:"from"`
	To        string               `json:"to"`
	ID        string               `json:"id"`
	Timestamp string               `json:"timestamp"`
	Type      string               `json:"type"`
	Text      *HistoryTextContent  `json:"text,omitempty"`
	Image     *HistoryMediaContent `json:"image,omitempty"`
	Video     *HistoryMediaContent `json:"video,omitempty"`
	Audio     *HistoryMediaContent `json:"audio,omitempty"`
	Document  *HistoryMediaContent `json:"document,omitempty"`
	Sticker   *HistoryMediaContent `json:"sticker,omitempty"`
}

const (
	AccountEventPartnerRemoved     = "PARTNER_REMOVED"
	AccountEventAccountOffboarded  = "ACCOUNT_OFFBOARDED"
	AccountEventAccountReconnected = "ACCOUNT_RECONNECTED"
)

type SyncJobStatus string

const (
	SyncJobStatusPending   SyncJobStatus = "pending"
	SyncJobStatusSyncing   SyncJobStatus = "syncing"
	SyncJobStatusCompleted SyncJobStatus = "completed"
	SyncJobStatusFailed    SyncJobStatus = "failed"
	SyncJobStatusDeclined  SyncJobStatus = "declined"
)

type SyncJob struct {
	ID                string        `json:"id"`
	PhoneNumberID     string        `json:"phoneNumberId"`
	BusinessPhoneID   string        `json:"businessPhoneId"`
	WorkspaceID       string        `json:"workspaceId"`
	CampaignID        string        `json:"campaignId"`
	Status            SyncJobStatus `json:"status"`
	ContactsRequestID string        `json:"contactsRequestId,omitempty"`
	HistoryRequestID  string        `json:"historyRequestId,omitempty"`
	ProgressPercent   int           `json:"progressPercent"`
	ErrorMessage      string        `json:"errorMessage,omitempty"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}
