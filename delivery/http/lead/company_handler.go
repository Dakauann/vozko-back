package lead

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/leadcompany"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

const (
	companyIDPath  = leadIDPath
	maxCompanyBody = 8 << 10
)

type Companies interface {
	Create(ctx context.Context, a lead_usecase.Actor, d leadcompany.Draft) (leadcompany.Company, error)
	Update(ctx context.Context, a lead_usecase.Actor, id string, p leadcompany.Patch) (leadcompany.Company, error)
	Get(ctx context.Context, a lead_usecase.Actor, id string) (leadcompany.Company, error)
	List(ctx context.Context, a lead_usecase.Actor, q leadcompany.ListQuery) (*shared.PaginatedResult[leadcompany.Company], error)
	Delete(ctx context.Context, a lead_usecase.Actor, id string) error
}

type CompanyHandler struct {
	companies Companies
}

func NewCompanyHandler(companies Companies) *CompanyHandler {
	return &CompanyHandler{companies: companies}
}

func RegisterCompanyRoutes(
	protected *mux.Router,
	h *CompanyHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		h = NewCompanyHandler(nil)
	}
	res := workspace_domain.ResourceLeadCompanies
	protected.HandleFunc("/lead-companies", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/lead-companies", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/lead-companies"+companyIDPath, ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/lead-companies"+companyIDPath, ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPatch)
	protected.HandleFunc("/lead-companies"+companyIDPath, ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}

func (h *CompanyHandler) actor(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, bool) {
	if h.companies == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_companies_unavailable", "As empresas de leads não estão disponíveis neste servidor", nil)
		return lead_usecase.Actor{}, false
	}
	a, ok := actorOf(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
	}
	return a, ok
}

func writeCompanyError(w http.ResponseWriter, err error) {
	coded := func(status int) {
		response.WriteErrorWithCode(w, status, leadcompany.ErrorCode(err), err.Error(), nil)
	}
	switch {
	case errors.Is(err, leadcompany.ErrForbidden):
		coded(http.StatusForbidden)
	case errors.Is(err, leadcompany.ErrNotFound):
		coded(http.StatusNotFound)
	case leadcompany.IsInputRefusal(err):
		coded(http.StatusBadRequest)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Falha ao salvar a empresa", nil)
	}
}

func companyListQuery(values url.Values) (leadcompany.ListQuery, bool) {
	q := leadcompany.ListQuery{Search: values.Get("search")}
	for key, target := range map[string]*int{"page": &q.Page, "pageSize": &q.PageSize} {
		raw := values.Get(key)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return leadcompany.ListQuery{}, false
		}
		*target = n
	}
	return q, true
}

// @Summary		Listar empresas de leads
// @Description	Lista as empresas do workspace em ordem alfabética, com a imagem (URL pública e prévia de 200px), se aceitam novos leads (`acceptsLeads`) e quantos leads fazem parte de cada uma (`membersCount`). `search` procura no nome sem diferenciar acentos nem maiúsculas. Paginada por `page` e `pageSize` (até 100). Os leads de uma empresa são a lista GET /leads com o filtro `{"field":"company","operator":"in","values":[id]}`; `is_empty` lista os leads sem empresa.
// @Tags			Leads
// @Produce		json
// @Param			search		query		string	false	"Parte do nome"
// @Param			page		query		int		false	"Página (começa em 1)"
// @Param			pageSize	query		int		false	"Itens por página (até 100)"
// @Success		200			{object}	lead.LeadCompanyListResponse
// @Failure		400			{object}	response.ErrorResponse	"lead_company_list_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/lead-companies [get]
func (h *CompanyHandler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	q, ok := companyListQuery(r.URL.Query())
	if !ok {
		writeCompanyError(w, leadcompany.ErrListQueryInvalid)
		return
	}
	page, err := h.companies.List(r.Context(), a, q)
	if err != nil {
		writeCompanyError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadCompanyList(page))
}

// @Summary		Cadastrar empresa de leads
// @Description	Cria uma empresa para agrupar leads. `acceptsLeads` liga ou desliga a entrada de novos leads e vale `true` quando omitido; desligado, ninguém entra, mas quem já faz parte continua e pode sair. A imagem é opcional: envie o arquivo antes em POST /medias com `mediaType=image` e mande aqui o `id` devolvido em `imageMediaId`; uma mídia que não é imagem, ou é de outro workspace, é recusada com 400 `lead_company_image_invalid`. O nome tem até 120 caracteres.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			body	body		lead.CreateLeadCompanyRequest	true	"Empresa"
// @Success		201		{object}	lead.LeadCompanyResponse
// @Failure		400		{object}	response.ErrorResponse	"invalid_body, lead_company_name_required, lead_company_name_too_long, lead_company_image_invalid"
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/lead-companies [post]
func (h *CompanyHandler) Create(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCompanyBody)
	var body CreateLeadCompanyRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	company, err := h.companies.Create(r.Context(), a, body.toDomain())
	if err != nil {
		writeCompanyError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toLeadCompany(company))
}

// @Summary		Ver empresa de leads
// @Description	Devolve uma empresa do workspace com a imagem e a contagem de leads. Uma empresa apagada ou de outro workspace responde 404.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID da empresa"
// @Success		200	{object}	lead.LeadCompanyResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"lead_company_not_found"
// @Security		BearerAuth
// @Router			/lead-companies/{id} [get]
func (h *CompanyHandler) Get(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	company, err := h.companies.Get(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeCompanyError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadCompany(company))
}

// @Summary		Alterar empresa de leads
// @Description	Altera só os campos enviados: `name`, `acceptsLeads` (desligar fecha a entrada de novos leads sem tirar ninguém) e `imageMediaId` (o ID de uma imagem enviada em POST /medias, ou "" para tirar a imagem).
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string							true	"ID da empresa"
// @Param			body	body		lead.UpdateLeadCompanyRequest	true	"Campos a alterar"
// @Success		200		{object}	lead.LeadCompanyResponse
// @Failure		400		{object}	response.ErrorResponse	"invalid_body, lead_company_name_required, lead_company_name_too_long, lead_company_image_invalid"
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Failure		404		{object}	response.ErrorResponse	"lead_company_not_found"
// @Security		BearerAuth
// @Router			/lead-companies/{id} [patch]
func (h *CompanyHandler) Update(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCompanyBody)
	var body UpdateLeadCompanyRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	company, err := h.companies.Update(r.Context(), a, mux.Vars(r)["id"], body.toDomain())
	if err != nil {
		writeCompanyError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadCompany(company))
}

// @Summary		Excluir empresa de leads
// @Description	Exclui a empresa. Os leads dela continuam cadastrados, agora sem empresa: cada um ganha uma versão nova e o evento `company_changed` no histórico, na mesma transação. Quem tenta colocar um lead na empresa ao mesmo tempo recebe 404 `lead_company_not_found`.
// @Tags			Leads
// @Param			id	path	string	true	"ID da empresa"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"lead_company_not_found"
// @Security		BearerAuth
// @Router			/lead-companies/{id} [delete]
func (h *CompanyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.companies.Delete(r.Context(), a, mux.Vars(r)["id"]); err != nil {
		writeCompanyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
