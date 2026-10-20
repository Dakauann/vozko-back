package whatsappformhttp

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/whatsappform"
)

var statuses = []struct {
	err    error
	status int
}{
	{whatsappform.ErrForbidden, http.StatusForbidden},
	{whatsappform.ErrNotOwner, http.StatusForbidden},
	{whatsappform.ErrFormNotFound, http.StatusNotFound},
	{whatsappform.ErrVersionNotFound, http.StatusNotFound},
	{whatsappform.ErrPublicationNotFound, http.StatusNotFound},
	{whatsappform.ErrResponseNotFound, http.StatusNotFound},
	{whatsappform.ErrSendNotFound, http.StatusNotFound},
	{whatsappform.ErrStaleDraft, http.StatusConflict},
	{whatsappform.ErrNameTaken, http.StatusConflict},
	{whatsappform.ErrOutsideWindow, http.StatusConflict},
	{whatsappform.ErrNoPublication, http.StatusConflict},
	{whatsappform.ErrPublicationNotSendable, http.StatusConflict},
	{whatsappform.ErrProviderUnsupported, http.StatusConflict},
	{whatsappform.ErrNoEligibleAccount, http.StatusConflict},
	{whatsappform.ErrFormArchived, http.StatusConflict},
	{whatsappform.ErrFormInUse, http.StatusConflict},
	{whatsappform.ErrChannelUnsupported, http.StatusConflict},
	{whatsappform.ErrMetaRejected, http.StatusBadGateway},
}

func writeError(w http.ResponseWriter, err error) {
	var problems whatsappform.Problems
	if errors.As(err, &problems) {
		response.WriteErrorWithDetails(w, http.StatusUnprocessableEntity, whatsappform.ErrorCode(whatsappform.ErrDocumentInvalid),
			whatsappform.ErrDocumentInvalid.Error(), map[string]any{"problems": problems})
		return
	}
	code := whatsappform.ErrorCode(err)
	for _, known := range statuses {
		if errors.Is(err, known.err) {
			response.WriteErrorWithCode(w, known.status, code, err.Error(), nil)
			return
		}
	}
	if whatsappform.IsInputRefusal(err) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, code, err.Error(), nil)
		return
	}
	log.Printf("[whatsapp-forms] request failed: %v", err)
	response.WriteError(w, http.StatusInternalServerError, "Falha ao processar o formulário", nil)
}
