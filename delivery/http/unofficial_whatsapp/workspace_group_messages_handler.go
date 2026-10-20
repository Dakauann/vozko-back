package unofficial_whatsapp

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

// @Summary		Mensagens de um grupo
// @Description	Mensagens da conversa do grupo lida por um número do escopo de departamentos de quem consulta, da mais antiga para a mais recente, com a mídia guardada (tamanho e thumbhash) e o nome de quem escreveu, vindo dos membros e dos contatos. Nossas mensagens saem como "Nossa equipe" (`ourTeam`), nunca com o operador. Sem `before` responde a página mais recente; para carregar as anteriores, mande `before` com o `nextBefore` da página. Sem `nextBefore`, é o início do histórico. Grupo fora do escopo responde 404.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id		path		string	true	"Grupo"
// @Param			before	query		string	false	"Cursor RFC 3339: mensagens anteriores a este instante"
// @Param			limit	query		int		false	"Mensagens por página (até 100)"	default(50)
// @Success		200		{object}	unofficial_whatsapp_group_usecase.GroupMessagesPage
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/messages [get]
func (h *WorkspaceGroupHandler) Messages(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	in := uwguc.GroupMessagesInput{}
	if limit := httpx.ParseIntQuery(values.Get("limit")); limit != nil {
		in.Limit = *limit
	}
	if raw := strings.TrimSpace(values.Get("before")); raw != "" {
		before, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, "before must be an RFC 3339 instant", nil)
			return
		}
		in.Before = &before
	}
	page, err := h.groups.GroupMessages(r.Context(), a, mux.Vars(r)["id"], in)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, page)
}
