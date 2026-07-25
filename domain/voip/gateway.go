package voip

import "context"

type SIPGateway interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Invite(ctx context.Context, input InviteCallInput) (CallSession, error)
	Hangup(ctx context.Context, input HangupInput) error
}

type RTPGateway interface {
	ReservePort() (int, error)
	ReleasePort(port int) error
	StartStream(ctx context.Context, cfg StreamConfig) (StreamHandle, error)
	StopStream(ctx context.Context, handle StreamHandle) error
}
