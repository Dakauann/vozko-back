package conversation

import (
	"context"
	"errors"

	"vozko/domain/shared"
)

var (
	ErrMessageEditTextInvalid = errors.New("conversation: an edit needs the new text")
	ErrResendDeliveryFailed   = errors.New("conversation: the resent message could not be delivered")
)

type MessageTarget struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType
	MessageID   string
}

type ReactToMessageUseCase interface {
	Execute(ctx context.Context, by shared.Person, target MessageTarget, emoji string) (*Message, error)
}

type EditMessageUseCase interface {
	Execute(ctx context.Context, by shared.Person, target MessageTarget, text string) (*Message, error)
}

type RetractMessageUseCase interface {
	Execute(ctx context.Context, by shared.Person, target MessageTarget) (*Message, error)
}

type ResendMessageUseCase interface {
	Execute(ctx context.Context, by shared.Person, target MessageTarget, requestID string) (*Message, error)
}

type MessageCapabilitiesReader interface {
	MessageCapabilities(entryType shared.EntryType) MessageCapabilities
}

type MessageActionUseCases struct {
	React        ReactToMessageUseCase
	Edit         EditMessageUseCase
	Delete       RetractMessageUseCase
	Resend       ResendMessageUseCase
	Capabilities MessageCapabilitiesReader
}
