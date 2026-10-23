package whatsapptemplate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	buttonactionhttp "vozko/delivery/http/buttonaction"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	whatsapptemplatedomain "vozko/domain/whatsapp/template"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type ExitGuardedEditor interface {
	EditTemplate(ctx context.Context, a buttonaction_usecase.Actor, req buttonaction_usecase.EditTemplateRequest) (*whatsapptemplatedomain.Template, error)
}

type EditTemplateRequest struct {
	Category       string                     `json:"category,omitempty" example:"MARKETING"`
	Components     []TemplateComponentRequest `json:"components"`
	HeaderMediaURL *string                    `json:"headerMediaUrl,omitempty" example:"https://cdn.exemplo.com/novo-cabecalho.jpg"`

	ButtonActions    []httpx.ButtonActionRequest `json:"buttonActions,omitempty"`
	ExitConfirmation string                      `json:"exitConfirmation,omitempty" example:"Pronto, você não receberá mais mensagens."`

	TrackedURLButtons []int `json:"trackedUrlButtons,omitempty" example:"2"`
}

var editRefusals = []struct {
	err    error
	status int
}{
	{whatsapptemplatedomain.ErrTemplateAccessDenied, http.StatusForbidden},
	{whatsapptemplatedomain.ErrEditNotOwner, http.StatusForbidden},
	{whatsapptemplatedomain.ErrTemplateNotFound, http.StatusNotFound},
	{whatsapptemplatedomain.ErrTemplateInUse, http.StatusConflict},
	{whatsapptemplatedomain.ErrTemplateNotEditable, http.StatusConflict},
	{whatsapptemplatedomain.ErrEditCategoryLocked, http.StatusConflict},
	{whatsapptemplatedomain.ErrEditUnsupported, http.StatusConflict},
	{whatsapptemplatedomain.ErrEditWindow, http.StatusTooManyRequests},
}

func (h *WhatsAppTemplateHandler) SetTemplateEditor(editor ExitGuardedEditor) {
	h.editor = editor
}

// @Summary		Editar o conteúdo de um modelo de mensagem
// @Description	Envia à Meta uma nova versão do modelo, que volta para a revisão. Nome e idioma não mudam; a categoria só muda em modelos REJECTED ou PAUSED. O corpo traz todos os componentes que o modelo deve ter (cabeçalho, corpo, rodapé e botões): o que não vier é removido. Partes que a plataforma não edita (por exemplo oferta por tempo limitado ou carrossel) são mantidas como estão na Meta; botões que a plataforma não edita (fluxo, código) impedem trocar os botões (409 `template_edit_unsupported`). Um cabeçalho de mídia é reenviado à Meta a partir de `headerMediaUrl` (precisa estar no armazenamento da plataforma), da mídia guardada do modelo ou da cópia que a Meta tem, então a imagem não se perde. Só modelos APPROVED, REJECTED ou PAUSED podem ser editados (409 `template_not_editable`); um modelo APPROVED aceita uma edição por dia e dez a cada 30 dias (429 `template_edit_window`, com `expected.nextEditAt`). Só um workspace com número na conta do modelo pode editá-lo (403 `template_edit_not_owner`), e nunca enquanto uma campanha em andamento ou agendada o usa (409 `template_in_use`). O corpo pode trazer a saída do lead junto, como na criação: `buttonActions` (pelo `buttonIndex` e com `buttonText` igual ao texto do botão na nova versão) e `exitConfirmation`; elas só são gravadas depois que a Meta aceita a edição. Um modelo de MARKETING precisa continuar mostrando uma saída ao lead, um botão de resposta rápida com ação `opt_out` ou `consent_grant` ou um comando `/palavra` no texto (422 `marketing_template_needs_exit`); as recusas das ações usam os códigos `button_action_*` da rota de ações. Modelos de AUTHENTICATION não são editados aqui. trackedUrlButtons lista os índices (entre todos os botões) dos botões de URL com "Rastrear cliques": a plataforma cria um link curto para o destino informado e troca a URL do botão pelo link curto com a variável de sufixo ({{1}}) e um exemplo, o formato que a Meta aprova; em cada envio a variável recebe um token do envio e o toque sempre abre o destino real. Um botão rastreado fora da lista volta para o destino do link curto (template_tracked_link_unreadable quando o link não pode ser lido). Uma recusa do rastreio traz expected.buttonIndex, o índice do botão recusado. Sem o campo, nada muda no rastreio.
// @Tags			Modelos de WhatsApp
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do modelo"
// @Param			request	body		EditTemplateRequest	true	"Nova versão do modelo"
// @Success		200		{object}	template.Template
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/content [put]
func (h *WhatsAppTemplateHandler) EditForWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, ok := guardActor(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if h.editor == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Template editing is not available", nil)
		return
	}
	var req EditTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	components, ok := h.trackButtons(w, r, actor.UserID, toDomainComponents(req.Components), req.TrackedURLButtons)
	if !ok {
		return
	}
	edited, err := h.editor.EditTemplate(r.Context(), actor, buttonaction_usecase.EditTemplateRequest{
		TemplateID: mux.Vars(r)["id"],
		Edit: whatsapptemplatedomain.Edit{
			Category:       whatsapptemplatedomain.TemplateCategory(req.Category),
			Components:     components,
			HeaderMediaURL: req.HeaderMediaURL,
		},
		Actions:          httpx.ButtonActionInputs(req.ButtonActions),
		ExitConfirmation: req.ExitConfirmation,
	})
	if err != nil {
		writeEditError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, edited)
}

func writeEditError(w http.ResponseWriter, err error) {
	for _, refusal := range editRefusals {
		if errors.Is(err, refusal.err) {
			response.WriteErrorWithCode(w, refusal.status, whatsapptemplatedomain.ErrorCode(err), err.Error(), editExpectation(err))
			return
		}
	}
	if buttonactionhttp.WriteRefusal(w, err) {
		return
	}
	writeTemplateError(w, err)
}

func editExpectation(err error) map[string]string {
	var window *whatsapptemplatedomain.EditWindowError
	if !errors.As(err, &window) {
		return nil
	}
	return map[string]string{"nextEditAt": window.NextAt.UTC().Format(time.RFC3339)}
}
