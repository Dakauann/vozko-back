package googleaccount

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/delivery/http/oauthpopup"
	"vozko/delivery/http/response"
	"vozko/domain/googleaccount"
	"vozko/infra/http/middleware"
	gauc "vozko/usecases/googleaccount"
)

const PopupSource = "google-login"

type Connector interface {
	Start(ctx context.Context, in gauc.StartInput) (string, error)
	Complete(ctx context.Context, in gauc.CompleteInput) (*gauc.CompleteOutput, error)
}

type Disconnector interface {
	Disconnect(ctx context.Context, workspaceID string, product googleaccount.Product) error
}

type StatusReader interface {
	ProductStatus(ctx context.Context, workspaceID string, product googleaccount.Product) (gauc.ProductStatus, error)
}

type HandlerDeps struct {
	Connect         Connector
	Disconnect      Disconnector
	Status          StatusReader
	FrontendBaseURL string
}

type Handler struct {
	connect         Connector
	disconnect      Disconnector
	status          StatusReader
	frontendBaseURL string
}

func NewHandler(d HandlerDeps) *Handler {
	if d.Connect == nil || d.Disconnect == nil || d.Status == nil {
		panic("googleaccount http: the handler requires connect, disconnect and status")
	}
	if strings.TrimSpace(d.FrontendBaseURL) == "" {
		panic("googleaccount http: the frontend base url is required")
	}
	return &Handler{connect: d.Connect, disconnect: d.Disconnect, status: d.Status, frontendBaseURL: strings.TrimRight(d.FrontendBaseURL, "/")}
}

type StartResponse struct {
	AuthorizeURL string `json:"authorizeUrl" example:"https://accounts.google.com/o/oauth2/v2/auth?client_id=..."`
}

type ProductStatusResponse struct {
	Product      string     `json:"product" example:"sheets"`
	Connected    bool       `json:"connected" example:"true"`
	Status       string     `json:"status" example:"connected"`
	Email        string     `json:"email,omitempty" example:"operacao@empresa.com"`
	StatusReason string     `json:"statusReason,omitempty"`
	ConnectedAt  *time.Time `json:"connectedAt,omitempty"`
}

type DisconnectedResponse struct {
	Disconnected bool `json:"disconnected" example:"true"`
}

func (h *Handler) start(product googleaccount.Product) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := ""
		if claims := middleware.GetClaims(r); claims != nil {
			userID = claims.UserID
		}
		q := r.URL.Query()
		target, err := h.connect.Start(r.Context(), gauc.StartInput{
			WorkspaceID: middleware.GetWorkspaceID(r),
			UserID:      userID,
			Product:     product,
			ReturnPath:  q.Get("returnPath"),
			Popup:       q.Get("popup") == "1",
		})
		if err != nil {
			writeError(w, err)
			return
		}
		if q.Get("redirect") == "1" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		response.WriteSuccess(w, http.StatusOK, StartResponse{AuthorizeURL: target})
	}
}

// @Summary		Iniciar a conexão do Google Agenda
// @Description	Retorna a URL de autorização do Google para o Google Agenda. Com redirect=1 redireciona direto. A mesma conta Google pode servir ao Google Agenda e ao Google Planilhas.
// @Tags			Google
// @Produce		json
// @Param			returnPath	query		string	false	"Caminho do painel para voltar depois da conexão"
// @Param			popup		query		string	false	"1 quando a conexão roda em popup"
// @Param			redirect	query		string	false	"1 para redirecionar em vez de responder JSON"
// @Success		200			{object}	StartResponse
// @Success		302
// @Failure		400			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/oauth/google/calendar/start [get]
func (h *Handler) StartCalendar(w http.ResponseWriter, r *http.Request) {
	h.start(googleaccount.ProductCalendar)(w, r)
}

// @Summary		Iniciar a conexão do Google Planilhas
// @Description	Retorna a URL de autorização do Google para o Google Planilhas. Pede só o acesso aos arquivos criados pela plataforma (drive.file). Com redirect=1 redireciona direto.
// @Tags			Google
// @Produce		json
// @Param			returnPath	query		string	false	"Caminho do painel para voltar depois da conexão"
// @Param			popup		query		string	false	"1 quando a conexão roda em popup"
// @Param			redirect	query		string	false	"1 para redirecionar em vez de responder JSON"
// @Success		200			{object}	StartResponse
// @Success		302
// @Failure		400			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/oauth/google/sheets/start [get]
func (h *Handler) StartSheets(w http.ResponseWriter, r *http.Request) {
	h.start(googleaccount.ProductSheets)(w, r)
}

// @Summary		Callback de conexão do Google
// @Description	Conclui o OAuth do Google: devolve o resultado ao popup (postMessage com source google-login, status, product, email e reason) ou redireciona ao painel com ?google=<status>&product=<produto>&reason=<motivo>.
// @Tags			Google
// @Param			code	query	string	false	"Código de autorização"
// @Param			state	query	string	true	"Estado assinado emitido no início"
// @Param			error	query	string	false	"Erro devolvido pelo Google"
// @Success		200
// @Success		302
// @Router			/oauth/google/callback [get]
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.connect.Complete(r.Context(), gauc.CompleteInput{Code: q.Get("code"), State: q.Get("state"), Error: q.Get("error")})
	result := map[string]any{"source": PopupSource, "status": "connected"}
	if out != nil && out.Product != "" {
		result["product"] = string(out.Product)
	}
	if err != nil {
		result["status"], result["reason"] = "error", callbackReason(err)
	} else {
		result["email"] = out.Account.Email
	}
	if out != nil && out.Popup {
		oauthpopup.WriteResult(w, h.frontendBaseURL, result)
		return
	}
	returnPath := gauc.IntegrationsPath
	if out != nil && out.ReturnPath != "" {
		returnPath = out.ReturnPath
	}
	params := url.Values{"google": {result["status"].(string)}}
	for _, key := range []string{"product", "reason", "email"} {
		if v, ok := result[key].(string); ok {
			params.Set(key, v)
		}
	}
	oauthpopup.Redirect(w, r, h.frontendBaseURL, returnPath, gauc.IntegrationsPath, params)
}

func callbackReason(err error) string {
	if code, ok := oauthpopup.StateErrorCode(err); ok {
		return code
	}
	return googleaccount.ErrorCode(err)
}

func (h *Handler) productStatus(product googleaccount.Product) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := h.status.ProductStatus(r.Context(), middleware.GetWorkspaceID(r), product)
		if err != nil {
			writeError(w, err)
			return
		}
		response.WriteSuccess(w, http.StatusOK, ProductStatusResponse{
			Product:      string(status.Product),
			Connected:    status.Connected,
			Status:       string(status.Status),
			Email:        status.Email,
			StatusReason: status.StatusReason,
			ConnectedAt:  status.ConnectedAt,
		})
	}
}

// @Summary		Estado da conexão do Google Agenda
// @Tags			Google
// @Produce		json
// @Success		200	{object}	ProductStatusResponse
// @Security		BearerAuth
// @Router			/google/accounts/calendar [get]
func (h *Handler) CalendarStatus(w http.ResponseWriter, r *http.Request) {
	h.productStatus(googleaccount.ProductCalendar)(w, r)
}

// @Summary		Estado da conexão do Google Planilhas
// @Tags			Google
// @Produce		json
// @Success		200	{object}	ProductStatusResponse
// @Security		BearerAuth
// @Router			/google/accounts/sheets [get]
func (h *Handler) SheetsStatus(w http.ResponseWriter, r *http.Request) {
	h.productStatus(googleaccount.ProductSheets)(w, r)
}

func (h *Handler) disconnectProduct(product googleaccount.Product) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.disconnect.Disconnect(r.Context(), middleware.GetWorkspaceID(r), product); err != nil {
			writeError(w, err)
			return
		}
		response.WriteSuccess(w, http.StatusOK, DisconnectedResponse{Disconnected: true})
	}
}

// @Summary		Desconectar o Google Agenda
// @Description	Remove o Google Agenda do workspace. A autorização no Google só é revogada quando nenhum outro produto ou workspace usa a mesma conta.
// @Tags			Google
// @Produce		json
// @Success		200	{object}	DisconnectedResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google/accounts/calendar [delete]
func (h *Handler) DisconnectCalendar(w http.ResponseWriter, r *http.Request) {
	h.disconnectProduct(googleaccount.ProductCalendar)(w, r)
}

// @Summary		Desconectar o Google Planilhas
// @Description	Remove o Google Planilhas do workspace. As linhas pendentes ficam guardadas até uma nova conexão. A autorização no Google só é revogada quando nenhum outro produto ou workspace usa a mesma conta.
// @Tags			Google
// @Produce		json
// @Success		200	{object}	DisconnectedResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google/accounts/sheets [delete]
func (h *Handler) DisconnectSheets(w http.ResponseWriter, r *http.Request) {
	h.disconnectProduct(googleaccount.ProductSheets)(w, r)
}

var refusals = []response.ErrorRefusal{
	{Err: googleaccount.ErrWorkspaceRequired, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "workspace_required", Message: "Workspace obrigatório"}},
	{Err: googleaccount.ErrUnknownProduct, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: googleaccount.CodeUnknownProduct, Message: "Produto do Google desconhecido"}},
	{Err: googleaccount.ErrNotConnected, Refusal: response.Refusal{Status: http.StatusNotFound, Code: googleaccount.CodeNotConnected, Message: "Nenhuma conta Google conectada para este produto"}},
}

func writeError(w http.ResponseWriter, err error) {
	if response.WriteErrorRefusal(w, err, refusals) {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	log.Printf("[googleaccount] request failed: %v", err)
	response.WriteErrorWithCode(w, http.StatusInternalServerError, codeFailed, "Não foi possível concluir a operação com o Google", nil)
}

const codeFailed = "google_account_failed"

var callbackCodes = []string{
	"invalid_state", "expired_state", googleaccount.CodeConnectFailed, googleaccount.CodeAuthorizationDeny,
	googleaccount.CodeScopeNotGranted, googleaccount.CodeNeedsReconnect,
}

func RefusalCodes() []string {
	return append(append(response.RefusalCodes(response.ErrorRefusalsOf(refusals)), codeFailed), callbackCodes...)
}
