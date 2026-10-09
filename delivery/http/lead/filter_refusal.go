package lead

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadarea"
)

func filterRefusal(err error) (int, string, bool) {
	switch {
	case errors.Is(err, customfield.ErrFilterSensitive):
		return http.StatusForbidden, customfield.ErrorCode(err), true
	case errors.Is(err, leaddomain.ErrLeadFilterAddressForbidden), errors.Is(err, leaddomain.ErrAddressesForbidden):
		return http.StatusForbidden, leaddomain.ErrorCode(err), true
	case errors.Is(err, leadarea.ErrNotFound), leadarea.IsInputRefusal(err):
		return http.StatusBadRequest, leadarea.ErrorCode(err), true
	case errors.Is(err, leaddomain.ErrLeadSearchTooShort):
		return http.StatusBadRequest, leaddomain.ErrorCode(leaddomain.ErrLeadSearchTooShort), true
	case errors.Is(err, leaddomain.ErrLeadFilterInvalid), errors.Is(err, crmfilter.ErrNotApplicable):
		if code := customfield.ErrorCode(err); code != "" {
			return http.StatusBadRequest, code, true
		}
		return http.StatusBadRequest, leaddomain.ErrorCode(leaddomain.ErrLeadFilterInvalid), true
	}
	return 0, "", false
}

func writeRefusal(w http.ResponseWriter, err error, classify func(error) (int, string, bool)) bool {
	status, code, refused := classify(err)
	if refused {
		response.WriteErrorWithCode(w, status, code, err.Error(), nil)
	}
	return refused
}

func writeFilterRefusal(w http.ResponseWriter, err error) bool {
	return writeRefusal(w, err, filterRefusal)
}
