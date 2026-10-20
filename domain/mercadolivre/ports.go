package mercadolivre

import "context"

type ErrorClassifier interface {
	IsAuth(err error) bool
	IsPermission(err error) bool
	IsRateLimit(err error) bool
	Blocked(err error) (substatus string, blocked bool)
	IsModerationPending(err error) bool
	IsAppBlocked(err error) bool
	IsOperatorUser(err error) bool
	IsNotFound(err error) bool
	IsAmbiguous(err error) bool
	IsRetryable(err error) bool
	IsDecode(err error) bool
	IsDefiniteRefusal(err error) bool
}

type PKCE interface {
	NewVerifier() (string, error)
	Challenge(verifier string) (string, error)
}

type MediaFetcher interface {
	Fetch(ctx context.Context, url string, maxBytes int64) (*AttachmentDownload, error)
}
