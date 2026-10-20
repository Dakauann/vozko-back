package mercadolivre

import mldomain "vozko/domain/mercadolivre"

type Classifier struct{}

var _ mldomain.ErrorClassifier = Classifier{}

func (Classifier) IsAuth(err error) bool { return IsAuth(err) }

func (Classifier) IsPermission(err error) bool { return IsPermission(err) }

func (Classifier) IsRateLimit(err error) bool { return IsRateLimit(err) }

func (Classifier) Blocked(err error) (string, bool) { return IsBlocked(err) }

func (Classifier) IsModerationPending(err error) bool { return IsModerationPending(err) }

func (Classifier) IsAppBlocked(err error) bool { return IsAppBlocked(err) }

func (Classifier) IsOperatorUser(err error) bool { return IsOperatorUser(err) }

func (Classifier) IsNotFound(err error) bool { return IsNotFound(err) }

func (Classifier) IsAmbiguous(err error) bool { return IsAmbiguous(err) }

func (Classifier) IsRetryable(err error) bool { return IsRetryable(err) }

func (Classifier) IsDecode(err error) bool {
	e, ok := asError(err)
	return ok && (e.Code == CodeDecode || e.Code == CodeResponseTooLarge)
}

func (Classifier) IsDefiniteRefusal(err error) bool {
	e, ok := asError(err)
	if !ok || e.Ambiguous {
		return false
	}
	switch e.Code {
	case CodeLimiterUnavailable, CodeLocalRateLimited:
		return true
	}
	return e.HTTPStatus >= 400 && e.HTTPStatus < 500
}

type PKCE struct{}

var _ mldomain.PKCE = PKCE{}

func (PKCE) NewVerifier() (string, error) { return NewPKCEVerifier() }

func (PKCE) Challenge(verifier string) (string, error) { return PKCEChallenge(verifier) }
