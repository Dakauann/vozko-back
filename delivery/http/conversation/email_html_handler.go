package conversation

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	conversationdomain "vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	emailinbox_usecase "vozko/usecases/emailinbox"
)

type EmailHTMLViewer interface {
	View(ctx context.Context, in emailinbox_usecase.HTMLRequest) (em.HTMLView, error)
}

var (
	refusalEmailHTMLUnavailable  = response.Refusal{Status: http.StatusServiceUnavailable, Code: "email_html_unavailable", Message: "A visualização do e-mail está indisponível"}
	refusalEmailHTMLUnauthorized = response.Refusal{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "Faça login de novo para continuar"}
	refusalEmailHTMLHidden       = response.Refusal{Status: http.StatusNotFound, Code: "conversation_not_found", Message: "Conversa não encontrada"}
	refusalEmailHTMLNotAnEmail   = response.Refusal{Status: http.StatusNotFound, Code: "email_message_not_found", Message: "Este e-mail não está nesta conversa"}
	refusalEmailHTMLFailed       = response.Refusal{Status: http.StatusInternalServerError, Code: "email_html_failed", Message: "Não foi possível abrir este e-mail"}
)

var emailHTMLRefusals = []response.Refusal{refusalEmailHTMLUnavailable, refusalEmailHTMLUnauthorized, refusalEmailHTMLHidden, refusalEmailHTMLNotAnEmail, refusalEmailHTMLFailed}

func (h *ConversationHandler) SetEmailHTML(viewer EmailHTMLViewer) {
	h.emailHTML = viewer
}

// @Summary		Ver um e-mail como foi enviado
// @Description	Devolve o HTML de um e-mail da conversa, já higienizado: sem scripts, atributos de evento, formulários, iframes, links `javascript:` e meta refresh; os links abrem em outra aba com `rel="noopener noreferrer"`. Os e-mails enviados mostram o HTML como foi composto, antes do pixel de abertura e da troca de links do provedor, então ver o e-mail não registra abertura nem clique; os originais de campanha são montados na hora a partir do modelo da campanha e dos valores de mesclagem do destinatário. Nos e-mails recebidos as imagens remotas ficam bloqueadas por padrão (`images_blocked: true`) para que o pixel do remetente não saiba quando o e-mail foi lido; envie `images=show` para mostrá-las. Sem HTML, ou se a higienização falhar, `has_html` é `false` e só `text` vem preenchido. Segue a mesma permissão e o mesmo escopo de departamento de ler a conversa.
// @Tags			Conversas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada (email)"
// @Param			entryId		path		string	true	"ID da conversa de e-mail"
// @Param			messageId	path		string	true	"ID da mensagem"
// @Param			images		query		string	false	"show para mostrar as imagens remotas de um e-mail recebido"
// @Success		200	{object}	emailinbox.HTMLView
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse	"Conversa ou e-mail não encontrado"
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/messages/{messageId}/email-html [get]
func (h *ConversationHandler) GetEmailHTML(w http.ResponseWriter, r *http.Request) {
	if h.emailHTML == nil {
		response.WriteRefusal(w, refusalEmailHTMLUnavailable)
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteRefusal(w, refusalEmailHTMLUnauthorized)
		return
	}
	vars := mux.Vars(r)
	if vars["entryType"] != string(shared.EntryTypeEmail) {
		response.WriteRefusal(w, refusalEmailHTMLNotAnEmail)
		return
	}
	view, err := h.emailHTML.View(r.Context(), emailinbox_usecase.HTMLRequest{
		Viewer:      shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)},
		WorkspaceID: middleware.GetWorkspaceID(r),
		EntryID:     vars["entryId"],
		MessageID:   vars["messageId"],
		ShowImages:  r.URL.Query().Get("images") == "show",
	})
	switch {
	case errors.Is(err, conversationdomain.ErrNotVisible):
		response.WriteRefusal(w, refusalEmailHTMLHidden)
		return
	case errors.Is(err, em.ErrNotAnEmail):
		response.WriteRefusal(w, refusalEmailHTMLNotAnEmail)
		return
	case err != nil:
		response.WriteRefusal(w, refusalEmailHTMLFailed)
		return
	}
	response.WriteSuccess(w, http.StatusOK, view)
}
