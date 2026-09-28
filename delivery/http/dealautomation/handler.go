package dealautomation

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
	dealautomation_usecase "vozko/usecases/dealautomation"
	opportunity_usecase "vozko/usecases/opportunity"
)

type Handler struct {
	settings *dealautomation_usecase.UseCase
}

func NewHandler(settings *dealautomation_usecase.UseCase) *Handler {
	return &Handler{settings: settings}
}

type SettingResponse struct {
	PipelineID string     `json:"pipelineId"`
	Enabled    bool       `json:"enabled"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

type UpdateRequest struct {
	PipelineID string `json:"pipelineId"`
}

var containerKinds = map[string]conversation.ContainerKind{
	"account":  conversation.ContainerKindAccount,
	"campaign": conversation.ContainerKindCampaign,
}

func channelFrom(r *http.Request) (dealautomation.Channel, string, bool) {
	vars := mux.Vars(r)
	kind, known := containerKinds[vars["kind"]]
	containerID := strings.TrimSpace(vars["containerId"])
	if !known || containerID == "" {
		return dealautomation.Channel{}, "", false
	}
	return dealautomation.Channel{EntryType: shared.EntryType(vars["entryType"]), Kind: kind}, containerID, true
}

func toResponse(setting *dealautomation.Setting) SettingResponse {
	out := SettingResponse{PipelineID: setting.PipelineID, Enabled: setting.Enabled()}
	if !setting.UpdatedAt.IsZero() {
		at := setting.UpdatedAt
		out.UpdatedAt = &at
	}
	return out
}

// @Summary		Consultar oportunidades automáticas do canal
// @Description	Retorna o funil de oportunidades que a análise automática usa neste canal. Sem funil, as oportunidades automáticas estão desligadas.
// @Tags			Oportunidades automáticas
// @Produce		json
// @Param			entryType	path	string	true	"Tipo de conversa do canal (whatsapp, unofficial_whatsapp, instagram, facebook, telegram)"
// @Param			kind		path	string	true	"account ou campaign"
// @Param			containerId	path	string	true	"ID da conta, número, página ou campanha"
// @Success		200	{object}	SettingResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/deal-automation/{entryType}/{kind}/{containerId} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	channel, containerID, ok := channelFrom(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Invalid channel", nil)
		return
	}
	setting, err := h.settings.Get(personFrom(claims.UserID, claims.Role), middleware.GetWorkspaceID(r), channel, containerID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(setting))
}

// @Summary		Configurar oportunidades automáticas do canal
// @Description	Define o funil de oportunidades que a análise automática usa neste canal. Envie pipelineId vazio para desligar.
// @Tags			Oportunidades automáticas
// @Accept			json
// @Produce		json
// @Param			entryType	path	string			true	"Tipo de conversa do canal"
// @Param			kind		path	string			true	"account ou campaign"
// @Param			containerId	path	string			true	"ID da conta, número, página ou campanha"
// @Param			request		body	UpdateRequest	true	"Funil de oportunidades"
// @Success		200	{object}	SettingResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/deal-automation/{entryType}/{kind}/{containerId} [put]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	channel, containerID, ok := channelFrom(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Invalid channel", nil)
		return
	}
	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{"pipelineId": "string"})
		return
	}
	setting, err := h.settings.Set(personFrom(claims.UserID, claims.Role), middleware.GetWorkspaceID(r), channel, containerID, req.PipelineID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(setting))
}

func personFrom(userID, role string) shared.Person {
	return shared.Person{UserID: userID, SystemAdmin: role == string(user.RoleAdmin)}
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dealautomation.ErrUnsupportedChannel):
		response.WriteError(w, http.StatusBadRequest, "Este canal não tem oportunidades automáticas", nil)
	case errors.Is(err, workspace.ErrUnauthorized), errors.Is(err, workspace.ErrInsufficientPermissions):
		response.WriteError(w, http.StatusForbidden, "Sem permissão para este canal", nil)
	default:
		if refusal, ok := opportunity_usecase.RefusalOf(err); ok && refusal == opportunity_usecase.RefusalPipelineInvalid {
			response.WriteError(w, http.StatusUnprocessableEntity, "Escolha um funil de oportunidades com etapas de ganho e perda", nil)
			return
		}
		log.Printf("[deal-automation] request failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
