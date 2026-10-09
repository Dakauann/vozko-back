package lead

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/address"
	"vozko/domain/customfield"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	lead_usecase "vozko/usecases/lead"
)

func (h *LeadHandler) commandsReady(w http.ResponseWriter) bool {
	if h.commands == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_commands_unavailable", "A edição de leads não está disponível neste servidor", nil)
		return false
	}
	return true
}

func (h *LeadHandler) commandActor(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, bool) {
	if !h.commandsReady(w) {
		return lead_usecase.Actor{}, false
	}
	return h.requestActor(w, r)
}

func (h *LeadHandler) writeCommandError(w http.ResponseWriter, err error) {
	var conflict *leaddomain.VersionConflict
	switch {
	case errors.As(err, &conflict):
		httpx.WriteVersionConflict(w, "O lead foi alterado por outra pessoa ou em outra aba", toLeadRecord(conflict.Current, h.now()))
	case errors.Is(err, shared.ErrVersionRequired):
		httpx.WriteVersionRequired(w)
	case errors.Is(err, leaddomain.ErrLeadForbidden), errors.Is(err, leaddomain.ErrLeadOwnerOutOfReach), errors.Is(err, leaddomain.ErrAddressesForbidden),
		errors.Is(err, customfield.ErrValueForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, leaddomain.ErrorCode(err), err.Error(), refusalDetails(err))
	case errors.Is(err, leaddomain.ErrLeadNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, leaddomain.ErrorCode(err), "Lead not found", nil)
	case errors.Is(err, leaddomain.ErrRelativeNotFound), errors.Is(err, leaddomain.ErrRelationOtherWorkspace), errors.Is(err, leaddomain.ErrRelationNotFound),
		errors.Is(err, leaddomain.ErrLocationNotFound), errors.Is(err, leaddomain.ErrAddressNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, leaddomain.ErrorCode(err), err.Error(), nil)
	case errors.Is(err, leaddomain.ErrLeadDuplicate):
		response.WriteErrorWithCode(w, http.StatusConflict, leaddomain.ErrorCode(err), "Já existe um lead com este número", identityHolderDetails(err))
	case errors.Is(err, leaddomain.ErrIdentityInUse), errors.Is(err, leaddomain.ErrRelationExists):
		response.WriteErrorWithCode(w, http.StatusConflict, leaddomain.ErrorCode(err), err.Error(), nil)
	case leaddomain.IsInputRefusal(err):
		response.WriteErrorWithCode(w, http.StatusBadRequest, leaddomain.ErrorCode(err), err.Error(), refusalDetails(err))
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to change the lead", nil)
	}
}

// @Summary		Cadastrar um lead
// @Description	Cria um lead à mão. O número é opcional: um lead pode existir só com o nome (um familiar, alguém conhecido pessoalmente). Quando informado, o número é normalizado para o formato brasileiro e não pode pertencer a outro lead do workspace (409 `lead_identity_taken`). O nome é marcado como definido pela equipe, então nenhuma importação ou canal o substitui depois. Aceita até 6 telefones de contato (`phones`, rótulos `mobile`, `landline`, `work`, `message`, `other`; não repetem o WhatsApp nem entre si, mas podem ser de outros leads, como o telefone de casa de uma família) e até 5 endereços (`addresses`, rótulos `home`, `work`, `other`, exatamente um `primary` quando há mais de um; cada um precisa de CEP ou de cidade com UF). A posição no mapa é do servidor: todo endereço novo entra como `geoStatus` `pending`, a não ser que traga `pin` {latitude, longitude}, o alfinete que a pessoa posicionou antes de salvar (exige também `leads:update` e `leads:read_addresses`, 403 `forbidden` ou `lead_addresses_forbidden`; o servidor confere o ponto, grava precisão `exact` e origem `manual`, e a geocodificação automática nunca o substitui; um ponto fora do Brasil ou inválido dá 400 `lead_location_invalid`). Um endereço só com `pin` e sem texto é aceito. Se o número já é de outro lead, a resposta é 409 `lead_identity_taken` com `expected.leadId` (só para quem lê leads). Aceita `customFields` (mapa pela chave do campo personalizado de lead; campos obrigatórios são exigidos, um campo sensível só pode ser preenchido por quem tem `leads:read_sensitive`, 403 `custom_field_sensitive_forbidden`; recusas de valor dão 400 com a chave em `expected.key`). A resposta traz em `duplicates` leads que podem ser a mesma pessoa (mesmo telefone, ou mesmo nome no mesmo endereço), um aviso que não impede o cadastro. Uma chave desconhecida no corpo é recusada com 400 `invalid_body`. Códigos de 400: `invalid_body`, `lead_identity_required`, `lead_number_invalid`, `lead_name_too_long`, `lead_nickname_too_long`, `lead_email_invalid`, `lead_birth_date_invalid`, `lead_phone_invalid`, `lead_phone_label_invalid`, `lead_phone_limit`, `lead_phone_repeats_identity`, `lead_phone_repeated`, `lead_address_invalid`, `lead_address_label_invalid`, `lead_address_limit`, `lead_address_primary`; nos itens de lista, `expected` traz `field` e `index` (e, no endereço, `addressField` e `rule`).
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			request	body		lead.CreateLeadRequest	true	"Dados do lead"
// @Success		201		{object}	lead.CreateLeadResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads [post]
func (h *LeadHandler) Create(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req CreateLeadRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	created, err := h.commands.Create(r.Context(), a, req.toDomain())
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, CreateLeadResponse{
		LeadRecordResponse: toLeadRecord(created.Lead, h.now()),
		Duplicates:         duplicateResponses(created.Duplicates),
	})
}

// @Summary		Editar um lead
// @Description	Altera os campos enviados e mantém todos os outros como estão (campos ausentes nunca são apagados; envie "" para limpar um texto). Exige no cabeçalho `If-Match` a `version` lida: sem ela a resposta é 428 `version_required`; se o lead mudou depois da leitura, a resposta é 409 `version_conflict` com o registro atual em `current`, mostrando só o que você pode ler. O corpo aceita só `number`, `name`, `nickname`, `email`, `birthDate`, `whatsappOptIn`, `phones`, `addresses` e `customFields`; qualquer outra chave é recusada com 400 `invalid_body`, que nomeia a chave. `customFields` é um mapa parcial pela chave do campo: só as chaves enviadas mudam, `null` limpa uma chave, e as chaves ausentes (inclusive as sensíveis que você não pode ver) ficam como estão. Cada valor é conferido com o campo personalizado de lead do workspace (400 `custom_field_unknown_key`, `custom_field_value_type`, `custom_field_value_not_in_options`, `custom_field_value_required`, com a chave em `expected.key`); escrever ou limpar um campo sensível exige `leads:read_sensitive` (403 `custom_field_sensitive_forbidden`). Enviar `addresses` exige `leads:read_addresses` (403 `lead_addresses_forbidden`), porque quem não tem essa permissão só vê o bairro e a cidade e substituiria a lista inteira sem ver o que apaga. `phones` e `addresses`, quando enviados, substituem a lista inteira (envie `id` para manter um item; um `id` que não é deste lead é recusado com `lead_phone_unknown` ou `lead_address_unknown`). Mudar o texto de um endereço devolve a posição para `pending`, a não ser que a posição tenha sido marcada à mão e `keepPosition` seja `true`; latitude e longitude nunca vêm do cliente como colunas do endereço; o alfinete posicionado à mão vem em `pin` {latitude, longitude} e é gravado no mesmo comando, com precisão `exact` e origem `manual`, pelas mesmas regras de POST /leads/{id}/addresses/{addressId}/pin (um ponto inválido dá 400 `lead_location_invalid`; a geocodificação automática nunca substitui esse alfinete). Trocar o número de WhatsApp (`number`) só é possível enquanto o lead não tem conversas (409 `lead_identity_in_use`: cadastre o número como telefone de contato); um número de outro lead dá 409 `lead_identity_taken`. O responsável (`ownerId`) muda por POST /leads/{id}/owner e o bloqueio por POST /leads/{id}/block. A resposta e o `current` do 409 mostram só o que você pode ler: campos sensíveis só para `leads:read_sensitive` e endereços completos só para `leads:read_addresses` (sem ela, cada endereço traz só bairro, cidade, UF e o código IBGE da cidade, sem CEP, rua, número, complemento nem posição). Códigos de 400: `invalid_body`, `lead_identity_required`, `lead_number_invalid`, `lead_name_too_long`, `lead_nickname_too_long`, `lead_email_invalid`, `lead_birth_date_invalid`, `lead_phone_*`, `lead_address_*`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id			path		string					true	"ID do lead (UUID)"
// @Param			If-Match	header		string					true	"version lida do lead"
// @Param			request		body		lead.UpdateLeadRequest	true	"Campos a alterar"
// @Success		200			{object}	lead.LeadRecordResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	lead.LeadVersionConflictResponse
// @Failure		428			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id} [put]
func (h *LeadHandler) Update(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	expected, ok := httpx.IfMatchVersion(w, r)
	if !ok {
		return
	}
	var req UpdateLeadRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	updated, err := h.commands.Update(r.Context(), a, mux.Vars(r)["id"], &expected, req.toDomain())
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(updated, h.now()))
}

// @Summary		Renomear um lead
// @Description	Define o nome de exibição do lead. Enviar um nome vazio remove o nome e o lead volta a ser exibido pelo número. Exige no cabeçalho `If-Match` a `version` lida (428 `version_required` sem ela; 409 `version_conflict` com o registro atual em `current` quando o lead mudou). O nome passa a valer como definido pela equipe: importações e canais não o substituem mais. O corpo aceita só `name`; outra chave é recusada com 400 `invalid_body`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id			path		string				true	"ID do lead (UUID)"
// @Param			If-Match	header		string				true	"version lida do lead"
// @Param			request		body		RenameLeadRequest	true	"Novo nome"
// @Success		200			{object}	lead.LeadRecordResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	lead.LeadVersionConflictResponse
// @Failure		428			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id} [patch]
func (h *LeadHandler) RenameLead(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	expected, ok := httpx.IfMatchVersion(w, r)
	if !ok {
		return
	}
	var req RenameLeadRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name": "string (required; send \"\" to clear the name)",
		})
		return
	}
	renamed, err := h.commands.Rename(r.Context(), a, mux.Vars(r)["id"], &expected, *req.Name)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(renamed, h.now()))
}

// @Summary		Bloquear ou desbloquear lead
// @Description	Bloqueia ou desbloqueia o lead no workspace. `blocked` é obrigatório: um corpo sem ele, ou com outra chave, é recusado com 400 em vez de desbloquear. Repetir o mesmo pedido não muda nada. Quando `businessPhoneId` é um número do workspace ou um número da plataforma liberado para ele, o contato também é bloqueado ou desbloqueado na Meta (melhor esforço; `metaApplied` diz se deu certo). Não exige `If-Match`: é uma mudança pontual, que também avança a `version`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do lead (UUID)"
// @Param			request	body		BlockLeadRequest	true	"Estado de bloqueio do lead"
// @Success		200		{object}	lead.BlockLeadResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/block [post]
func (h *LeadHandler) BlockLead(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req BlockLeadRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.Blocked == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"blocked": "boolean (required; true blocks, false unblocks)",
		})
		return
	}
	result, err := h.commands.Block(r.Context(), a, mux.Vars(r)["id"], lead_usecase.BlockInput{Blocked: *req.Blocked, BusinessPhoneID: req.BusinessPhoneID})
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, BlockLeadResponse{
		LeadID:      result.Lead.ID,
		Blocked:     result.Lead.Blocked,
		MetaApplied: result.MetaApplied,
		Version:     result.Lead.Version,
	})
}

// @Summary		Definir o responsável pelo lead
// @Description	Define quem cuida do lead: um membro (`ownerId` = id do usuário), um agente (`ai:<id>`) ou um fluxo (`workflow:<id>`). Envie `ownerId` vazio para tirar o responsável; outra chave no corpo é recusada com 400 `invalid_body`. O responsável precisa ser do workspace (400 `lead_owner_outside_workspace`) e, quando é uma pessoa, alguém que você pode ver na equipe (403 `lead_owner_out_of_reach`). Não exige `If-Match`; repetir o mesmo responsável não muda nada.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do lead (UUID)"
// @Param			request	body		SetLeadOwnerRequest	true	"Novo responsável"
// @Success		200		{object}	lead.LeadRecordResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/owner [post]
func (h *LeadHandler) SetOwner(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req SetLeadOwnerRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.OwnerID == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"ownerId": "string (required; send \"\" to remove the owner)",
		})
		return
	}
	owned, err := h.commands.SetOwner(r.Context(), a, mux.Vars(r)["id"], *req.OwnerID)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(owned, h.now()))
}

// @Summary		Registrar que o lead não quer receber mensagens
// @Description	Marca o lead como "não quer receber mensagens" e retira o consentimento registrado. A partir daí os envios para ele passam a ser recusados antes de qualquer cobrança: nenhuma campanha (oficial ou não oficial) envia para ele, e a entrada fica com o código 920002 (`opted_out`), inclusive em campanhas criadas antes da marca; um modelo avulso (nova conversa, modelo numa conversa, mensagem agendada, envio pela Elo) responde `lead_opted_out`; um fluxo de automação não envia modelos para ele (o passo falha). Listas de ligação também o deixam de fora; uma ligação direta continua possível. Registra a origem em `source`: `operator` (a equipe decidiu, o padrão quando o corpo vem vazio ou sem `source`) ou `lead_request` (o lead pediu); a origem volta em `optOutSource` e fica no histórico de alterações. Só uma nova autorização explícita (whatsappOptIn em PUT /leads/{id}) desfaz a marca; importações e canais nunca a desfazem. Não exige `If-Match`; repetir o pedido não muda nada (a primeira origem fica). Uma chave desconhecida no corpo responde 400 `invalid_body`; uma origem desconhecida, 400 `lead_opt_out_source_invalid`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do lead (UUID)"
// @Param			request	body		lead.OptOutLeadRequest	false	"Origem do pedido (opcional)"
// @Success		200		{object}	lead.LeadRecordResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/opt-out [post]
func (h *LeadHandler) OptOut(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req OptOutLeadRequest
	if !httpx.DecodeOptionalStrictJSON(w, r, &req) {
		return
	}
	opted, err := h.commands.OptOut(r.Context(), a, mux.Vars(r)["id"], leaddomain.OptOutSource(req.Source))
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(opted, h.now()))
}

// @Summary		Anonimizar um lead
// @Description	Apaga de forma definitiva os dados pessoais do lead (LGPD art. 18): nome, apelido, e-mail, nascimento, número de WhatsApp, foto, consentimento, campos personalizados, telefones de contato, endereços, laços de família e indicação (as contagens do outro lado são ajustadas), memórias da IA, variáveis e metadados das entradas de campanha (oficial e não oficial) e os valores do histórico de alterações (fica só o nome de cada campo). As cobranças e as chamadas são mantidas, com o número mascarado (••••1234). O lead some de todas as listas e leituras; se a pessoa voltar a escrever, ela entra como um lead novo. Não pode ser desfeito. Exige `leads:anonymize` (capacidade `leads.anonymize`, reservada a gerentes; papéis antigos com `leads:delete` não ganham este poder). Números de contato que outro lead ainda guarda (o fixo da família, o celular de um parente) só são mascarados nas chamadas e envios deste lead; os registros dos outros ficam como estão. A resposta diz quantas linhas cada parte apagou ou mascarou em `erased` (chaves `lead_record`, `lead_phones`, `lead_addresses`, `lead_relations`, `lead_events`, `lead_memories`, `whatsapp_campaign_entries`, `unofficial_whatsapp_campaign_entries`, `calls`, `whatsapp_template_sends`). Quem tinha o lead aberto recebe `conversation:lead_update` com o campo `anonymized`.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID do lead (UUID)"
// @Success		200	{object}	lead.AnonymizeLeadResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/anonymize [post]
func (h *LeadHandler) Anonymize(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	erasure, err := h.commands.Anonymize(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAnonymizeResponse(erasure))
}

func identityHolderDetails(err error) map[string]string {
	var taken *leaddomain.IdentityTaken
	if errors.As(err, &taken) && taken.LeadID != "" {
		return map[string]string{"leadId": taken.LeadID}
	}
	return nil
}

func refusalDetails(err error) map[string]string {
	details := map[string]string{}
	var item *leaddomain.ItemError
	if errors.As(err, &item) {
		details["field"], details["index"] = item.Field, strconv.Itoa(item.Index)
	}
	var invalid address.InvalidFieldError
	if errors.As(err, &invalid) {
		details["addressField"], details["rule"] = string(invalid.Field), string(invalid.Rule)
	}
	var value *customfield.ValueError
	if errors.As(err, &value) {
		details["key"] = value.Key
	}
	if len(details) == 0 {
		return nil
	}
	return details
}

// @Summary		Definir o bairro e a cidade do lead
// @Description	Comando de um campo só, usado pelo painel da conversa: troca o bairro, a cidade e a UF do endereço principal sem mexer no resto (rua, número, CEP e os outros endereços ficam como estão). Sem endereço, cria um endereço principal `home` só com bairro, cidade e UF. Os três campos são obrigatórios no corpo (envie "" para limpar o bairro); uma área sem cidade e UF, num lead sem CEP, é recusada com 400 `lead_address_invalid`. O endereço volta para a fila de localização (`geoStatus` `pending`), mas um alfinete posto à mão continua. Exige `leads:update`; não exige `leads:read_addresses`, porque só mexe no que quem não vê o endereço completo já vê, e a resposta segue a mesma regra de visibilidade. Não exige `If-Match`; repetir a mesma área não muda nada.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do lead (UUID)"
// @Param			request	body		SetLeadDistrictRequest	true	"Bairro, cidade e UF"
// @Success		200		{object}	lead.LeadRecordResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/district [post]
func (h *LeadHandler) SetDistrict(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req SetLeadDistrictRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.District == nil || req.City == nil || req.State == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"district": "string (required; send \"\" to clear it)",
			"city":     "string (required)",
			"state":    "string (required; the two letter UF)",
			"cityCode": "string (optional; the 7 digit IBGE code)",
		})
		return
	}
	placed, err := h.commands.SetArea(r.Context(), a, mux.Vars(r)["id"], leaddomain.Area{
		District: *req.District, City: *req.City, State: *req.State, CityCode: req.CityCode,
	})
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(placed, h.now()))
}
