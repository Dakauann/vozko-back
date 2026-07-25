package voip

import "context"

type InitiateCallUseCase interface {
	Execute(ctx context.Context, input InviteCallInput) (CallSession, error)
}

type EndCallUseCase interface {
	Execute(ctx context.Context, input HangupInput) error
}
