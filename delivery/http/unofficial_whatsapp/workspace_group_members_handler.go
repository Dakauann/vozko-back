package unofficial_whatsapp

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

// @Summary		Membros de um grupo
// @Description	Membros do grupo com o lead vinculado e em quantos outros grupos acompanhados cada um está. Membros com o telefone oculto pelo WhatsApp aparecem sem telefone.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id			path		string	true	"Grupo"
// @Param			status		query		string	false	"active, left (saiu, por conta própria ou sem autor conhecido) ou removed (removido por um admin)"
// @Param			adminsOnly	query		bool	false	"Somente admins"
// @Param			search		query		string	false	"Nome ou telefone"
// @Param			leadLinked	query		bool	false	"Com ou sem lead"
// @Param			inOtherOnly	query		bool	false	"Somente quem está em outros grupos"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]unofficial_whatsapp_group.MemberRow}
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/members [get]
func (h *WorkspaceGroupHandler) Members(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	status := uwg.MemberStatus(strings.TrimSpace(values.Get("status")))
	if status != "" && !status.Valid() {
		response.WriteError(w, http.StatusBadRequest, "status must be active, left or removed", nil)
		return
	}
	page, err := h.groups.ListMembers(r.Context(), a, mux.Vars(r)["id"], uwg.MemberFilter{
		Status:      status,
		AdminsOnly:  isSet(values, "adminsOnly"),
		Search:      strings.TrimSpace(values.Get("search")),
		LeadLinked:  httpx.ParseBoolQuery(values.Get("leadLinked")),
		InOtherOnly: isSet(values, "inOtherOnly"),
		Pagination:  httpx.ParsePagination(values),
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeGroupPage(w, page)
}

// @Summary		Exportar os membros de um grupo
// @Description	A mesma planilha CSV da exportação de vários grupos (`/unofficial-whatsapp/groups/members/export`), só com este grupo.
// @Tags			Unofficial WhatsApp Groups
// @Produce		text/csv
// @Param			id	path		string	true	"Grupo"
// @Success		200	{string}	string	"arquivo CSV"
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/members/export [get]
func (h *WorkspaceGroupHandler) ExportMembers(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	export, err := h.groups.ExportMembers(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeMemberExport(w, export)
}

// @Summary		Exportar os membros de vários grupos
// @Description	Uma única planilha CSV (separada por ponto e vírgula, com BOM) com os membros dos grupos escolhidos e/ou de uma série inteira: grupo, JID do grupo, nossos números no grupo, nome, telefone, JID, número oculto (LID), status (ativo, saiu, removido), papel (superadmin, admin, membro), datas no fuso do workspace, lead, outros grupos e qual dos nossos números é o membro. Um grupo fora do escopo de quem pede recusa a exportação inteira.
// @Tags			Unofficial WhatsApp Groups
// @Produce		text/csv
// @Param			groupIds	query		string	false	"IDs dos grupos separados por vírgula"
// @Param			seriesId	query		string	false	"Série: exporta todos os grupos dela"
// @Success		200			{string}	string	"arquivo CSV"
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/members/export [get]
func (h *WorkspaceGroupHandler) ExportGroupMembers(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	export, err := h.groups.ExportGroupMembers(r.Context(), a, uwguc.MemberExportInput{
		GroupIDs: listQuery(values, "groupIds"),
		SeriesID: strings.TrimSpace(values.Get("seriesId")),
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeMemberExport(w, export)
}

func writeMemberExport(w http.ResponseWriter, export *uwguc.MemberExport) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, export.FileName))
	if _, err := io.WriteString(w, export.Document); err != nil {
		log.Printf("[groups] member export not fully written: %v", err)
	}
}

func listQuery(values url.Values, key string) []string {
	var out []string
	for _, raw := range values[key] {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// @Summary		Cadastrar membros como leads
// @Description	Cria ou reaproveita um lead pelo telefone de cada membro escolhido e vincula o membro a ele. Membros com o telefone oculto não viram lead.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Grupo"
// @Param			body	body		membersToLeadsRequest	true	"Membros"
// @Success		200		{object}	unofficial_whatsapp_group_usecase.LeadLinkResult
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/members/leads [post]
func (h *WorkspaceGroupHandler) MembersToLeads(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req membersToLeadsRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	if len(req.PersonKeys) == 0 {
		response.WriteError(w, http.StatusBadRequest, "choose at least one member", nil)
		return
	}
	result, err := h.groups.AddMembersToLeads(r.Context(), a, mux.Vars(r)["id"], req.PersonKeys)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary		Cadastrar pessoas dos grupos como leads
// @Description	Para cada pessoa escolhida na lista de pessoas dos grupos, cria ou reaproveita um lead pelo telefone e vincula a ele todas as participações dela nos grupos acompanhados do escopo de quem pede. Pessoas com o telefone oculto não viram lead.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			body	body		membersToLeadsRequest	true	"Pessoas"
// @Success		200		{object}	unofficial_whatsapp_group_usecase.LeadLinkResult
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/people/leads [post]
func (h *WorkspaceGroupHandler) PeopleToLeads(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req membersToLeadsRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	if len(req.PersonKeys) == 0 {
		response.WriteError(w, http.StatusBadRequest, "choose at least one person", nil)
		return
	}
	result, err := h.groups.AddPeopleToLeads(r.Context(), a, req.PersonKeys)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary		Gerenciar membros de um grupo
// @Description	Adiciona (`add`), promove a admin (`promote`) ou rebaixa (`demote`) membros pelo POST, e remove (`remove`) pelo DELETE, sempre por um número admin do escopo de quem pede. `participants` aceita telefones com DDI ou JIDs. Depois da mudança a lista de membros é relida e as entradas e saídas ficam no histórico.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Grupo"
// @Param			body	body		manageMembersRequest	true	"Ação e membros"
// @Success		200		{object}	unofficial_whatsapp_group.Group
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/participants [post]
// @Router			/unofficial-whatsapp/groups/{id}/participants [delete]
func (h *WorkspaceGroupHandler) ManageMembers(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req manageMembersRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	action := uw.GroupAction(strings.TrimSpace(req.Action))
	if action.Destructive() != (r.Method == http.MethodDelete) {
		response.WriteError(w, http.StatusBadRequest, "use DELETE to remove members and POST to add, promote or demote", nil)
		return
	}
	g, err := h.groups.ManageMembers(r.Context(), a, mux.Vars(r)["id"], action, req.Participants)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, g)
}

// @Summary		Campanhas de um grupo
// @Description	Campanhas por QR Code que enviaram ou vão enviar para este grupo, com o status do envio ao grupo.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo"
// @Success		200	{array}		unofficial_whatsapp_group.GroupCampaign
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/campaigns [get]
func (h *WorkspaceGroupHandler) Campaigns(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	campaigns, err := h.groups.GroupCampaigns(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	if campaigns == nil {
		campaigns = []uwg.GroupCampaign{}
	}
	response.WriteSuccess(w, http.StatusOK, campaigns)
}

// @Summary		Link de convite de um grupo
// @Description	Link de convite lido no WhatsApp por um número admin do escopo.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo"
// @Success		200	{object}	map[string]string
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/invite-link [get]
func (h *WorkspaceGroupHandler) InviteLink(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	link, err := h.groups.InviteLink(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"inviteLink": link})
}

// @Summary		Sair de um grupo
// @Description	Os números escolhidos, todos do escopo de quem pede, saem do grupo no WhatsApp.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"Grupo"
// @Param			body	body		leaveWorkspaceGroupRequest	true	"Números"
// @Success		200		{object}	map[string]bool
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/leave [post]
func (h *WorkspaceGroupHandler) Leave(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req leaveWorkspaceGroupRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	if err := h.groups.LeaveGroup(r.Context(), a, mux.Vars(r)["id"], req.InstanceIDs); err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"left": true})
}

// @Summary		Criar um grupo
// @Description	O primeiro número escolhido cria o grupo no WhatsApp, os outros entram como participantes e viram admins. O grupo nasce acompanhado, a não ser com `skipFollow`.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			body	body		createWorkspaceGroupRequest	true	"Grupo"
// @Success		201		{object}	unofficial_whatsapp_group_usecase.ManagedGroupResult
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups [post]
func (h *WorkspaceGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req createWorkspaceGroupRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	result, err := h.groups.CreateManagedGroup(r.Context(), a, uwguc.ManagedGroupInput{
		Name: req.Name, Description: req.Description, Category: req.Category,
		InstanceIDs: req.InstanceIDs, AdminsOnly: req.AdminsOnly, SkipFollow: req.SkipFollow,
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, result)
}

// @Summary		Conferir um link de convite
// @Description	Lê no WhatsApp o grupo do convite (nome, descrição, membros) e diz quais números do escopo já estão nele.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			body	body		inviteRequest	true	"Link"
// @Success		200		{object}	unofficial_whatsapp_group_usecase.InvitePreview
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/invites/preview [post]
func (h *WorkspaceGroupHandler) PreviewInvite(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req inviteRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	preview, err := h.groups.PreviewInvite(r.Context(), a, req.InviteLink)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, preview)
}

// @Summary		Entrar em um grupo
// @Description	O primeiro número entra na hora; os outros entram um a cada 10 minutos, para não parecer automação ao WhatsApp. Um convite revogado ou inválido para o plano inteiro; falhas passageiras tentam de novo até 3 vezes. Com `category` o grupo fica nessa categoria (até 60 caracteres): a primeira entrada do plano define a categoria, as seguintes só preenchem um grupo ainda sem categoria.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			body	body		joinWorkspaceGroupRequest	true	"Convite e números"
// @Success		201		{object}	unofficial_whatsapp_group_usecase.JoinResult
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/joins [post]
func (h *WorkspaceGroupHandler) Join(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req joinWorkspaceGroupRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	result, err := h.groups.JoinGroup(r.Context(), a, uwguc.JoinInput{InviteLink: req.InviteLink, InstanceIDs: req.InstanceIDs, Follow: req.Follow, Category: req.Category})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, result)
}

// @Summary		Adicionar números a um grupo
// @Description	Coloca no grupo números seus que ainda não estão nele, pela mesma fila de entradas: o primeiro entra na hora e os outros um a cada 10 minutos. O convite vem de um número seu que é admin do grupo; sem admin, usa o convite de uma entrada anterior do grupo. Sem nenhum dos dois o pedido é recusado e o grupo precisa ser aberto com um convite. Categoria e acompanhamento do grupo continuam como estão.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do grupo"
// @Param			body	body		addGroupNumbersRequest	true	"Números que vão entrar"
// @Success		201		{object}	unofficial_whatsapp_group_usecase.JoinResult
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/numbers [post]
func (h *WorkspaceGroupHandler) AddNumbers(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req addGroupNumbersRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	result, err := h.groups.AddNumbers(r.Context(), a, mux.Vars(r)["id"], req.InstanceIDs)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, result)
}

// @Summary		Entradas em grupos
// @Description	Entradas planejadas e recentes dos números do escopo, com status, tentativas e o horário previsto.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Success		200	{array}		unofficial_whatsapp_group.JoinTask
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/joins [get]
func (h *WorkspaceGroupHandler) Joins(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	tasks, err := h.groups.Joins(r.Context(), a)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	if tasks == nil {
		tasks = []uwg.JoinTask{}
	}
	response.WriteSuccess(w, http.StatusOK, tasks)
}

// @Summary		Cancelar uma entrada planejada
// @Description	Cancela as entradas do plano que ainda não aconteceram.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			planId	path		string	true	"Plano de entrada"
// @Success		200		{object}	map[string]int64
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/joins/{planId}/cancel [post]
func (h *WorkspaceGroupHandler) CancelJoin(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	cancelled, err := h.groups.CancelJoin(r.Context(), a, mux.Vars(r)["planId"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]int64{"cancelled": cancelled})
}
