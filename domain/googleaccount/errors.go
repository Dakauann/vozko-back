package googleaccount

import (
	"errors"
	"fmt"
)

var (
	ErrNotConnected       = errors.New("googleaccount: no google account is connected for this product")
	ErrNeedsReconnect     = fmt.Errorf("%w: google refused the stored access, connect the account again", ErrNotConnected)
	ErrUnknownProduct     = errors.New("googleaccount: unknown product")
	ErrScopeNotGranted    = errors.New("googleaccount: the permission this product needs was not granted on the google consent screen")
	ErrIncompleteGrant    = errors.New("googleaccount: google returned an incomplete grant")
	ErrNoRefreshToken     = errors.New("googleaccount: google returned no refresh token")
	ErrEmailMissing       = errors.New("googleaccount: google returned no email for the account")
	ErrAccountNotFound    = errors.New("googleaccount: account not found")
	ErrRefreshInProgress  = errors.New("googleaccount: another replica is refreshing this account")
	ErrWorkspaceRequired  = errors.New("googleaccount: workspace is required")
	ErrAuthorizationCode  = errors.New("googleaccount: google returned no authorization code")
	ErrAuthorizationError = errors.New("googleaccount: the google authorization was declined")
)

const (
	CodeNotConnected      = "google_account_not_connected"
	CodeNeedsReconnect    = "google_account_needs_reconnect"
	CodeUnknownProduct    = "google_account_unknown_product"
	CodeScopeNotGranted   = "google_account_scope_not_granted"
	CodeConnectFailed     = "google_account_connect_failed"
	CodeAuthorizationDeny = "google_account_authorization_declined"
)

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNeedsReconnect):
		return CodeNeedsReconnect
	case errors.Is(err, ErrNotConnected):
		return CodeNotConnected
	case errors.Is(err, ErrUnknownProduct):
		return CodeUnknownProduct
	case errors.Is(err, ErrScopeNotGranted):
		return CodeScopeNotGranted
	case errors.Is(err, ErrAuthorizationError):
		return CodeAuthorizationDeny
	}
	return CodeConnectFailed
}
