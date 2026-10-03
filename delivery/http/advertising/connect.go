package advertisinghttp

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"vozko/delivery/http/oauthpopup"
	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

// @Summary		Iniciar conexão de contas de anúncios
// @Description	Retorna a URL de autorização da Meta (Login for Business) para conectar contas de anúncios. Com redirect=1, redireciona direto.
// @Tags			Anúncios
// @Produce		json
// @Param			popup		query		string	false	"1 para devolver o resultado ao popup"
// @Param			returnPath	query		string	false	"caminho do painel para voltar"
// @Success		200			{object}	ConnectStartResponse
// @Failure		500			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/oauth/meta-ads/start [get]
func (h *Handler) StartConnect(w http.ResponseWriter, r *http.Request) {
	authorize, err := h.d.Connect.Start(adsuc.StartConnectInput{
		WorkspaceID: workspaceOf(r),
		UserID:      personOf(r).UserID,
		ReturnPath:  r.URL.Query().Get("returnPath"),
		Popup:       r.URL.Query().Get("popup") == "1",
	})
	if err != nil {
		log.Printf("[ads] connect start failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to start the Meta ads connection", nil)
		return
	}
	if r.URL.Query().Get("redirect") == "1" {
		http.Redirect(w, r, authorize, http.StatusFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ConnectStartResponse{AuthorizeURL: authorize})
}

// @Summary		Callback de conexão de contas de anúncios
// @Description	Finaliza o OAuth da Meta: devolve o resultado ao popup ou redireciona ao gerenciador de anúncios.
// @Tags			Anúncios
// @Success		302
// @Router			/oauth/meta-ads/callback [get]
func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	out, err := h.d.Connect.Complete(r.Context(), adsuc.CompleteConnectInput{
		Code: query.Get("code"), State: query.Get("state"), Error: query.Get("error"), ErrorReason: query.Get("error_reason"),
	})
	if err != nil {
		log.Printf("[ads] connect failed: %v", err)
		status, reason := "error", connectErrorCode(err)
		if reason == "declined" {
			status = "cancelled"
		}
		var connectErr *adsuc.ConnectError
		if errors.As(err, &connectErr) && connectErr.Popup {
			oauthpopup.WriteResult(w, h.d.FrontendBaseURL, map[string]any{"source": popupMessageSource, "status": status, "reason": reason})
			return
		}
		returnPath := managerPath
		if connectErr != nil {
			returnPath = connectErr.ReturnPath
		}
		oauthpopup.Redirect(w, r, h.d.FrontendBaseURL, returnPath, managerPath, url.Values{"ads": {status}, "reason": {reason}})
		return
	}
	connected := out.ConnectedCount()
	status := "connected"
	if connected < len(out.Accounts) {
		status = "partial"
	}
	if out.Popup {
		oauthpopup.WriteResult(w, h.d.FrontendBaseURL, map[string]any{"source": popupMessageSource, "status": status, "count": connected})
		return
	}
	oauthpopup.Redirect(w, r, h.d.FrontendBaseURL, out.ReturnPath, managerPath, url.Values{"ads": {status}, "connected": {strconv.Itoa(connected)}})
}

func connectErrorCode(err error) string {
	if code, ok := oauthpopup.StateErrorCode(err); ok {
		return code
	}
	switch {
	case errors.Is(err, advertising.ErrAuthorizationDenied):
		return "declined"
	case errors.Is(err, advertising.ErrMissingScopes):
		return "missing_permissions"
	case errors.Is(err, advertising.ErrNoAccountsGranted):
		return "no_accounts_granted"
	default:
		return "connect_failed"
	}
}
