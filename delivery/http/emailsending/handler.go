package emailsending

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

const maxRequestBody = 16 << 10

type SetupService interface {
	Status(ctx context.Context, workspaceID string) (esuc.StatusView, error)
	Provision(ctx context.Context, workspaceID, slug string) (esuc.StatusView, error)
	CheckSubdomain(ctx context.Context, workspaceID, slug string) (esuc.SubdomainCheck, error)
	ChangeSubdomain(ctx context.Context, workspaceID, slug string) (esuc.StatusView, error)
}

type DomainService interface {
	Get(ctx context.Context, workspaceID, id string) (*es.SendingDomain, error)
	Connect(ctx context.Context, workspaceID, rawDomain string) (*es.SendingDomain, error)
	Recheck(ctx context.Context, workspaceID, id string) (*es.SendingDomain, error)
	Remove(ctx context.Context, workspaceID, id string) error
}

type SenderService interface {
	List(ctx context.Context, workspaceID string) ([]esuc.SenderView, error)
	Create(ctx context.Context, workspaceID string, in esuc.CreateSenderInput) (esuc.SenderView, error)
	Update(ctx context.Context, workspaceID, id string, in esuc.UpdateSenderInput) (esuc.SenderView, error)
	Delete(ctx context.Context, workspaceID, id string) error
}

type SuppressionService interface {
	List(ctx context.Context, workspaceID, search string, offset, limit int) ([]es.Suppression, int64, error)
	Add(ctx context.Context, workspaceID, rawAddress string) (es.Suppression, error)
	Remove(ctx context.Context, workspaceID, rawAddress string) error
}

type Handler struct {
	setup        SetupService
	domains      DomainService
	senders      SenderService
	suppressions SuppressionService
}

func NewHandler(setup SetupService, domains DomainService, senders SenderService, suppressions SuppressionService) *Handler {
	if setup == nil || domains == nil || senders == nil || suppressions == nil {
		panic("email http: setup, domains, senders and suppressions are required")
	}
	return &Handler{setup: setup, domains: domains, senders: senders, suppressions: suppressions}
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(into); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return false
	}
	return true
}

// @Summary		Consultar a configuração de envio de e-mail
// @Description	Retorna a conta de envio do workspace (modo e situação), o progresso do período de avaliação com as metas para sair dele (`targets`), os domínios com os registros DNS (nome relativo em `host` e a situação de cada registro em `status`) e o uso do dia contra o limite diário, e os remetentes. Antes da ativação responde com `provisioned` falso.
// @Description	`setup` (na conta e em cada domínio) diz como anda a configuração no provedor: `in_progress` enquanto ela roda em segundo plano, `failed` com o motivo em `error_code` (`email_provider_unavailable` ou `email_reply_capacity_reached`) e `done` quando não há nada a configurar. Consulte de novo até sair de `in_progress`.
// @Description	`lifecycle` diz em que fase o domínio está: `active` em uso, `releasing` enquanto a rota de respostas, os registros DNS e a identidade no provedor são removidos em segundo plano (depois de trocar o endereço ou de liberar o nome do período de avaliação). A lista traz só domínios `active` e `releasing`; um domínio já removido sai dela. `actions` diz o que dá para fazer com cada domínio (`recheck` e `remove`); um domínio em `releasing` não oferece nenhuma ação e não aceita remetentes. Cada remetente traz em `domain_lifecycle` a fase do domínio dele, para avisar quando o domínio está sendo removido (`releasing`) ou já foi (`retired`).
// @Description	`activation` diz antes do clique se o workspace pode ativar o envio, trocar o subdomínio ou conectar um domínio próprio: `allowed` verdadeiro com plano pago ativo; senão `error_code` traz `email_plan_required` (sem plano pago ativo) ou `email_plan_unavailable` (o plano não pôde ser lido agora). Uma conta já ativa não é desfeita quando o plano acaba.
// @Tags			E-mail
// @Produce		json
// @Success		200	{object}	SetupResponse
// @Failure		403	{object}	response.ErrorResponse	"Workspace obrigatório"
// @Security		BearerAuth
// @Router			/email/setup [get]
func (h *Handler) GetSetup(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	view, err := h.setup.Status(r.Context(), ws)
	if err != nil {
		writeError(w, err, "setup status")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSetup(view))
}

// @Summary		Ativar o envio de e-mail
// @Description	Cria a conta de envio do workspace e o subdomínio compartilhado do período de avaliação e responde na hora, com `setup.state` em `in_progress`. A configuração no provedor (conta, identidade, registros DNS e rota de respostas) roda em segundo plano logo em seguida; consulte `GET /email/setup` até `setup.state` sair de `in_progress`. Se falhar, `setup.state` fica `failed` com o motivo em `setup.error_code`. Pode ser repetido: cada chamada recomeça a configuração pendente, sem duplicar nada. Depois o domínio fica em verificação até o provedor confirmar. Exige plano pago ativo (`activation.allowed` de `GET /email/setup` diz antes); sem ele responde `email_plan_required`; uma conta já ativa não é desfeita quando o plano acaba.
// @Description	O corpo é opcional: `slug` escolhe o nome do subdomínio de envio (o endereço fica `contato@<slug>.<domínio compartilhado>`). Sem ele, vale o nome sugerido em `subdomain.slug` de `GET /email/setup`. O nome precisa vir na forma final (minúsculas, números e hífens), não pode ser reservado nem estar em uso por outro workspace. Ativar de novo com outro nome troca o subdomínio enquanto nada foi enviado por ele.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			body	body		ProvisionRequest	false	"Nome do subdomínio"
// @Success		200		{object}	SetupResponse
// @Failure		409		{object}	response.ErrorResponse	"email_domain_taken, email_subdomain_taken ou email_subdomain_locked"
// @Failure		422		{object}	response.ErrorResponse	"email_setup_invalid, email_subdomain_invalid ou email_subdomain_reserved"
// @Failure		402		{object}	response.ErrorResponse	"email_plan_required: o workspace não tem plano pago ativo"
// @Failure		503		{object}	response.ErrorResponse	"email_plan_unavailable: o plano não pôde ser lido"
// @Security		BearerAuth
// @Router			/email/setup/provision [post]
func (h *Handler) Provision(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req ProvisionRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	view, err := h.setup.Provision(r.Context(), ws, req.Slug)
	if err != nil {
		writeError(w, err, "provision")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSetup(view))
}

// @Summary		Conferir um nome de subdomínio de envio
// @Description	Diz se o nome pode ser usado como subdomínio de envio deste workspace, sem reservar nada. `available` falso traz o motivo em `error_code` (`email_subdomain_invalid`, `email_subdomain_reserved` ou `email_subdomain_taken`). `address` mostra o endereço padrão no nível atual da conta (avaliação ou confiável) sempre que o nome é válido.
// @Tags			E-mail
// @Produce		json
// @Param			slug	query		string	true	"Nome do subdomínio"
// @Success		200		{object}	SubdomainCheckResponse
// @Security		BearerAuth
// @Router			/email/setup/subdomain/check [get]
func (h *Handler) CheckSubdomain(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	check, err := h.setup.CheckSubdomain(r.Context(), ws, r.URL.Query().Get("slug"))
	if err != nil {
		writeError(w, err, "check subdomain")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSubdomainCheck(check))
}

// @Summary		Trocar o subdomínio de envio
// @Description	Troca o nome do subdomínio compartilhado enquanto nenhum e-mail saiu por ele; depois do primeiro envio responde `email_subdomain_locked` (a reputação fica com o nome; para controle total, conecte um domínio próprio). Responde na hora com o novo domínio em configuração (`setup.state` em `in_progress`); os remetentes passam para o novo nome, e a identidade, os registros DNS e a rota de respostas do nome antigo são removidos em segundo plano.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			body	body		SubdomainRequest	true	"Novo nome"
// @Success		200		{object}	SetupResponse
// @Failure		409		{object}	response.ErrorResponse	"email_subdomain_taken, email_subdomain_locked, email_domain_taken ou email_account_not_provisioned"
// @Failure		422		{object}	response.ErrorResponse	"email_subdomain_invalid ou email_subdomain_reserved"
// @Failure		402		{object}	response.ErrorResponse	"email_plan_required: o workspace não tem plano pago ativo"
// @Failure		503		{object}	response.ErrorResponse	"email_plan_unavailable: o plano não pôde ser lido"
// @Security		BearerAuth
// @Router			/email/setup/subdomain [put]
func (h *Handler) ChangeSubdomain(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req SubdomainRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := h.setup.ChangeSubdomain(r.Context(), ws, req.Slug)
	if err != nil {
		writeError(w, err, "change subdomain")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSetup(view))
}

// @Summary		Conectar domínio próprio
// @Description	Conecta um domínio da empresa para enviar e-mails e responde na hora com o domínio e `setup.state` em `in_progress`. A configuração no provedor roda em segundo plano logo em seguida e gera os registros DNS que o cliente cria no provedor dele (nome relativo em `host`, tipo, valor e a situação de cada registro); consulte `GET /email/domains/{id}` até `setup.state` sair de `in_progress`. Se falhar, `setup.state` fica `failed` com o motivo em `setup.error_code`, e conectar de novo recomeça a configuração. A verificação acontece em "verificar de novo" e na varredura periódica, sempre com a confirmação do provedor de e-mail. Recusa domínios de e-mail gratuito, os domínios compartilhados da plataforma e domínios de outro workspace.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			body	body		ConnectDomainRequest	true	"Domínio"
// @Success		201		{object}	DomainResponse
// @Failure		409		{object}	response.ErrorResponse	"email_domain_taken"
// @Failure		422		{object}	response.ErrorResponse	"email_domain_invalid, email_domain_free_mailbox ou email_domain_shared"
// @Failure		402		{object}	response.ErrorResponse	"email_plan_required: o workspace não tem plano pago ativo"
// @Failure		503		{object}	response.ErrorResponse	"email_plan_unavailable: o plano não pôde ser lido"
// @Security		BearerAuth
// @Router			/email/domains [post]
func (h *Handler) ConnectDomain(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req ConnectDomainRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := h.domains.Connect(r.Context(), ws, req.Domain)
	if err != nil {
		writeError(w, err, "connect domain")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentDomain(*d))
}

// @Summary		Detalhar domínio de envio
// @Tags			E-mail
// @Produce		json
// @Param			id	path		string	true	"ID do domínio"
// @Success		200	{object}	DomainResponse
// @Failure		404	{object}	response.ErrorResponse	"email_domain_not_found"
// @Security		BearerAuth
// @Router			/email/domains/{id} [get]
func (h *Handler) GetDomain(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	d, err := h.domains.Get(r.Context(), ws, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "get domain")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentDomain(*d))
}

// @Summary		Verificar domínio de novo
// @Description	Consulta o provedor e atualiza a situação do domínio e de cada registro DNS: DKIM e MAIL FROM pela confirmação do provedor, MX de respostas e DMARC por consulta DNS (`unknown` quando a consulta falha). A verificação do domínio só vale com a confirmação do provedor. Um domínio cuja configuração ainda está pendente (`setup.state` em `in_progress` ou `failed`) não é consultado: a configuração recomeça em segundo plano e a resposta traz `setup.state` em `in_progress`. Um domínio em `releasing` ou já removido não é consultado e volta como está.
// @Tags			E-mail
// @Produce		json
// @Param			id	path		string	true	"ID do domínio"
// @Success		200	{object}	DomainResponse
// @Failure		404	{object}	response.ErrorResponse	"email_domain_not_found"
// @Failure		503	{object}	response.ErrorResponse	"email_provider_unavailable"
// @Security		BearerAuth
// @Router			/email/domains/{id}/recheck [post]
func (h *Handler) RecheckDomain(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	d, err := h.domains.Recheck(r.Context(), ws, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "recheck domain")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentDomain(*d))
}

// @Summary		Remover domínio próprio
// @Description	Remove um domínio próprio sem remetentes. O domínio compartilhado não pode ser removido.
// @Tags			E-mail
// @Param			id	path	string	true	"ID do domínio"
// @Success		204
// @Failure		409	{object}	response.ErrorResponse	"email_domain_in_use ou email_domain_shared_removal"
// @Security		BearerAuth
// @Router			/email/domains/{id} [delete]
func (h *Handler) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.domains.Remove(r.Context(), ws, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "remove domain")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Listar remetentes
// @Description	Cada remetente traz em `domain_lifecycle` a fase do domínio dele: `active`, `releasing` enquanto o domínio é removido do provedor, ou `retired` quando ele já foi removido.
// @Tags			E-mail
// @Produce		json
// @Success		200	{array}	SenderResponse
// @Security		BearerAuth
// @Router			/email/senders [get]
func (h *Handler) ListSenders(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	views, err := h.senders.List(r.Context(), ws)
	if err != nil {
		writeError(w, err, "list senders")
		return
	}
	out := make([]SenderResponse, 0, len(views))
	for _, v := range views {
		out = append(out, presentSender(v))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar remetente
// @Description	Cria um remetente num domínio verificado do workspace. O primeiro remetente vira o padrão. Um domínio que está sendo removido responde `email_domain_releasing`.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			body	body		CreateSenderRequest	true	"Remetente"
// @Success		201		{object}	SenderResponse
// @Failure		409		{object}	response.ErrorResponse	"email_domain_not_verified, email_domain_releasing ou email_sender_exists"
// @Failure		422		{object}	response.ErrorResponse	"email_sender_invalid_local_part, email_sender_invalid_display_name ou email_sender_invalid_forward"
// @Security		BearerAuth
// @Router			/email/senders [post]
func (h *Handler) CreateSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req CreateSenderRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := h.senders.Create(r.Context(), ws, esuc.CreateSenderInput{
		DomainID: req.DomainID, LocalPart: req.LocalPart, DisplayName: req.DisplayName, ReplyForwardTo: req.ReplyForwardTo, IsDefault: req.IsDefault,
	})
	if err != nil {
		writeError(w, err, "create sender")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentSender(v))
}

// @Summary		Atualizar remetente
// @Description	Recusa com `email_domain_releasing` quando o domínio do remetente (o atual ou o novo) está sendo removido; nesse caso o remetente só pode ser removido.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do remetente"
// @Param			body	body		UpdateSenderRequest	true	"Campos a alterar"
// @Success		200		{object}	SenderResponse
// @Failure		404		{object}	response.ErrorResponse	"email_sender_not_found"
// @Failure		409		{object}	response.ErrorResponse	"email_sender_default_required, email_domain_not_verified, email_domain_releasing ou email_sender_exists"
// @Security		BearerAuth
// @Router			/email/senders/{id} [patch]
func (h *Handler) UpdateSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req UpdateSenderRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := h.senders.Update(r.Context(), ws, mux.Vars(r)["id"], esuc.UpdateSenderInput{
		DomainID: req.DomainID, LocalPart: req.LocalPart, DisplayName: req.DisplayName, ReplyForwardTo: req.ReplyForwardTo, IsDefault: req.IsDefault,
	})
	if err != nil {
		writeError(w, err, "update sender")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSender(v))
}

// @Summary		Remover remetente
// @Tags			E-mail
// @Param			id	path	string	true	"ID do remetente"
// @Success		204
// @Failure		409	{object}	response.ErrorResponse	"email_sender_default_required"
// @Security		BearerAuth
// @Router			/email/senders/{id} [delete]
func (h *Handler) DeleteSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.senders.Delete(r.Context(), ws, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "delete sender")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Listar endereços bloqueados
// @Description	Lista paginada da lista de bloqueio do workspace, das entradas mais novas para as mais antigas. Com `q`, mostra só os endereços que começam pelo texto informado (sem diferença de maiúsculas).
// @Tags			E-mail
// @Produce		json
// @Param			q			query		string	false	"Início do endereço"
// @Param			page		query		int	false	"Página"
// @Param			pageSize	query		int	false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload
// @Security		BearerAuth
// @Router			/email/suppressions [get]
func (h *Handler) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	p := httpx.ParsePagination(r.URL.Query())
	rows, total, err := h.suppressions.List(r.Context(), ws, r.URL.Query().Get("q"), (p.Page-1)*p.PageSize, p.PageSize)
	if err != nil {
		writeError(w, err, "list suppressions")
		return
	}
	out := make([]SuppressionResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, presentSuppression(s))
	}
	pages := int((total + int64(p.PageSize) - 1) / int64(p.PageSize))
	response.WritePaginated(w, http.StatusOK, out, response.PaginationMeta{Page: p.Page, PageSize: p.PageSize, TotalPages: pages, TotalItems: total})
}

// @Summary		Bloquear endereço
// @Description	Coloca um endereço na lista de bloqueio do workspace. Nenhum e-mail sai para ele.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			body	body		SuppressionRequest	true	"Endereço"
// @Success		201		{object}	SuppressionResponse
// @Failure		422		{object}	response.ErrorResponse	"email_address_invalid"
// @Security		BearerAuth
// @Router			/email/suppressions [post]
func (h *Handler) AddSuppression(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req SuppressionRequest
	if !decode(w, r, &req) {
		return
	}
	s, err := h.suppressions.Add(r.Context(), ws, req.Address)
	if err != nil {
		writeError(w, err, "add suppression")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentSuppression(s))
}

// @Summary		Desbloquear endereço
// @Description	Tira um endereço da lista de bloqueio. Endereços que denunciaram spam ou que voltaram como inexistentes (hard bounce) não saem.
// @Tags			E-mail
// @Param			address	path	string	true	"Endereço de e-mail"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse	"email_suppression_not_found"
// @Failure		409	{object}	response.ErrorResponse	"email_suppression_locked"
// @Security		BearerAuth
// @Router			/email/suppressions/{address} [delete]
func (h *Handler) RemoveSuppression(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.suppressions.Remove(r.Context(), ws, mux.Vars(r)["address"]); err != nil {
		writeError(w, err, "remove suppression")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
