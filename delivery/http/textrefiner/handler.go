package textrefiner

import (
	"encoding/json"
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/ai"
	"vozko/domain/text_refiner"
	"vozko/infra/http/middleware"
)

type TextRefinerHandler struct {
	refineUC  text_refiner.RefineTextUseCase
	aiService ai.Service
}

func NewTextRefinerHandler(refineUC text_refiner.RefineTextUseCase, aiService ai.Service) *TextRefinerHandler {
	return &TextRefinerHandler{refineUC: refineUC, aiService: aiService}
}

// @Summary		Refinar um texto com IA
// @Description	Reescreve um texto conforme a instrução informada e retorna o texto refinado, os segmentos de diferença em relação ao original e a estimativa de custo. Utilizado nos editores de prompts e de mensagens.
// @Tags			Refinador de texto
// @Accept			json
// @Produce		json
// @Param			request	body		RefineRequest	true	"Texto original, instrução e opções de modelo"
// @Success		200		{object}	RefineResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		402		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		502		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/text-refiner/refine [post]
func (h *TextRefinerHandler) Refine(w http.ResponseWriter, r *http.Request) {
	var req RefineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"text":        "string (required)",
			"instruction": "string (required)",
			"kind":        "string (optional: generic | messaging_prompt | initial_message)",
			"model":       "string (optional, OpenRouter model id)",
			"maxTokens":   "int (optional)",
			"temperature": "float (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace context required", nil)
		return
	}

	out, err := h.refineUC.Execute(r.Context(), text_refiner.RefineInput{
		WorkspaceID:  wsID,
		UserID:       claims.UserID,
		Model:        req.Model,
		Kind:         text_refiner.TextKind(req.Kind).Normalize(),
		Instruction:  req.Instruction,
		OriginalText: req.Text,
		MaxTokens:    req.MaxTokens,
		Temperature:  req.Temperature,
	})
	if err != nil {
		h.writeRefineError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toRefineResponse(out))
}

// @Summary		Listar modelos de IA disponíveis
// @Description	Retorna os modelos de linguagem disponíveis para o refinamento de texto, com os respectivos preços por token, para seleção no editor.
// @Tags			Refinador de texto
// @Produce		json
// @Success		200	{array}		ModelInfoResponse
// @Failure		502	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/text-refiner/models [get]
func (h *TextRefinerHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	if h.aiService == nil {
		response.WriteSuccess(w, http.StatusOK, []ModelInfoResponse{})
		return
	}
	models, err := h.aiService.GetModelsWithPricing(r.Context())
	if err != nil {
		response.WriteError(w, http.StatusBadGateway, "failed to load models", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toModelInfoResponses(models))
}

func (h *TextRefinerHandler) writeRefineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, text_refiner.ErrTextRequired),
		errors.Is(err, text_refiner.ErrInstructionRequired),
		errors.Is(err, text_refiner.ErrTextTooLong),
		errors.Is(err, text_refiner.ErrInstructionTooLong),
		errors.Is(err, text_refiner.ErrModelNotAllowed):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, text_refiner.ErrInsufficientBalance):
		response.WriteErrorWithCode(w, http.StatusPaymentRequired, "INSUFFICIENT_BALANCE", err.Error(), nil)
	case errors.Is(err, text_refiner.ErrAIUnavailable),
		errors.Is(err, text_refiner.ErrEmptyResponse):
		response.WriteError(w, http.StatusBadGateway, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "failed to refine text", nil)
	}
}
