package customerportfoliohttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
	customerportfolio_usecase "vozko/usecases/customerportfolio"
)

const everyHolder = "all"

type Actor = customerportfolio_usecase.Actor

type Service interface {
	List(ctx context.Context, a Actor, q cp.ListQuery) (*shared.PaginatedResult[cp.Row], error)
	ForConversation(ctx context.Context, a Actor, ref shared.EntryRef) (cp.Holding, error)
	ForLead(ctx context.Context, a Actor, leadID string) (cp.Holding, error)
	PlaceConversation(ctx context.Context, a Actor, ref shared.EntryRef, holderID string) (cp.Holding, error)
	PlaceLead(ctx context.Context, a Actor, leadID, holderID string) (cp.Holding, error)
	Move(ctx context.Context, a Actor, entryID, holderID string) (cp.Holding, error)
	Remove(ctx context.Context, a Actor, entryID string) error
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	if h.service == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "customer_portfolio_unavailable", "A carteira de clientes não está disponível neste servidor", nil)
		return Actor{}, false
	}
	a, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
	}
	return a, ok
}

func conversationRef(w http.ResponseWriter, r *http.Request) (shared.EntryRef, bool) {
	vars := mux.Vars(r)
	ref := shared.EntryRef{EntryID: vars["entryId"], EntryType: shared.EntryType(vars["entryType"])}
	if !ref.EntryType.SupportsConversationView() {
		response.WriteErrorWithCode(w, http.StatusBadRequest, cp.ErrorCode(cp.ErrSubjectInvalid), "unknown channel "+vars["entryType"], nil)
		return shared.EntryRef{}, false
	}
	return ref, true
}

func listQuery(values url.Values, callerID string) (cp.ListQuery, bool) {
	q := cp.ListQuery{Search: values.Get("search"), HolderID: callerID}
	switch holder := strings.TrimSpace(values.Get("holderId")); holder {
	case "":
	case everyHolder:
		q.HolderID = ""
	default:
		q.HolderID = holder
	}
	for key, target := range map[string]*int{"page": &q.Page, "pageSize": &q.PageSize} {
		raw := values.Get(key)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cp.ListQuery{}, false
		}
		*target = n
	}
	return q, true
}

// @Summary		Listar a carteira de clientes
// @Description	Lista quem está numa carteira, do mais recente para o mais antigo. Sem `holderId`, lista a carteira de quem chama (exige `customer_portfolios:read`); `holderId` com o ID de outro membro, ou `all` para todas as carteiras do workspace, exige `customer_portfolios:manage`. Cada cliente é um lead (todas as conversas dele em qualquer canal) ou um contato de um canal sem lead (perfil do Instagram ou do Facebook, Telegram sem telefone, visitante do chat do site); `display` traz nome, perfil (@), número e foto de quem está na carteira. `holderReceives` é falso quando o dono da carteira não pode mais receber conversas (saiu do workspace, perdeu a permissão da carteira ou o acesso às conversas): os clientes dele voltam para a roleta até alguém movê-los. `search` procura em nome, perfil e número sem diferenciar acentos nem maiúsculas. Paginada por `page` e `pageSize` (até 100).
// @Tags			Carteira de clientes
// @Produce		json
// @Param			holderId	query		string	false	"ID do membro, ou all; sem valor, a própria carteira"
// @Param			search		query		string	false	"Parte do nome, do perfil ou do número"
// @Param			page		query		int		false	"Página (começa em 1)"
// @Param			pageSize	query		int		false	"Itens por página (até 100)"
// @Success		200			{object}	customerportfoliohttp.ListResponse
// @Failure		400			{object}	response.ErrorResponse	"customer_portfolio_list_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/customer-portfolio/entries [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	q, ok := listQuery(r.URL.Query(), a.UserID)
	if !ok {
		writeError(w, cp.ErrListQueryInvalid)
		return
	}
	page, err := h.service.List(r.Context(), a, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toList(page))
}

// @Summary		Ver de quem é o cliente de uma conversa
// @Description	Diz se o cliente da conversa está numa carteira e de quem. O cliente é o lead da conversa; quando a conversa não tem lead (Instagram, Facebook, Telegram sem telefone, chat do site sem cadastro), é o contato do canal. `entry` é null quando o cliente não está em nenhuma carteira. Exige `customer_portfolios:read` e acesso à conversa.
// @Tags			Carteira de clientes
// @Produce		json
// @Param			entryType	path		string	true	"Canal da conversa"	Enums(whatsapp, unofficial_whatsapp, telegram, instagram, facebook, webchat, email)
// @Param			entryId		path		string	true	"ID da conversa (UUID)"
// @Success		200			{object}	customerportfoliohttp.HoldingResponse
// @Failure		400			{object}	response.ErrorResponse	"customer_portfolio_subject_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Failure		404			{object}	response.ErrorResponse	"customer_portfolio_subject_not_found"
// @Security		BearerAuth
// @Router			/customer-portfolio/conversations/{entryType}/{entryId} [get]
func (h *Handler) ForConversation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	ref, ok := conversationRef(w, r)
	if !ok {
		return
	}
	holding, err := h.service.ForConversation(r.Context(), a, ref)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHolding(holding))
}

// @Summary		Colocar o cliente de uma conversa numa carteira
// @Description	Coloca o cliente da conversa (o lead; sem lead, o contato do canal, como o perfil @ do Instagram) na carteira de quem chama, ou na de `holderId`. A partir daí, toda conversa nova desse cliente vai direto para o dono da carteira, mesmo off-line, em vez de passar pela roleta; quando o canal tem IA ou fluxo, eles atendem primeiro e o repasse para humano vai para o dono da carteira. Conversas que já têm responsável não mudam. Colocar na própria carteira exige `customer_portfolios:use` e acesso à conversa; colocar na carteira de outro membro exige `customer_portfolios:manage`, o membro precisa estar entre as pessoas que quem chama pode ver (403 `customer_portfolio_holder_out_of_reach`) e poder ter carteira (409 `customer_portfolio_holder_ineligible`). Um cliente fica em no máximo uma carteira: se já está na de outro membro, 409 `customer_portfolio_held_by_other` com `expected.entryId`, `expected.holderId` e `expected.holderName` (quem gerencia pode então mover com PUT /customer-portfolio/entries/{entryId}). Repetir a carteira atual não muda nada.
// @Tags			Carteira de clientes
// @Accept			json
// @Produce		json
// @Param			entryType	path		string			true	"Canal da conversa"	Enums(whatsapp, unofficial_whatsapp, telegram, instagram, facebook, webchat, email)
// @Param			entryId		path		string			true	"ID da conversa (UUID)"
// @Param			request		body		PlaceRequest	true	"Dono da carteira (vazio para a própria)"
// @Success		200			{object}	customerportfoliohttp.HoldingResponse
// @Failure		400			{object}	response.ErrorResponse	"invalid_body, customer_portfolio_subject_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden, customer_portfolio_holder_out_of_reach"
// @Failure		404			{object}	response.ErrorResponse	"customer_portfolio_subject_not_found"
// @Failure		409			{object}	response.ErrorResponse	"customer_portfolio_held_by_other, customer_portfolio_holder_ineligible"
// @Security		BearerAuth
// @Router			/customer-portfolio/conversations/{entryType}/{entryId} [post]
func (h *Handler) PlaceConversation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	ref, ok := conversationRef(w, r)
	if !ok {
		return
	}
	var req PlaceRequest
	if !httpx.DecodeOptionalStrictJSON(w, r, &req) {
		return
	}
	holding, err := h.service.PlaceConversation(r.Context(), a, ref, req.HolderID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHolding(holding))
}

// @Summary		Ver de quem é um lead
// @Description	Diz se o lead está numa carteira e de quem; `entry` é null quando ele não está em nenhuma. Exige `customer_portfolios:read` e `leads:read`.
// @Tags			Carteira de clientes
// @Produce		json
// @Param			leadId	path		string	true	"ID do lead (UUID)"
// @Success		200		{object}	customerportfoliohttp.HoldingResponse
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/customer-portfolio/leads/{leadId} [get]
func (h *Handler) ForLead(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	holding, err := h.service.ForLead(r.Context(), a, mux.Vars(r)["leadId"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHolding(holding))
}

// @Summary		Colocar um lead numa carteira
// @Description	Coloca o lead na carteira de quem chama, ou na de `holderId`, com as mesmas regras de POST /customer-portfolio/conversations/{entryType}/{entryId}; exige também `leads:read`. As conversas novas desse lead em qualquer canal ligado a ele vão para o dono da carteira. Um lead de outro workspace ou anonimizado responde 404 `customer_portfolio_subject_not_found`.
// @Tags			Carteira de clientes
// @Accept			json
// @Produce		json
// @Param			leadId	path		string			true	"ID do lead (UUID)"
// @Param			request	body		PlaceRequest	true	"Dono da carteira (vazio para a própria)"
// @Success		200		{object}	customerportfoliohttp.HoldingResponse
// @Failure		400		{object}	response.ErrorResponse	"invalid_body"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, customer_portfolio_holder_out_of_reach"
// @Failure		404		{object}	response.ErrorResponse	"customer_portfolio_subject_not_found"
// @Failure		409		{object}	response.ErrorResponse	"customer_portfolio_held_by_other, customer_portfolio_holder_ineligible"
// @Security		BearerAuth
// @Router			/customer-portfolio/leads/{leadId} [post]
func (h *Handler) PlaceLead(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req PlaceRequest
	if !httpx.DecodeOptionalStrictJSON(w, r, &req) {
		return
	}
	holding, err := h.service.PlaceLead(r.Context(), a, mux.Vars(r)["leadId"], req.HolderID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHolding(holding))
}

// @Summary		Mover um cliente para outra carteira
// @Description	Passa o cliente para a carteira de `holderId`. Exige `customer_portfolios:manage`; o novo dono precisa estar entre as pessoas que quem chama pode ver (403 `customer_portfolio_holder_out_of_reach`) e poder ter carteira (409 `customer_portfolio_holder_ineligible`). Conversas que já têm responsável não mudam; a próxima conversa nova vai para o novo dono.
// @Tags			Carteira de clientes
// @Accept			json
// @Produce		json
// @Param			entryId	path		string		true	"ID do item da carteira (UUID)"
// @Param			request	body		MoveRequest	true	"Novo dono da carteira"
// @Success		200		{object}	customerportfoliohttp.HoldingResponse
// @Failure		400		{object}	response.ErrorResponse	"invalid_body, customer_portfolio_holder_required"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, customer_portfolio_holder_out_of_reach"
// @Failure		404		{object}	response.ErrorResponse	"customer_portfolio_entry_not_found"
// @Failure		409		{object}	response.ErrorResponse	"customer_portfolio_holder_ineligible"
// @Security		BearerAuth
// @Router			/customer-portfolio/entries/{entryId} [put]
func (h *Handler) Move(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req MoveRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.HolderID) == "" {
		writeError(w, cp.ErrHolderRequired)
		return
	}
	holding, err := h.service.Move(r.Context(), a, mux.Vars(r)["entryId"], req.HolderID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHolding(holding))
}

// @Summary		Tirar um cliente da carteira
// @Description	Tira o cliente da carteira; as conversas novas dele voltam para a roleta. O dono tira da própria carteira com `customer_portfolios:use`; tirar da carteira de outro membro exige `customer_portfolios:manage`.
// @Tags			Carteira de clientes
// @Param			entryId	path	string	true	"ID do item da carteira (UUID)"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"customer_portfolio_entry_not_found"
// @Security		BearerAuth
// @Router			/customer-portfolio/entries/{entryId} [delete]
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.Remove(r.Context(), a, mux.Vars(r)["entryId"]); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
