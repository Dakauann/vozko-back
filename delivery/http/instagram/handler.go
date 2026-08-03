package instagram

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	iguc "vozko/usecases/instagram"
)

// Handler serves the Instagram account, OAuth, posts and comments endpoints.
type Handler struct {
	connect     *iguc.ConnectAccountUseCase
	list        *iguc.ListAccountsUseCase
	get         *iguc.GetAccountUseCase
	updateCfg   *iguc.UpdateAccountConfigUseCase
	disconnect  *iguc.DisconnectAccountUseCase
	manageRules *iguc.ManageCommentRulesUseCase

	listMedia         *iguc.ListMediaUseCase
	getMedia          *iguc.GetMediaUseCase
	proxyMedia        *iguc.ProxyMediaUseCase
	proxyAvatar       *iguc.ProxyAvatarUseCase
	createMedia       *iguc.CreateMediaUseCase
	setCommentEnabled *iguc.SetCommentEnabledUseCase

	listComments *iguc.ListCommentsUseCase
	replyComment *iguc.ReplyToCommentUseCase
	moderate     *iguc.ModerateCommentUseCase
	privateReply *iguc.SendPrivateReplyUseCase

	// frontendBaseURL is where the OAuth callback sends the browser.
	frontendBaseURL string
}

// HandlerDeps groups the usecases.
type HandlerDeps struct {
	Connect     *iguc.ConnectAccountUseCase
	List        *iguc.ListAccountsUseCase
	Get         *iguc.GetAccountUseCase
	UpdateCfg   *iguc.UpdateAccountConfigUseCase
	ManageRules *iguc.ManageCommentRulesUseCase
	Disconnect  *iguc.DisconnectAccountUseCase

	ListMedia         *iguc.ListMediaUseCase
	GetMedia          *iguc.GetMediaUseCase
	ProxyMedia        *iguc.ProxyMediaUseCase
	ProxyAvatar       *iguc.ProxyAvatarUseCase
	CreateMedia       *iguc.CreateMediaUseCase
	SetCommentEnabled *iguc.SetCommentEnabledUseCase

	ListComments *iguc.ListCommentsUseCase
	ReplyComment *iguc.ReplyToCommentUseCase
	Moderate     *iguc.ModerateCommentUseCase
	PrivateReply *iguc.SendPrivateReplyUseCase

	FrontendBaseURL string
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{
		connect:           d.Connect,
		list:              d.List,
		get:               d.Get,
		updateCfg:         d.UpdateCfg,
		manageRules:       d.ManageRules,
		disconnect:        d.Disconnect,
		listMedia:         d.ListMedia,
		getMedia:          d.GetMedia,
		proxyMedia:        d.ProxyMedia,
		proxyAvatar:       d.ProxyAvatar,
		createMedia:       d.CreateMedia,
		setCommentEnabled: d.SetCommentEnabled,
		listComments:      d.ListComments,
		replyComment:      d.ReplyComment,
		moderate:          d.Moderate,
		privateReply:      d.PrivateReply,
		frontendBaseURL:   strings.TrimRight(d.FrontendBaseURL, "/"),
	}
}

// ---------------------------------------------------------------- OAuth

// StartConnect begins onboarding.
//
// Business Login for Instagram is just a URL, there is no Facebook JS SDK and no
// config_id, unlike WhatsApp Embedded Signup. Two transports are supported over
// that one URL:
//
//   - popup=1  : the callback answers with a page that posts the result to
//     window.opener, matching the WhatsApp connect experience so the
//     dashboard tab is never navigated away from.
//
//   - otherwise: the callback 302s back to returnPath, which is also the
//     fallback when a popup is blocked and the mobile-friendly path
//     where window.opener is often null.
//
//     @Summary		Iniciar conexão de conta do Instagram
//     @Description	Retorna a URL de autorização do Instagram para conectar uma conta profissional.
//     @Tags			Instagram
//     @Produce		json
//     @Success		200	{object}	ConnectStartResponse
//     @Failure		400	{object}	response.ErrorResponse
//     @Security		BearerAuth
//     @Router			/instagram/oauth/start [get]
func (h *Handler) StartConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace is required", nil)
		return
	}
	claims := middleware.GetClaims(r)
	userID := ""
	if claims != nil {
		userID = claims.UserID
	}

	out, err := h.connect.Start(r.Context(), iguc.StartConnectInput{
		WorkspaceID: workspaceID,
		UserID:      userID,
		ReturnPath:  r.URL.Query().Get("returnPath"),
		// popup=1 makes the callback answer with a postMessage page instead of a
		// redirect, so the dashboard tab is never navigated away from.
		Popup: r.URL.Query().Get("popup") == "1",
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to start Instagram connection", nil)
		return
	}

	// Both forms are supported: `redirect=1` bounces straight to Instagram (the
	// plain-anchor flow), otherwise the URL is returned for the client to follow.
	if r.URL.Query().Get("redirect") == "1" {
		http.Redirect(w, r, out.AuthorizeURL, http.StatusFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ConnectStartResponse{AuthorizeURL: out.AuthorizeURL})
}

// HandleCallback completes the OAuth flow.
//
// This route is public because Instagram redirects the browser here directly.
// Authorization comes from the signed, single-use state rather than from the
// session, which also blocks CSRF and replay.
//
//	@Summary		Callback de conexão do Instagram
//	@Tags			Instagram
//	@Router			/oauth/instagram/callback [get]
func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	out, err := h.connect.Complete(r.Context(), iguc.CompleteConnectInput{
		Code:        query.Get("code"),
		State:       query.Get("state"),
		Error:       query.Get("error"),
		ErrorReason: query.Get("error_reason"),
	})
	if err != nil {
		// Log the real error. Without this a failed connect is completely silent
		// server-side: the user only sees a translated toast, and the actual cause
		// (which upstream call failed, which scope was missing) is unrecoverable.
		log.Printf("[instagram] connect failed: %v", err)

		result := connectResult{Status: "error", Reason: connectErrorCode(err)}
		// A failure before the state could be decoded has no transport hint, so
		// fall back to a redirect. When the state did decode we honour its mode.
		if popupHint(query) {
			h.writePopupResult(w, result)
			return
		}
		h.redirectWithResult(w, r, "/dashboard/instagram-accounts", result)
		return
	}

	result := connectResult{Status: "connected", Username: out.Account.Username}
	if out.Reconnected {
		result.Status = "reconnected"
	}

	if out.Popup {
		h.writePopupResult(w, result)
		return
	}
	h.redirectWithResult(w, r, out.ReturnPath, result)
}

// connectResult is the onboarding outcome, delivered either as query params on a
// redirect or as a postMessage payload in popup mode.
type connectResult struct {
	Status   string `json:"status"`
	Username string `json:"username,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// popupHint recovers the transport when the state itself could not be decoded.
func popupHint(query url.Values) bool {
	return query.Get("popup") == "1"
}

// writePopupResult answers the popup with a minimal page that hands the result to
// the opener and closes itself.
//
// The message is posted to the frontend origin explicitly rather than to "*", so
// the payload is never readable by another origin that happens to hold a
// reference to this window. The page carries no user-controlled markup: the status
// fields are JSON-encoded into a script literal, and the visible text is static.
func (h *Handler) writePopupResult(w http.ResponseWriter, result connectResult) {
	payload, err := json.Marshal(map[string]string{
		"source":   "ig-business-login",
		"status":   result.Status,
		"username": result.Username,
		"reason":   result.Reason,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to render result", nil)
		return
	}

	targetOrigin := h.frontendBaseURL
	if targetOrigin == "" {
		// Without a configured frontend origin there is no safe target to post to,
		// so say so rather than broadcasting to "*".
		targetOrigin = "null"
	}
	origin, err := json.Marshal(targetOrigin)
	if err != nil {
		origin = []byte(`"null"`)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Instagram</title></head>
<body style="font:14px system-ui;padding:24px;text-align:center;color:#444">
<p>You can close this window.</p>
<script>
(function () {
  var payload = %s;
  var target = %s;
  try {
    if (window.opener && target !== "null") {
      window.opener.postMessage(payload, target);
    }
  } catch (e) {}
  setTimeout(function () { try { window.close(); } catch (e) {} }, 300);
})();
</script>
</body></html>`, payload, origin)
}

// redirectWithResult sends the browser back to the dashboard with a result the UI
// can turn into a toast. Used when onboarding was launched as a full-page
// redirect (or when the popup was blocked).
func (h *Handler) redirectWithResult(w http.ResponseWriter, r *http.Request, path string, result connectResult) {
	target := h.frontendBaseURL + iguc.SafeReturnPath(path, "/dashboard/instagram-accounts")

	parsed, err := url.Parse(target)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Invalid redirect target", nil)
		return
	}
	q := parsed.Query()
	q.Set("instagram", result.Status)
	if result.Username != "" {
		q.Set("username", result.Username)
	}
	if result.Reason != "" {
		q.Set("reason", result.Reason)
	}
	parsed.RawQuery = q.Encode()

	http.Redirect(w, r, parsed.String(), http.StatusFound)
}

// connectErrorCode maps a failure onto a stable code the UI can translate.
func connectErrorCode(err error) string {
	switch {
	case errors.Is(err, iguc.ErrInvalidState), errors.Is(err, iguc.ErrReplayedState):
		return "invalid_state"
	case errors.Is(err, iguc.ErrExpiredState):
		return "expired_state"
	case errors.Is(err, igdomain.ErrMissingMessagingScope):
		return "missing_messaging_scope"
	case errors.Is(err, igdomain.ErrAccountAlreadyLinked):
		return "already_linked"
	default:
		return "connect_failed"
	}
}

// ---------------------------------------------------------------- accounts

// @Summary		Listar contas do Instagram
// @Tags			Instagram
// @Produce		json
// @Success		200	{object}	AccountResponse
// @Security		BearerAuth
// @Router			/instagram/accounts [get]
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	pagination := httpx.ParsePagination(r.URL.Query())

	input := igdomain.ListAccountsInput{
		WorkspaceID: workspaceID,
		Search:      strings.TrimSpace(r.URL.Query().Get("search")),
		Options:     shared.QueryOptions{Pagination: pagination},
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		status := igdomain.Status(strings.ToUpper(raw))
		if status.Valid() {
			input.Status = &status
		}
	}

	result, err := h.list.Execute(r.Context(), input)
	if err != nil {
		writeDomainError(w, err, "Failed to list Instagram accounts")
		return
	}

	response.WritePaginated(w, http.StatusOK, toAccountResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

// @Summary		Obter conta do Instagram
// @Tags			Instagram
// @Produce		json
// @Success		200	{object}	AccountResponse
// @Security		BearerAuth
// @Router			/instagram/accounts/{id} [get]
func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	account, err := h.get.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to load Instagram account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountResponse(account))
}

// @Summary		Atualizar configuração da conta do Instagram
// @Tags			Instagram
// @Accept			json
// @Produce		json
// @Success		200	{object}	AccountResponse
// @Security		BearerAuth
// @Router			/instagram/accounts/{id} [put]
func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req UpdateAccountConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	account, err := h.updateCfg.Execute(r.Context(), iguc.UpdateAccountConfigInput{
		WorkspaceID:          middleware.GetWorkspaceID(r),
		ID:                   mux.Vars(r)["id"],
		DepartmentID:         req.DepartmentID,
		AgentID:              req.AgentID,
		WorkflowID:           req.WorkflowID,
		PipelineID:           req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		EnableAnalysis:       req.EnableAnalysis,
		EnableAutoStaging:    req.EnableAutoStaging,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to update Instagram account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountResponse(account))
}

// @Summary		Desconectar conta do Instagram
// @Tags			Instagram
// @Security		BearerAuth
// @Router			/instagram/accounts/{id} [delete]
func (h *Handler) DisconnectAccount(w http.ResponseWriter, r *http.Request) {
	if err := h.disconnect.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err, "Failed to disconnect Instagram account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// ---------------------------------------------------------------- shared

// maxRequestBody bounds a JSON body, matching the 1 MiB cap the webhook and
// embedded-signup handlers already use.
const maxRequestBody = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
	if err := decoder.Decode(target); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return false
	}
	return true
}

// writeDomainError maps domain errors onto HTTP status codes in one place.
func writeDomainError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, igdomain.ErrAccountNotFound),
		errors.Is(err, igdomain.ErrContactNotFound),
		errors.Is(err, igdomain.ErrConversationNotFound),
		errors.Is(err, igdomain.ErrMediaNotFound),
		errors.Is(err, igdomain.ErrCommentNotFound):
		response.WriteError(w, http.StatusNotFound, "Not found", nil)

	case errors.Is(err, igdomain.ErrAccountAlreadyLinked):
		response.WriteErrorWithCode(w, http.StatusConflict, "already_linked",
			"This Instagram account is already connected", nil)

	case errors.Is(err, igdomain.ErrWorkspaceIDRequired),
		errors.Is(err, igdomain.ErrIGUserIDRequired),
		errors.Is(err, igdomain.ErrTextTooLong):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)

	case errors.Is(err, igdomain.ErrMissingMessagingScope):
		response.WriteErrorWithCode(w, http.StatusForbidden, "missing_messaging_scope",
			"The Instagram account did not grant the messaging permission", nil)

	case errors.Is(err, igdomain.ErrAccessTokenRequired):
		response.WriteErrorWithCode(w, http.StatusConflict, "reconnect_required",
			"This Instagram account needs to be reconnected", nil)

	// The one-shot private-reply rules are user-facing states, not server faults.
	case errors.Is(err, igdomain.ErrPrivateReplyUsed):
		response.WriteErrorWithCode(w, http.StatusConflict, "private_reply_used",
			"Instagram allows only one private reply per comment", nil)

	case errors.Is(err, igdomain.ErrPrivateReplyExpired):
		response.WriteErrorWithCode(w, http.StatusConflict, "private_reply_expired",
			"Private replies must be sent within 7 days of the comment", nil)

	default:
		response.WriteError(w, http.StatusInternalServerError, fallback, nil)
	}
}
