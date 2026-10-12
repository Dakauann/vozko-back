package aichat

import (
	"errors"
	"time"
)

var ErrThreadNotFound = errors.New("aichat: thread not found")

type ListThreadsInput struct {
	WorkspaceID string
	UserID      string
	Query       string
	Limit       int
	Offset      int
}

type ThreadProjects interface {
	BindProject(threadID, projectID string) (bool, error)
}

type ProjectAdopter interface {
	AdoptProject(threadID, projectID string) error
}

type ThreadRepository interface {
	Create(thread *Thread) error
	GetByID(id string) (*Thread, error)
	ListByUser(input ListThreadsInput) ([]*Thread, int64, error)
	Rename(id, title string) error
	Touch(id string, lastMessageAt time.Time, model, reasoningLevel string) error
	Delete(id string) error
}

type ListMessagesInput struct {
	ThreadID string
	Limit    int
	Offset   int
}

type MessageRepository interface {
	Create(message *Message) error
	ListByThread(input ListMessagesInput) ([]*Message, int64, error)
	DeleteByThread(threadID string) error
	ClaimProposal(threadID, proposalID string, outcome ProposalStatus) (*Message, error)
	ExpireProposals(threadID string) error
	HasPendingProposal(threadID string) (bool, error)
}
