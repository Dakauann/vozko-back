package conversation

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/ai_attendance"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	aa_usecase "vozko/usecases/ai_attendance"
)

type ToolCallLister interface {
	Execute(person shared.Person, workspaceID, entryType, entryID string, pagination shared.Pagination) (*shared.PaginatedResult[*ai_attendance.ToolCall], error)
}

var (
	refusalToolCallsUnavailable       = response.Refusal{Status: http.StatusServiceUnavailable, Code: "tool_calls_unavailable", Message: "Registro de ferramentas indisponível"}
	refusalToolCallsUnauthorized      = response.Refusal{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "Faça login de novo para continuar"}
	refusalToolCallConversationHidden = response.Refusal{Status: http.StatusNotFound, Code: "conversation_not_found", Message: "Conversa não encontrada"}
	refusalToolCallsFailed            = response.Refusal{Status: http.StatusInternalServerError, Code: "tool_calls_failed", Message: "Erro ao listar as chamadas de ferramenta"}
)

var toolCallRefusals = []response.Refusal{refusalToolCallsUnavailable, refusalToolCallsUnauthorized, refusalToolCallConversationHidden, refusalToolCallsFailed}

func (h *ConversationHandler) SetToolCalls(lister ToolCallLister) {
	h.toolCalls = lister
}

// @Summary		Listar as chamadas de ferramenta da IA em uma conversa
// @Description	Lista, da mais recente para a mais antiga, as ferramentas que o agente de IA chamou nesta conversa: nome da ferramenta, argumentos (com credenciais e documentos mascarados), resultado (ok, error ou requested, quando a ferramenta só foi pedida pelo modelo e quem a executa é um fluxo), mensagem de erro, duração e a execução de fluxo ligada, quando houver. toolCallId é o id da chamada (o do modelo, ou um gerado quando o modelo não manda), o mesmo gravado em metadata.tool_call_id nas linhas de ferramenta do histórico da conversa; registros antigos não o têm. Só aparece para quem pode ver a conversa; as chamadas ficam guardadas por 180 dias.
// @Tags			Conversas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada"
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			page		query		int		false	"Página, a partir de 1"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200	{object}	response.PaginatedPayload{data=[]ai_attendance.ToolCall}
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse	"Conversa não encontrada"
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/ai-tool-calls [get]
func (h *ConversationHandler) ListAIToolCalls(w http.ResponseWriter, r *http.Request) {
	if h.toolCalls == nil {
		response.WriteRefusal(w, refusalToolCallsUnavailable)
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteRefusal(w, refusalToolCallsUnauthorized)
		return
	}
	vars := mux.Vars(r)
	person := shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}
	result, err := h.toolCalls.Execute(person, middleware.GetWorkspaceID(r), vars["entryType"], vars["entryId"], httpx.ParsePagination(r.URL.Query()))
	switch {
	case errors.Is(err, aa_usecase.ErrConversationNotVisible):
		response.WriteRefusal(w, refusalToolCallConversationHidden)
		return
	case err != nil:
		response.WriteRefusal(w, refusalToolCallsFailed)
		return
	}
	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}
