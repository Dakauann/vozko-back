package advertising

import "errors"

const (
	CodeSensitiveAudience    = "audience_sensitive_filter"
	CodeNoCustomersMatched   = "no_customers_matched"
	CodeAudienceTerms        = "audience_terms_not_accepted"
	CodeReconnectRequired    = "reconnect_required"
	CodeAdAccountUnavailable = "not_found"
)

var audienceCodes = []struct {
	err  error
	code string
}{
	{ErrSensitiveAudience, CodeSensitiveAudience},
	{ErrNoCustomersMatched, CodeNoCustomersMatched},
	{ErrAudienceTermsNotAccepted, CodeAudienceTerms},
	{ErrGrantNotFound, CodeReconnectRequired},
	{ErrAccountNeedsReconnect, CodeReconnectRequired},
	{ErrAccountNotFound, CodeAdAccountUnavailable},
}

func AudienceErrorCode(err error) string {
	for _, known := range audienceCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
