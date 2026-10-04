package webchat

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/user"
	wcdomain "vozko/domain/webchat"
	"vozko/infra/http/middleware"
	wcuc "vozko/usecases/webchat"
)

const maxManagementBody = 64 << 10

type Handler struct {
	widgets    *wcuc.Widgets
	moderation *wcuc.Moderation
	publicBase string
}

func NewHandler(widgets *wcuc.Widgets, moderation *wcuc.Moderation, publicBase string) *Handler {
	return &Handler{widgets: widgets, moderation: moderation, publicBase: publicBase}
}

type WidgetRequest struct {
	Name                 *string   `json:"name"`
	DepartmentID         *string   `json:"departmentId"`
	Status               *string   `json:"status" enums:"active,paused"`
	AllowedOrigins       *[]string `json:"allowedOrigins"`
	AccentColor          *string   `json:"accentColor"`
	Position             *string   `json:"position" enums:"right,left"`
	LauncherLabel        *string   `json:"launcherLabel"`
	WelcomeTitle         *string   `json:"welcomeTitle"`
	WelcomeMessage       *string   `json:"welcomeMessage"`
	TeamName             *string   `json:"teamName"`
	AssistantName        *string   `json:"assistantName"`
	IntakeName           *string   `json:"intakeName" enums:"hidden,optional,required"`
	IntakeEmail          *string   `json:"intakeEmail" enums:"hidden,optional,required"`
	IntakePhone          *string   `json:"intakePhone" enums:"hidden,optional,required"`
	PrivacyPolicyURL     *string   `json:"privacyPolicyUrl"`
	DefaultCountryCode   *string   `json:"defaultCountryCode"`
	AllowHumanRequest    *bool     `json:"allowHumanRequest"`
	AllowAttachments     *bool     `json:"allowAttachments"`
	IdentityMode         *string   `json:"identityMode" enums:"off,optional,required"`
	AgentID              *string   `json:"agentId"`
	WorkflowID           *string   `json:"workflowId"`
	PipelineID           *string   `json:"pipelineId"`
	EnableAgentResponses *bool     `json:"enableAgentResponses"`
	EnableWorkflow       *bool     `json:"enableWorkflow"`
	EnableAnalysis       *bool     `json:"enableAnalysis"`
	EnableAutoStaging    *bool     `json:"enableAutoStaging"`
	EnableAutoMemory     *bool     `json:"enableAutoMemory"`
}

func (r WidgetRequest) input() wcuc.WidgetInput {
	return wcuc.WidgetInput{
		Name: r.Name, DepartmentID: r.DepartmentID, Status: as[wcdomain.Status](r.Status), AllowedOrigins: r.AllowedOrigins,
		AccentColor: r.AccentColor, Position: as[wcdomain.Position](r.Position), LauncherLabel: r.LauncherLabel,
		WelcomeTitle: r.WelcomeTitle, WelcomeMessage: r.WelcomeMessage, TeamName: r.TeamName,
		AssistantName: r.AssistantName, IntakeName: as[wcdomain.FieldRule](r.IntakeName), IntakeEmail: as[wcdomain.FieldRule](r.IntakeEmail),
		IntakePhone: as[wcdomain.FieldRule](r.IntakePhone), PrivacyPolicyURL: r.PrivacyPolicyURL, DefaultCountryCode: r.DefaultCountryCode,
		AllowHumanRequest: r.AllowHumanRequest, AllowAttachments: r.AllowAttachments, IdentityMode: as[wcdomain.IdentityMode](r.IdentityMode),
		AgentID: r.AgentID, WorkflowID: r.WorkflowID, PipelineID: r.PipelineID,
		EnableAgentResponses: r.EnableAgentResponses, EnableWorkflow: r.EnableWorkflow,
		EnableAnalysis: r.EnableAnalysis, EnableAutoStaging: r.EnableAutoStaging, EnableAutoMemory: r.EnableAutoMemory,
	}
}

type SecretResponse struct {
	IdentitySecret string `json:"identitySecret"`
}

type BlockRequest struct {
	Blocked bool `json:"blocked"`
}

func (h *Handler) present(w *wcdomain.Widget) WidgetResponse {
	return toWidgetResponse(w, h.publicBase)
}

func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, into any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil || json.Unmarshal(body, into) != nil {
		response.WriteCodedError(w, http.StatusBadRequest, "invalid_body", "invalid request body")
		return false
	}
	return true
}

func personOf(r *http.Request) shared.Person {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return shared.Person{}
	}
	return shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}
}

// @Summary		Listar WebChats
// @Description	WebChats do workspace, do mais novo ao mais antigo, com o código de instalação de cada um (snippet). search filtra pelo nome.
// @Tags			WebChat
// @Produce		json
// @Param			search		query		string	false	"parte do nome"
// @Param			page		query		int		false	"página"
// @Param			pageSize	query		int		false	"itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]WidgetResponse}
// @Failure		403			{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := h.widgets.List(r.Context(), wcdomain.ListWidgetsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Search:      query.Get("search"),
		Options:     shared.QueryOptions{Pagination: httpx.ParsePagination(query)},
	})
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]WidgetResponse, 0, len(result.Items))
	for _, widget := range result.Items {
		items = append(items, h.present(widget))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page: result.Page, PageSize: result.PageSize, TotalItems: result.TotalItems, TotalPages: result.TotalPages,
	})
}

// @Summary		Criar WebChat
// @Description	Cria um chat para instalar em sites. allowedOrigins é obrigatório e aceita só origens exatas (https://www.exemplo.com) ou subdomínios (https://*.exemplo.com); http só para localhost. O chat só abre nesses sites. Agente, fluxo, funil e departamento precisam ser deste workspace (422 reference_not_in_workspace). Com identityMode optional ou required é gerado um segredo para assinar a identidade do visitante (GET .../identity-secret).
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			body	body		WidgetRequest	true	"configuração do chat"
// @Success		201		{object}	WidgetResponse
// @Failure		400		{object}	response.CodedErrorResponse
// @Failure		422		{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req WidgetRequest
	if !decodeBody(w, r, maxManagementBody, &req) {
		return
	}
	widget, err := h.widgets.Create(r.Context(), middleware.GetWorkspaceID(r), req.input())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, h.present(widget))
}

// @Summary		Consultar WebChat
// @Description	Configuração de um WebChat do workspace e o código de instalação.
// @Tags			WebChat
// @Produce		json
// @Param			id	path		string	true	"id do chat"
// @Success		200	{object}	WidgetResponse
// @Failure		404	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	widget, err := h.widgets.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present(widget))
}

// @Summary		Editar WebChat
// @Description	Altera só os campos enviados. As mesmas regras da criação valem para sites permitidos, formulário inicial e referências.
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"id do chat"
// @Param			body	body		WidgetRequest	true	"campos a alterar"
// @Success		200		{object}	WidgetResponse
// @Failure		404		{object}	response.CodedErrorResponse
// @Failure		422		{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets/{id} [put]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req WidgetRequest
	if !decodeBody(w, r, maxManagementBody, &req) {
		return
	}
	widget, err := h.widgets.Update(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], req.input())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present(widget))
}

// @Summary		Remover WebChat
// @Description	Remove o chat; ele deixa de abrir em todos os sites. As conversas continuam no CRM.
// @Tags			WebChat
// @Param			id	path	string	true	"id do chat"
// @Success		204
// @Failure		404	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.widgets.Delete(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Ver segredo de identidade
// @Description	Segredo com que o servidor do site assina, em HS256, o JWT de identidade do visitante (sub obrigatório, exp obrigatório e no máximo 24 h à frente; name, email e phone opcionais). Nunca coloque o segredo no navegador.
// @Tags			WebChat
// @Produce		json
// @Param			id	path		string	true	"id do chat"
// @Success		200	{object}	SecretResponse
// @Failure		404	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets/{id}/identity-secret [get]
func (h *Handler) RevealSecret(w http.ResponseWriter, r *http.Request) {
	secret, err := h.widgets.RevealIdentitySecret(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.WriteSuccess(w, http.StatusOK, SecretResponse{IdentitySecret: secret})
}

// @Summary		Trocar segredo de identidade
// @Description	Gera um novo segredo; JWTs assinados com o anterior deixam de valer na hora.
// @Tags			WebChat
// @Produce		json
// @Param			id	path		string	true	"id do chat"
// @Success		200	{object}	SecretResponse
// @Failure		404	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/widgets/{id}/identity-secret [post]
func (h *Handler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	secret, err := h.widgets.RotateIdentitySecret(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.WriteSuccess(w, http.StatusOK, SecretResponse{IdentitySecret: secret})
}

// @Summary		Bloquear visitante do WebChat
// @Description	blocked true bloqueia o visitante da conversa: o chat dele para de funcionar na hora e a resposta fica fechada no CRM. blocked false desbloqueia. Exige acesso à conversa.
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			entryId	path		string			true	"id da conversa"
// @Param			body	body		BlockRequest	true	"bloquear ou desbloquear"
// @Success		204
// @Failure		404		{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/webchat/conversations/{entryId}/block [put]
func (h *Handler) Block(w http.ResponseWriter, r *http.Request) {
	var req BlockRequest
	if !decodeBody(w, r, maxManagementBody, &req) {
		return
	}
	if err := h.moderation.SetBlocked(r.Context(), personOf(r), middleware.GetWorkspaceID(r), mux.Vars(r)["entryId"], req.Blocked); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func as[T ~string](value *string) *T {
	if value == nil {
		return nil
	}
	converted := T(*value)
	return &converted
}
