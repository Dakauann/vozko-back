package studiohttp

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/studio"
)

const presetEnvelopeBytes = 16 << 10

type PresetService interface {
	List(ctx context.Context, workspaceID string) ([]studio.AnimationPreset, error)
	Create(ctx context.Context, workspaceID, userID, name, description string, animation json.RawMessage) (*studio.AnimationPreset, error)
	Update(ctx context.Context, workspaceID, id string, change studio.PresetChange) (*studio.AnimationPreset, error)
	Delete(ctx context.Context, workspaceID, id string) error
}

type PresetRequest struct {
	Name        string          `json:"name" example:"Entrada com giro"`
	Description string          `json:"description,omitempty" example:"Para títulos de abertura"`
	Animation   json.RawMessage `json:"animation" swaggertype:"object"`
}

type PresetChangeRequest struct {
	Name        *string `json:"name,omitempty" example:"Entrada com giro"`
	Description *string `json:"description,omitempty" example:"Para títulos de abertura"`
}

type PresetResponse struct {
	ID           string            `json:"id" example:"builtin.riseIn"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	BuiltIn      bool              `json:"builtIn"`
	Names        map[string]string `json:"names,omitempty"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Animation    json.RawMessage   `json:"animation" swaggertype:"object"`
	CreatedAt    *time.Time        `json:"createdAt,omitempty"`
	UpdatedAt    *time.Time        `json:"updatedAt,omitempty"`
}

type PresetListResponse struct {
	Items []PresetResponse `json:"items"`
}

func presentPreset(p *studio.AnimationPreset) PresetResponse {
	out := PresetResponse{
		ID: p.ID, Name: p.Name, Description: p.Description, BuiltIn: p.BuiltIn,
		Names: p.Names, Descriptions: p.Descriptions, Animation: p.Animation,
	}
	if !p.BuiltIn {
		createdAt, updatedAt := p.CreatedAt, p.UpdatedAt
		out.CreatedAt, out.UpdatedAt = &createdAt, &updatedAt
	}
	return out
}

// @Summary		Listar predefinições de animação
// @Description	As predefinições prontas (builtIn true, somente leitura, com nomes e descrições em pt, en, es e de) seguidas das predefinições salvas no workspace, da mais nova para a mais antiga. animation guarda as partes da animação de um clipe; x, y, rotation e o x e y das chaves de position são deslocamentos a partir da posição do clipe.
// @Tags			Estúdio
// @Produce		json
// @Success		200	{object}	PresetListResponse
// @Security		BearerAuth
// @Router			/studio/animation-presets [get]
func (h *Handler) ListPresets(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	presets, err := h.presets.List(r.Context(), workspaceID)
	if err != nil {
		writeError(w, err, "Failed to list the animation presets")
		return
	}
	out := PresetListResponse{Items: make([]PresetResponse, 0, len(presets))}
	for i := range presets {
		out.Items = append(out.Items, presentPreset(&presets[i]))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Salvar predefinição de animação
// @Description	Salva a animação de um clipe como predefinição do workspace. name tem de 1 a 120 caracteres e description até 500. animation é validada inteira: scope visual ou text, durationMs de 100 a 600000, só as faixas x, y, position, scale, scaleX, scaleY, rotation, opacity e blur, comportamentos sem trackId, partes de texto só em scope text e ao menos uma parte; os erros respondem 422 com o código por campo. Até 1 MB.
// @Tags			Estúdio
// @Accept			json
// @Produce		json
// @Param			body	body		PresetRequest	true	"predefinição"
// @Success		201		{object}	PresetResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/animation-presets [post]
func (h *Handler) CreatePreset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body PresetRequest
	if !decodePreset(w, r, &body) {
		return
	}
	p, err := h.presets.Create(r.Context(), workspaceID, requesterOf(r), body.Name, body.Description, body.Animation)
	if err != nil {
		writeError(w, err, "Failed to save the animation preset")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentPreset(p))
}

// @Summary		Renomear predefinição de animação
// @Description	Troca o nome e ou a descrição de uma predefinição do workspace; a animação não muda. As predefinições prontas respondem 409 read_only.
// @Tags			Estúdio
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"id da predefinição"
// @Param			body	body		PresetChangeRequest	true	"mudanças"
// @Success		200		{object}	PresetResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/animation-presets/{id} [patch]
func (h *Handler) UpdatePreset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body PresetChangeRequest
	if !decodePreset(w, r, &body) {
		return
	}
	p, err := h.presets.Update(r.Context(), workspaceID, mux.Vars(r)["id"], studio.PresetChange{Name: body.Name, Description: body.Description})
	if err != nil {
		writeError(w, err, "Failed to rename the animation preset")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentPreset(p))
}

// @Summary		Apagar predefinição de animação
// @Description	Apaga de vez uma predefinição do workspace. Os clipes que já usaram a predefinição não mudam. As predefinições prontas respondem 409 read_only.
// @Tags			Estúdio
// @Param			id	path	string	true	"id da predefinição"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/animation-presets/{id} [delete]
func (h *Handler) DeletePreset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.presets.Delete(r.Context(), workspaceID, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to delete the animation preset")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodePreset(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, studio.MaxPresetAnimationBytes+presetEnvelopeBytes)
	return httpx.DecodeStrictJSON(w, r, into)
}
