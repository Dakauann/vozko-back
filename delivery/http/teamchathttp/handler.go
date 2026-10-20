package teamchathttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/delivery/teamchatview"
	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

type Handler struct {
	chat teamchat.UseCases
}

func NewHandler(chat teamchat.UseCases) *Handler {
	return &Handler{chat: chat}
}

func caller(w http.ResponseWriter, r *http.Request) (shared.Person, string, bool) {
	by, ok := httpx.RequirePerson(w, r)
	if !ok {
		return shared.Person{}, "", false
	}
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	return by, workspaceID, ok
}

func pathID(r *http.Request, name string) string {
	return strings.TrimSpace(mux.Vars(r)[name])
}

func queryInt(w http.ResponseWriter, r *http.Request, name, code string) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		response.WriteErrorWithCode(w, http.StatusBadRequest, code, name+" must be a whole number, zero or more", nil)
		return 0, false
	}
	return value, true
}

// @Summary		Pessoas do chat da equipe
// @Description	Lista todos os membros do workspace, com nome e foto, em ordem de nome. Todo membro usa o chat da equipe, sem permissão; só criar grupos depende de `team_chat:create`. É com essas pessoas que você pode abrir uma conversa direta ou formar um grupo. Os departamentos não limitam a lista.
// @Tags			Chat da equipe
// @Produce		json
// @Success		200	{object}	PeopleResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden: você não é membro deste workspace"
// @Security		BearerAuth
// @Router			/team-chat/people [get]
func (h *Handler) People(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	people, err := h.chat.Directory(r.Context(), by, workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, PeopleResponse{People: teamchatview.PeopleOf(people)})
}

// @Summary		Não lidas do chat da equipe
// @Description	Quantas conversas da equipe têm mensagens que você não leu e quantas mensagens são. Conversas silenciadas não contam, a não ser que tenham uma menção a você ainda não lida. Serve para o selo do botão Equipe; atualize ao receber `team_chat:message` ou `team_chat:read` pelo WebSocket de conversas, ou a cada minuto quando não há WebSocket.
// @Tags			Chat da equipe
// @Produce		json
// @Success		200	{object}	SummaryResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/team-chat/summary [get]
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	totals, err := h.chat.Summary(r.Context(), by, workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SummaryResponse{UnreadThreads: totals.UnreadThreads, UnreadMessages: totals.UnreadMessages})
}

// @Summary		Conversas da equipe
// @Description	Suas conversas da equipe (diretas e grupos de que você participa), da atividade mais recente para a mais antiga, com os participantes, a última mensagem, `unread` (mensagens depois da sua última leitura) e `unread_mentions`. Para a próxima página, envie `next` em `cursor`; `next` vazio indica a última página.
// @Tags			Chat da equipe
// @Produce		json
// @Param			cursor	query		string	false	"Valor de next da página anterior"
// @Param			limit	query		int		false	"Quantidade (padrão 30, máximo 50)"
// @Success		200	{object}	ThreadListResponse
// @Failure		400	{object}	response.ErrorResponse	"invalid_cursor ou invalid_limit"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/team-chat/threads [get]
func (h *Handler) Threads(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	limit, ok := queryInt(w, r, "limit", "invalid_limit")
	if !ok {
		return
	}
	page, err := h.chat.Threads(r.Context(), by, workspaceID, r.URL.Query().Get("cursor"), int(limit))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ThreadListResponse{Threads: teamchatview.ThreadsOf(page.Threads), Next: page.Next})
}

// @Summary		Abrir conversa direta
// @Description	Abre a conversa direta com a pessoa (ou devolve a que já existe: há uma só por par de pessoas). A pessoa precisa ser membro do workspace; senão `member_unavailable`.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			request	body		OpenDirectRequest	true	"Com quem"
// @Success		200	{object}	ThreadEnvelope
// @Failure		400	{object}	response.ErrorResponse	"invalid_body, person_required ou same_person"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden: você não é membro deste workspace"
// @Failure		409	{object}	response.ErrorResponse	"member_unavailable"
// @Security		BearerAuth
// @Router			/team-chat/direct [post]
func (h *Handler) OpenDirect(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req OpenDirectRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	view, err := h.chat.OpenDirect(r.Context(), by, workspaceID, req.UserID)
	h.writeThread(w, http.StatusOK, view, err)
}

// @Summary		Criar grupo
// @Description	Cria um grupo com título (1 a 80 caracteres) e as pessoas escolhidas (você entra como administrador; no máximo 256 pessoas). Todas precisam ser membros do workspace; senão `member_unavailable` e nada é criado. Precisa de `team_chat:create`. Os outros participantes recebem `team_chat:thread`.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			request	body		CreateGroupRequest	true	"Título e participantes"
// @Success		201	{object}	ThreadEnvelope
// @Failure		400	{object}	response.ErrorResponse	"invalid_body, title_required, title_too_long, members_required ou too_many_members"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden: sem team_chat:create"
// @Failure		409	{object}	response.ErrorResponse	"member_unavailable"
// @Security		BearerAuth
// @Router			/team-chat/groups [post]
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req CreateGroupRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	view, err := h.chat.CreateGroup(r.Context(), by, workspaceID, req.Title, req.MemberIDs)
	h.writeThread(w, http.StatusCreated, view, err)
}

// @Summary		Uma conversa da equipe
// @Description	Uma conversa de que você participa, com os participantes e o `last_read_seq` de cada um (a confirmação de leitura). Quem não participa recebe 404.
// @Tags			Chat da equipe
// @Produce		json
// @Param			id	path		string	true	"ID da conversa"
// @Success		200	{object}	ThreadEnvelope
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id} [get]
func (h *Handler) Thread(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	view, err := h.chat.Thread(r.Context(), by, workspaceID, pathID(r, "id"))
	h.writeThread(w, http.StatusOK, view, err)
}

// @Summary		Renomear grupo
// @Description	Só administradores do grupo. Grava uma mensagem de sistema `renamed` e os participantes recebem `team_chat:message` e `team_chat:thread`.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"ID do grupo"
// @Param			request	body		RenameRequest	true	"Novo título"
// @Success		200	{object}	ThreadEnvelope
// @Failure		400	{object}	response.ErrorResponse	"title_required ou title_too_long"
// @Failure		403	{object}	response.ErrorResponse	"forbidden ou not_admin"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Failure		409	{object}	response.ErrorResponse	"direct_thread"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id} [patch]
func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req RenameRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	view, err := h.chat.Rename(r.Context(), by, workspaceID, pathID(r, "id"), req.Title)
	h.writeThread(w, http.StatusOK, view, err)
}

// @Summary		Adicionar pessoas ao grupo
// @Description	Só administradores do grupo. Quem já participa é ignorado; quem entra começa com o histórico lido até agora e vê o histórico do grupo. Todas as pessoas precisam ser membros do workspace.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do grupo"
// @Param			request	body		AddMembersRequest	true	"Pessoas"
// @Success		200	{object}	ThreadEnvelope
// @Failure		400	{object}	response.ErrorResponse	"members_required ou too_many_members"
// @Failure		403	{object}	response.ErrorResponse	"forbidden ou not_admin"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Failure		409	{object}	response.ErrorResponse	"direct_thread ou member_unavailable"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/members [post]
func (h *Handler) AddMembers(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req AddMembersRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	view, err := h.chat.AddMembers(r.Context(), by, workspaceID, pathID(r, "id"), req.UserIDs)
	h.writeThread(w, http.StatusOK, view, err)
}

// @Summary		Tirar uma pessoa do grupo
// @Description	Só administradores do grupo (tirar a si mesmo é sair). A pessoa perde o acesso ao grupo na hora e recebe `team_chat:thread_removed`. Se ela era a última administradora, quem entrou primeiro vira administrador.
// @Tags			Chat da equipe
// @Param			id		path	string	true	"ID do grupo"
// @Param			userId	path	string	true	"ID da pessoa"
// @Success		204	"Removida"
// @Failure		403	{object}	response.ErrorResponse	"forbidden ou not_admin"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Failure		409	{object}	response.ErrorResponse	"direct_thread"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/members/{userId} [delete]
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	h.writeNoContent(w, h.chat.RemoveMember(r.Context(), by, workspaceID, pathID(r, "id"), pathID(r, "userId")))
}

// @Summary		Sair do grupo
// @Description	Você deixa de participar e perde o acesso ao grupo e ao histórico. Se era o último administrador, quem entrou primeiro vira administrador. Conversas diretas não podem ser deixadas (`direct_thread`).
// @Tags			Chat da equipe
// @Param			id	path	string	true	"ID do grupo"
// @Success		204	"Saiu"
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Failure		409	{object}	response.ErrorResponse	"direct_thread"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/leave [post]
func (h *Handler) Leave(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	h.writeNoContent(w, h.chat.Leave(r.Context(), by, workspaceID, pathID(r, "id")))
}

// @Summary		Silenciar conversa
// @Description	Só para você: uma conversa silenciada não conta no selo de não lidas e não toca som nem mostra notificação no computador, a não ser que a mensagem mencione você. As suas outras abas recebem `team_chat:thread`.
// @Tags			Chat da equipe
// @Accept			json
// @Param			id		path	string		true	"ID da conversa"
// @Param			request	body	MuteRequest	true	"Silenciar ou não"
// @Success		204	"Gravado"
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/mute [put]
func (h *Handler) Mute(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req MuteRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	h.writeNoContent(w, h.chat.Mute(r.Context(), by, workspaceID, pathID(r, "id"), req.Muted))
}

// @Summary		Mensagens de uma conversa da equipe
// @Description	Mensagens da mais antiga para a mais nova. Sem `before_seq` nem `after_seq`: as mais recentes. Com `before_seq`: as anteriores a essa sequência (`has_more` diz se há ainda mais antigas). Com `after_seq`: as seguintes, para recuperar o que chegou durante uma queda de conexão (`has_more` diz se há mais novas; repita com o maior `seq` recebido). Mensagens apagadas vêm com `deleted_at` e sem texto.
// @Tags			Chat da equipe
// @Produce		json
// @Param			id			path		string	true	"ID da conversa"
// @Param			before_seq	query		int		false	"Só mensagens com seq menor"
// @Param			after_seq	query		int		false	"Só mensagens com seq maior"
// @Param			limit		query		int		false	"Quantidade (padrão 50, máximo 100)"
// @Success		200	{object}	MessageListResponse
// @Failure		400	{object}	response.ErrorResponse	"invalid_seq, invalid_limit ou page_direction"
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/messages [get]
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	q := teamchat.MessageQuery{ThreadID: pathID(r, "id")}
	if q.BeforeSeq, ok = queryInt(w, r, "before_seq", "invalid_seq"); !ok {
		return
	}
	if q.AfterSeq, ok = queryInt(w, r, "after_seq", "invalid_seq"); !ok {
		return
	}
	limit, ok := queryInt(w, r, "limit", "invalid_limit")
	if !ok {
		return
	}
	q.Limit = int(limit)
	page, err := h.chat.Messages(r.Context(), by, workspaceID, q)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, MessageListResponse{Messages: teamchatview.MessagesOf(page.Messages), HasMore: page.HasMore})
}

// @Summary		Enviar mensagem para a equipe
// @Description	Texto de 1 a 4000 caracteres. `mentions` traz até 50 IDs; só ficam os de quem participa da conversa. `reply_to_id` cita uma mensagem da mesma conversa. `client_id` (até 128 caracteres, um UUID por envio) torna o envio idempotente: repetir o mesmo `client_id` devolve a mensagem já gravada com 200, sem enviar de novo. Os participantes recebem `team_chat:message`. Numa conversa direta cuja outra pessoa saiu do workspace, `recipient_unavailable`.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string		true	"ID da conversa"
// @Param			request	body		SendRequest	true	"Mensagem"
// @Success		201	{object}	MessageEnvelope	"Enviada"
// @Success		200	{object}	MessageEnvelope	"Já enviada com este client_id"
// @Failure		400	{object}	response.ErrorResponse	"invalid_body, body_required, body_too_long, too_many_mentions, client_id_too_long ou reply_outside_thread"
// @Failure		403	{object}	response.ErrorResponse	"forbidden: você não é membro deste workspace"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Failure		409	{object}	response.ErrorResponse	"recipient_unavailable"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/messages [post]
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req SendRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	sent, err := h.chat.Send(r.Context(), by, workspaceID, pathID(r, "id"), teamchat.Draft{
		Body: req.Body, Mentions: req.Mentions, ReplyToID: req.ReplyToID, ClientID: req.ClientID,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	status := http.StatusOK
	if sent.Created {
		status = http.StatusCreated
	}
	response.WriteSuccess(w, status, MessageEnvelope{Message: teamchatview.MessageOf(sent.Message)})
}

// @Summary		Marcar como lida
// @Description	Marca a conversa como lida até `seq`. Nunca volta para trás nem passa da última mensagem; a resposta traz onde a sua leitura ficou. Quando muda, os participantes recebem `team_chat:read` (confirmação de leitura e sincronia entre as suas abas).
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string		true	"ID da conversa"
// @Param			request	body		ReadRequest	true	"Até qual seq"
// @Success		200	{object}	ReadResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/read [post]
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req ReadRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	read, err := h.chat.MarkRead(r.Context(), by, workspaceID, pathID(r, "id"), req.Seq)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ReadResponse{LastReadSeq: read})
}

// @Summary		Editar mensagem da equipe
// @Description	Só o autor, e só mensagens de texto ainda não apagadas. A mensagem ganha `edited_at` e os participantes recebem `team_chat:message_updated`.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string		true	"ID da mensagem"
// @Param			request	body		EditRequest	true	"Novo texto"
// @Success		200	{object}	MessageEnvelope
// @Failure		400	{object}	response.ErrorResponse	"body_required ou body_too_long"
// @Failure		403	{object}	response.ErrorResponse	"forbidden ou not_author"
// @Failure		404	{object}	response.ErrorResponse	"message_not_found"
// @Failure		409	{object}	response.ErrorResponse	"system_message"
// @Security		BearerAuth
// @Router			/team-chat/messages/{id} [patch]
func (h *Handler) Edit(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req EditRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	msg, err := h.chat.Edit(r.Context(), by, workspaceID, pathID(r, "id"), req.Body)
	h.writeMessage(w, msg, err)
}

// @Summary		Apagar mensagem da equipe
// @Description	Só o autor. A mensagem continua no histórico como apagada (`deleted_at`, sem texto nem menções) e os participantes recebem `team_chat:message_updated`.
// @Tags			Chat da equipe
// @Produce		json
// @Param			id	path		string	true	"ID da mensagem"
// @Success		200	{object}	MessageEnvelope
// @Failure		403	{object}	response.ErrorResponse	"forbidden ou not_author"
// @Failure		404	{object}	response.ErrorResponse	"message_not_found"
// @Failure		409	{object}	response.ErrorResponse	"system_message"
// @Security		BearerAuth
// @Router			/team-chat/messages/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	msg, err := h.chat.Delete(r.Context(), by, workspaceID, pathID(r, "id"))
	h.writeMessage(w, msg, err)
}

// @Summary		Mensagens agendadas nesta conversa
// @Description	As suas mensagens ainda pendentes nesta conversa, da mais próxima para a mais distante. Ninguém mais vê as suas mensagens agendadas.
// @Tags			Chat da equipe
// @Produce		json
// @Param			id	path		string	true	"ID da conversa"
// @Success		200	{object}	ScheduledListResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/scheduled [get]
func (h *Handler) ScheduledList(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	list, err := h.chat.Scheduled(r.Context(), by, workspaceID, pathID(r, "id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ScheduledListResponse{Scheduled: teamchatview.ScheduledListOf(list)})
}

// @Summary		Agendar mensagem para a equipe
// @Description	Agenda uma mensagem para `send_at` (de 1 minuto a 30 dias à frente). Valem as mesmas regras de enviar agora, conferidas de novo na hora do envio: se você saiu da conversa ou do workspace, ou a outra pessoa da conversa direta saiu do workspace, a mensagem fica `failed` com o motivo em `failure_reason` e nada é enviado. O envio nunca se repete, mesmo se o servidor reiniciar no meio. Você recebe `team_chat:scheduled` a cada mudança.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"ID da conversa"
// @Param			request	body		ScheduleRequest	true	"Mensagem e horário"
// @Success		201	{object}	ScheduledEnvelope
// @Failure		400	{object}	response.ErrorResponse	"send_at_required, send_at_too_soon, send_at_too_far, body_required, body_too_long ou reply_outside_thread"
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"thread_not_found"
// @Failure		409	{object}	response.ErrorResponse	"recipient_unavailable"
// @Security		BearerAuth
// @Router			/team-chat/threads/{id}/scheduled [post]
func (h *Handler) Schedule(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req ScheduleRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.SendAt == nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "send_at_required", "send_at is required", nil)
		return
	}
	scheduled, err := h.chat.Schedule(r.Context(), by, workspaceID, pathID(r, "id"), teamchat.Draft{
		Body: req.Body, Mentions: req.Mentions, ReplyToID: req.ReplyToID,
	}, *req.SendAt)
	h.writeScheduled(w, http.StatusCreated, scheduled, err)
}

// @Summary		Alterar mensagem agendada
// @Description	Muda o texto e ou o horário de uma mensagem sua ainda pendente. Uma mensagem já enviada, cancelada ou que falhou responde `scheduled_final`.
// @Tags			Chat da equipe
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da mensagem agendada"
// @Param			request	body		RescheduleRequest	true	"O que muda"
// @Success		200	{object}	ScheduledEnvelope
// @Failure		400	{object}	response.ErrorResponse	"send_at_too_soon, send_at_too_far, body_required ou body_too_long"
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"scheduled_not_found"
// @Failure		409	{object}	response.ErrorResponse	"scheduled_final"
// @Security		BearerAuth
// @Router			/team-chat/scheduled/{id} [patch]
func (h *Handler) Reschedule(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	var req RescheduleRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	scheduled, err := h.chat.Reschedule(r.Context(), by, workspaceID, pathID(r, "id"), teamchat.Reschedule{Body: req.Body, SendAt: req.SendAt})
	h.writeScheduled(w, http.StatusOK, scheduled, err)
}

// @Summary		Cancelar mensagem agendada
// @Description	Cancela uma mensagem sua ainda pendente.
// @Tags			Chat da equipe
// @Param			id	path	string	true	"ID da mensagem agendada"
// @Success		204	"Cancelada"
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"scheduled_not_found"
// @Failure		409	{object}	response.ErrorResponse	"scheduled_final"
// @Security		BearerAuth
// @Router			/team-chat/scheduled/{id} [delete]
func (h *Handler) CancelScheduled(w http.ResponseWriter, r *http.Request) {
	by, workspaceID, ok := caller(w, r)
	if !ok {
		return
	}
	h.writeNoContent(w, h.chat.CancelScheduled(r.Context(), by, workspaceID, pathID(r, "id")))
}

func (h *Handler) writeThread(w http.ResponseWriter, status int, view teamchat.ThreadView, err error) {
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, status, ThreadEnvelope{Thread: teamchatview.ThreadOf(view)})
}

func (h *Handler) writeMessage(w http.ResponseWriter, msg teamchat.AuthoredMessage, err error) {
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, MessageEnvelope{Message: teamchatview.MessageOf(msg)})
}

func (h *Handler) writeScheduled(w http.ResponseWriter, status int, scheduled teamchat.Scheduled, err error) {
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, status, ScheduledEnvelope{Scheduled: teamchatview.ScheduledOf(scheduled)})
}

func (h *Handler) writeNoContent(w http.ResponseWriter, err error) {
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
