package googleaccount

import (
	"context"
	"time"
)

type RefreshFunc func(ctx context.Context, account *Account) (RefreshOutcome, error)

type Repository interface {
	SaveConnected(ctx context.Context, account *Account) error
	FindByID(ctx context.Context, id string) (*Account, error)
	FindByEmail(ctx context.Context, workspaceID, email string) (*Account, error)
	FindBound(ctx context.Context, workspaceID string, product Product) (*Account, error)
	ListBound(ctx context.Context, workspaceID string) ([]BoundAccount, error)
	Bind(ctx context.Context, binding Binding) error
	Unbind(ctx context.Context, workspaceID string, product Product) (accountID string, err error)
	CountBindings(ctx context.Context, accountID string) (int, error)
	Disconnect(ctx context.Context, id string) error
	ConnectedElsewhere(ctx context.Context, email, exceptID string) (bool, error)
	ListDueForRefresh(ctx context.Context, expiresBefore time.Time, limit int) ([]*Account, error)
	LockForRefresh(ctx context.Context, id string, refresh RefreshFunc) (acquired bool, err error)
}

type OAuthClient interface {
	AuthorizeURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (Grant, error)
	Refresh(ctx context.Context, refreshToken string) (Grant, error)
	Profile(ctx context.Context, accessToken string) (Profile, error)
	Revoke(ctx context.Context, token string) error
}

type ErrorClassifier interface {
	IsAuth(err error) bool
}

type AccessSource interface {
	Access(ctx context.Context, workspaceID string, product Product) (Access, error)
}
