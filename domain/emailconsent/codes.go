package emailconsent

import "errors"

var (
	ErrUnsubscribeTokenInvalid = errors.New("emailconsent: invalid unsubscribe token")
	ErrImportEmpty             = errors.New("emailconsent: the import has no rows")
	ErrImportTooLarge          = errors.New("emailconsent: the import has too many rows")
)

const MaxImportRows = 10000

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "workspace_required"},
	{ErrAddressInvalid, "address_invalid"},
	{ErrSourceInvalid, "source_invalid"},
	{ErrConsentIPRequired, "consent_ip_required"},
	{ErrConsentIPInvalid, "consent_ip_invalid"},
	{ErrConsentDateRequired, "consent_date_required"},
	{ErrConsentDateInFuture, "consent_date_in_future"},
	{ErrUnsubscribeSourceInvalid, "unsubscribe_source_invalid"},
	{ErrAddressMismatch, "address_mismatch"},
	{ErrSubscriptionNotFound, "subscription_not_found"},
	{ErrUnsubscribeTokenInvalid, "unsubscribe_token_invalid"},
	{ErrImportEmpty, "import_empty"},
	{ErrImportTooLarge, "import_too_large"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

func KnownErrors() []error {
	out := make([]error, len(errorCodes))
	for i, known := range errorCodes {
		out[i] = known.err
	}
	return out
}
