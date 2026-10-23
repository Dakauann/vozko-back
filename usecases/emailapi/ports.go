package emailapi_usecase

import (
	"context"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	balance_usecase "vozko/usecases/balance"
)

type Billing interface {
	QuoteSend(ctx context.Context, workspaceID string, recipients, encodedAttachmentBytes int64) (balance_usecase.EmailQuote, error)
	ChargeSend(ctx context.Context, workspaceID, reference string, q balance_usecase.EmailQuote) (bool, error)
	RefundSend(ctx context.Context, workspaceID, reference string, q balance_usecase.EmailQuote, units int64) error
	SendCharged(ctx context.Context, reference string) (bool, error)
}

type Senders interface {
	Get(ctx context.Context, workspaceID, id string) (*es.Sender, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]es.Sender, error)
}

type Domains interface {
	ListByWorkspace(ctx context.Context, workspaceID string) ([]es.SendingDomain, error)
}

type Suppressions interface {
	Suppressed(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]es.SuppressReason, error)
}

type quote = balance_usecase.EmailQuote
