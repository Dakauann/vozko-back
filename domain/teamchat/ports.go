package teamchat

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type ThreadRow struct {
	Thread Thread
	Me     Member
}

type Totals struct {
	UnreadThreads  int
	UnreadMessages int64
}

type ThreadStore interface {
	CreateThread(ctx context.Context, t Thread, members []Member, notice *Message) (Thread, bool, error)
	FindThread(ctx context.Context, workspaceID, id string) (Thread, error)
	FindDirect(ctx context.Context, workspaceID, directKey string) (Thread, error)
	Roster(ctx context.Context, threadID string) (Roster, error)
	Rosters(ctx context.Context, threadIDs []string) (map[string]Roster, error)
	ListThreads(ctx context.Context, q ThreadQuery) ([]ThreadRow, error)
	ChangeMembers(ctx context.Context, t Thread, members []Member, notice Message) (Message, error)
	Rename(ctx context.Context, t Thread, notice Message) (Message, error)
	MarkRead(ctx context.Context, threadID, userID string, seq int64) (int64, error)
	SetMuted(ctx context.Context, threadID, userID string, muted bool) error
	UnreadTotals(ctx context.Context, workspaceID, userID string) (Totals, error)
	UnreadMentions(ctx context.Context, userID string, threadIDs []string) (map[string]int64, error)
}

type MessageStore interface {
	Append(ctx context.Context, m Message) (Message, bool, error)
	ListMessages(ctx context.Context, q MessageQuery) ([]Message, error)
	LastMessages(ctx context.Context, threads []Thread) (map[string]Message, error)
	FindMessage(ctx context.Context, workspaceID, id string) (Message, error)
	SaveMessage(ctx context.Context, m Message) error
}

type ScheduledStore interface {
	CreateScheduled(ctx context.Context, s Scheduled) error
	FindScheduled(ctx context.Context, id string) (Scheduled, error)
	ListScheduled(ctx context.Context, threadID, authorID string) ([]Scheduled, error)
	SaveScheduled(ctx context.Context, s Scheduled) error
	DueScheduled(ctx context.Context, now time.Time, limit int) ([]Scheduled, error)
}

type Repository interface {
	ThreadStore
	MessageStore
	ScheduledStore
}

type Person struct {
	UserID  string
	Name    string
	Picture string
}

type Participant struct {
	Person
	Role        Role
	LastReadSeq int64
}

type AuthoredMessage struct {
	Message
	AuthorName string
}

type ThreadView struct {
	Thread         Thread
	Me             Member
	Members        []Participant
	LastMessage    *AuthoredMessage
	Unread         int64
	UnreadMentions int64
}

type ThreadPage struct {
	Threads []ThreadView
	Next    string
}

type MessagePage struct {
	Messages []AuthoredMessage
	HasMore  bool
}

type ReadMark struct {
	ThreadID string
	UserID   string
	Seq      int64
}

type Sent struct {
	Message AuthoredMessage
	Created bool
}

type Reschedule struct {
	Body   *string
	SendAt *time.Time
}

type UseCases interface {
	Directory(ctx context.Context, by shared.Person, workspaceID string) ([]Person, error)
	Summary(ctx context.Context, by shared.Person, workspaceID string) (Totals, error)
	Threads(ctx context.Context, by shared.Person, workspaceID, cursor string, limit int) (ThreadPage, error)
	Thread(ctx context.Context, by shared.Person, workspaceID, threadID string) (ThreadView, error)
	Messages(ctx context.Context, by shared.Person, workspaceID string, q MessageQuery) (MessagePage, error)
	OpenDirect(ctx context.Context, by shared.Person, workspaceID, otherUserID string) (ThreadView, error)
	CreateGroup(ctx context.Context, by shared.Person, workspaceID, title string, memberIDs []string) (ThreadView, error)
	Rename(ctx context.Context, by shared.Person, workspaceID, threadID, title string) (ThreadView, error)
	AddMembers(ctx context.Context, by shared.Person, workspaceID, threadID string, userIDs []string) (ThreadView, error)
	RemoveMember(ctx context.Context, by shared.Person, workspaceID, threadID, userID string) error
	Leave(ctx context.Context, by shared.Person, workspaceID, threadID string) error
	Mute(ctx context.Context, by shared.Person, workspaceID, threadID string, muted bool) error
	Send(ctx context.Context, by shared.Person, workspaceID, threadID string, d Draft) (Sent, error)
	Edit(ctx context.Context, by shared.Person, workspaceID, messageID, body string) (AuthoredMessage, error)
	Delete(ctx context.Context, by shared.Person, workspaceID, messageID string) (AuthoredMessage, error)
	MarkRead(ctx context.Context, by shared.Person, workspaceID, threadID string, seq int64) (int64, error)
	Typing(ctx context.Context, by shared.Person, workspaceID, threadID string) error
	Schedule(ctx context.Context, by shared.Person, workspaceID, threadID string, d Draft, sendAt time.Time) (Scheduled, error)
	Scheduled(ctx context.Context, by shared.Person, workspaceID, threadID string) ([]Scheduled, error)
	Reschedule(ctx context.Context, by shared.Person, workspaceID, scheduledID string, change Reschedule) (Scheduled, error)
	CancelScheduled(ctx context.Context, by shared.Person, workspaceID, scheduledID string) error
}

type ScheduledDelivery interface {
	DispatchScheduled(ctx context.Context, scheduledID string) error
	SweepScheduled(ctx context.Context) error
}

type WakeScheduler interface {
	ScheduleFire(id string, fireAt time.Time) error
}

const FireTopic = "team_chat.scheduled_fire"
