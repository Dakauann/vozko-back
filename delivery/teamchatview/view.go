package teamchatview

import (
	"time"

	"vozko/domain/teamchat"
)

type Person struct {
	UserID  string `json:"user_id" example:"0e9d8c7b-6a5f-4e3d-2c1b-0a9f8e7d6c5b"`
	Name    string `json:"name" example:"Carla"`
	Picture string `json:"picture" example:"https://cdn.exemplo.com/carla.png"`
}

type Participant struct {
	Person
	Role        string `json:"role" example:"member" enums:"admin,member"`
	LastReadSeq int64  `json:"last_read_seq" example:"41"`
}

type Me struct {
	Role        string `json:"role" example:"admin" enums:"admin,member"`
	LastReadSeq int64  `json:"last_read_seq" example:"40"`
	Muted       bool   `json:"muted"`
}

type Message struct {
	ID         string     `json:"id" example:"5b8f7a0e-2d7c-4f6e-9a51-0c3b7e2f9d10"`
	ThreadID   string     `json:"thread_id" example:"8a3c2f4e-1b5d-4c6a-9e7f-2d1b3c4a5e6f"`
	Seq        int64      `json:"seq" example:"42"`
	AuthorID   string     `json:"author_id" example:"0e9d8c7b-6a5f-4e3d-2c1b-0a9f8e7d6c5b"`
	AuthorName string     `json:"author_name" example:"Carla"`
	Kind       string     `json:"kind" example:"text" enums:"text,system"`
	Body       string     `json:"body" example:"Pode assumir o cliente da loja 3?"`
	Event      string     `json:"event,omitempty" example:"members_added" enums:"created,renamed,members_added,member_removed,member_left"`
	SubjectIDs []string   `json:"subject_ids"`
	ReplyToID  string     `json:"reply_to_id,omitempty"`
	Mentions   []string   `json:"mentions"`
	ClientID   string     `json:"client_id,omitempty" example:"3f1c9a52-1e0b-4d6f-8f0e-2a7c4b9d1e33"`
	CreatedAt  time.Time  `json:"created_at" example:"2026-10-19T12:00:00Z"`
	EditedAt   *time.Time `json:"edited_at,omitempty"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

type Thread struct {
	ID             string        `json:"id" example:"8a3c2f4e-1b5d-4c6a-9e7f-2d1b3c4a5e6f"`
	Kind           string        `json:"kind" example:"group" enums:"direct,group"`
	Title          string        `json:"title" example:"Vendas"`
	CreatedBy      string        `json:"created_by"`
	LastSeq        int64         `json:"last_seq" example:"42"`
	LastMessageAt  time.Time     `json:"last_message_at" example:"2026-10-19T12:00:00Z"`
	CreatedAt      time.Time     `json:"created_at" example:"2026-10-01T09:00:00Z"`
	Me             Me            `json:"me"`
	Members        []Participant `json:"members"`
	LastMessage    *Message      `json:"last_message,omitempty"`
	Unread         int64         `json:"unread" example:"2"`
	UnreadMentions int64         `json:"unread_mentions" example:"1"`
}

type Scheduled struct {
	ID            string    `json:"id" example:"c4a1d2e3-5f6a-4b7c-8d9e-0f1a2b3c4d5e"`
	ThreadID      string    `json:"thread_id"`
	Body          string    `json:"body" example:"Lembrete: reunião às 15h"`
	Mentions      []string  `json:"mentions"`
	ReplyToID     string    `json:"reply_to_id,omitempty"`
	SendAt        time.Time `json:"send_at" example:"2026-10-20T15:00:00Z"`
	Status        string    `json:"status" example:"pending" enums:"pending,sent,canceled,failed"`
	FailureReason string    `json:"failure_reason,omitempty" example:"recipient_unavailable" enums:"not_member,recipient_unavailable,left_workspace,thread_unavailable"`
	SentMessageID string    `json:"sent_message_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ReadMark struct {
	ThreadID string `json:"thread_id"`
	UserID   string `json:"user_id"`
	Seq      int64  `json:"seq" example:"42"`
}

func PersonOf(p teamchat.Person) Person {
	return Person{UserID: p.UserID, Name: p.Name, Picture: p.Picture}
}

func PeopleOf(people []teamchat.Person) []Person {
	out := make([]Person, 0, len(people))
	for _, p := range people {
		out = append(out, PersonOf(p))
	}
	return out
}

func MessageOf(m teamchat.AuthoredMessage) Message {
	return Message{
		ID:         m.ID,
		ThreadID:   m.ThreadID,
		Seq:        m.Seq,
		AuthorID:   m.AuthorID,
		AuthorName: m.AuthorName,
		Kind:       string(m.Kind),
		Body:       m.Body,
		Event:      string(m.Event),
		SubjectIDs: nonNil(m.SubjectIDs),
		ReplyToID:  m.ReplyToID,
		Mentions:   nonNil(m.Mentions),
		ClientID:   m.ClientID,
		CreatedAt:  m.CreatedAt,
		EditedAt:   m.EditedAt,
		DeletedAt:  m.DeletedAt,
	}
}

func MessagesOf(messages []teamchat.AuthoredMessage) []Message {
	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		out = append(out, MessageOf(m))
	}
	return out
}

func ThreadOf(v teamchat.ThreadView) Thread {
	members := make([]Participant, 0, len(v.Members))
	for _, p := range v.Members {
		members = append(members, Participant{Person: PersonOf(p.Person), Role: string(p.Role), LastReadSeq: p.LastReadSeq})
	}
	thread := Thread{
		ID:             v.Thread.ID,
		Kind:           string(v.Thread.Kind),
		Title:          v.Thread.Title,
		CreatedBy:      v.Thread.CreatedBy,
		LastSeq:        v.Thread.LastSeq,
		LastMessageAt:  v.Thread.LastMessageAt,
		CreatedAt:      v.Thread.CreatedAt,
		Me:             Me{Role: string(v.Me.Role), LastReadSeq: v.Me.LastReadSeq, Muted: v.Me.Muted},
		Members:        members,
		Unread:         v.Unread,
		UnreadMentions: v.UnreadMentions,
	}
	if v.LastMessage != nil {
		last := MessageOf(*v.LastMessage)
		thread.LastMessage = &last
	}
	return thread
}

func ThreadsOf(views []teamchat.ThreadView) []Thread {
	out := make([]Thread, 0, len(views))
	for _, v := range views {
		out = append(out, ThreadOf(v))
	}
	return out
}

func ScheduledOf(s teamchat.Scheduled) Scheduled {
	return Scheduled{
		ID:            s.ID,
		ThreadID:      s.ThreadID,
		Body:          s.Body,
		Mentions:      nonNil(s.Mentions),
		ReplyToID:     s.ReplyToID,
		SendAt:        s.SendAt,
		Status:        string(s.Status),
		FailureReason: string(s.FailureReason),
		SentMessageID: s.SentMessageID,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func ScheduledListOf(list []teamchat.Scheduled) []Scheduled {
	out := make([]Scheduled, 0, len(list))
	for _, s := range list {
		out = append(out, ScheduledOf(s))
	}
	return out
}

func ReadMarkOf(mark teamchat.ReadMark) ReadMark {
	return ReadMark{ThreadID: mark.ThreadID, UserID: mark.UserID, Seq: mark.Seq}
}

func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
