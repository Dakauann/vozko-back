package teamchathttp

import (
	"time"

	"vozko/delivery/teamchatview"
)

type PeopleResponse struct {
	People []teamchatview.Person `json:"people"`
}

type SummaryResponse struct {
	UnreadThreads  int   `json:"unread_threads" example:"2"`
	UnreadMessages int64 `json:"unread_messages" example:"7"`
}

type ThreadListResponse struct {
	Threads []teamchatview.Thread `json:"threads"`
	Next    string                `json:"next" example:"MjAyNi0xMC0xOVQxMjowMDowMFp8OGEzYw"`
}

type ThreadEnvelope struct {
	Thread teamchatview.Thread `json:"thread"`
}

type MessageListResponse struct {
	Messages []teamchatview.Message `json:"messages"`
	HasMore  bool                   `json:"has_more"`
}

type MessageEnvelope struct {
	Message teamchatview.Message `json:"message"`
}

type ScheduledListResponse struct {
	Scheduled []teamchatview.Scheduled `json:"scheduled"`
}

type ScheduledEnvelope struct {
	Scheduled teamchatview.Scheduled `json:"scheduled"`
}

type ReadResponse struct {
	LastReadSeq int64 `json:"last_read_seq" example:"42"`
}

type OpenDirectRequest struct {
	UserID string `json:"user_id" example:"0e9d8c7b-6a5f-4e3d-2c1b-0a9f8e7d6c5b"`
}

type CreateGroupRequest struct {
	Title     string   `json:"title" example:"Vendas"`
	MemberIDs []string `json:"member_ids"`
}

type RenameRequest struct {
	Title string `json:"title" example:"Vendas Sul"`
}

type AddMembersRequest struct {
	UserIDs []string `json:"user_ids"`
}

type MuteRequest struct {
	Muted bool `json:"muted"`
}

type SendRequest struct {
	Body      string   `json:"body" example:"@Carla pode assumir o cliente da loja 3?"`
	Mentions  []string `json:"mentions,omitempty"`
	ReplyToID string   `json:"reply_to_id,omitempty"`
	ClientID  string   `json:"client_id" example:"3f1c9a52-1e0b-4d6f-8f0e-2a7c4b9d1e33"`
}

type EditRequest struct {
	Body string `json:"body" example:"Pode assumir o cliente da loja 4?"`
}

type ReadRequest struct {
	Seq int64 `json:"seq" example:"42"`
}

type ScheduleRequest struct {
	Body      string     `json:"body" example:"Lembrete: reunião às 15h"`
	Mentions  []string   `json:"mentions,omitempty"`
	ReplyToID string     `json:"reply_to_id,omitempty"`
	SendAt    *time.Time `json:"send_at" example:"2026-10-20T15:00:00Z"`
}

type RescheduleRequest struct {
	Body   *string    `json:"body,omitempty"`
	SendAt *time.Time `json:"send_at,omitempty"`
}
