package conversation

import "context"

type OperatorSendInput struct {
	EntryID   string
	EntryType string

	WorkspaceID string

	SenderUserID string

	Text             string
	MediaID          string
	MediaType        string
	ReplyToMessageID string

	Signed bool

	Buttons *SendButtonInput
}

type OperatorSendUseCase interface {
	Execute(ctx context.Context, in OperatorSendInput) (*Message, error)
}

type FinalizeOperatorSendInput struct {
	EntryID   string
	EntryType string

	WorkspaceID string

	ActorUserID string

	Message *Message
}

type OperatorSendFinalizer interface {
	FinalizeOperatorSend(ctx context.Context, in FinalizeOperatorSendInput) error
}
