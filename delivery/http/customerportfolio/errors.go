package customerportfoliohttp

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	cp "vozko/domain/customerportfolio"
)

func writeError(w http.ResponseWriter, err error) {
	coded := func(status int, expected map[string]string) {
		response.WriteErrorWithCode(w, status, cp.ErrorCode(err), err.Error(), expected)
	}
	var held *cp.HeldByOther
	switch {
	case errors.As(err, &held):
		coded(http.StatusConflict, map[string]string{"entryId": held.Entry.ID, "holderId": held.Entry.HolderID, "holderName": held.HolderName})
	case errors.Is(err, cp.ErrForbidden), errors.Is(err, cp.ErrHolderOutOfReach):
		coded(http.StatusForbidden, nil)
	case errors.Is(err, cp.ErrEntryNotFound), errors.Is(err, cp.ErrSubjectNotFound):
		coded(http.StatusNotFound, nil)
	case errors.Is(err, cp.ErrHolderIneligible):
		coded(http.StatusConflict, nil)
	case cp.IsInputRefusal(err):
		coded(http.StatusBadRequest, nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Falha ao atualizar a carteira", nil)
	}
}
