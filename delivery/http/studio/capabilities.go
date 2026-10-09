package studiohttp

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/domain/studio"
)

type CapabilityReporter interface {
	Report(ctx context.Context, report studio.CapabilityReport) error
}

type CapabilitiesRequest struct {
	Backend     string  `json:"backend" enums:"webgl,webgpu,unavailable,disabled"`
	GPUVendor   string  `json:"gpuVendor" example:"Google Inc. (Intel)"`
	GPURenderer string  `json:"gpuRenderer" example:"ANGLE (Intel, Intel(R) Iris(R) Xe Graphics Direct3D11)"`
	WebGPU      bool    `json:"webgpu"`
	Decode      bool    `json:"decode"`
	EncodeVideo bool    `json:"encodeVideo"`
	EncodeAudio bool    `json:"encodeAudio"`
	PixelRatio  float64 `json:"pixelRatio" example:"2"`
	Cores       int     `json:"cores" example:"8"`
	MemoryGB    float64 `json:"memoryGb" example:"8"`
}

type UsageRequest struct {
	Frames          int64            `json:"frames"`
	SlowFrames      int64            `json:"slowFrames"`
	Stalls          int64            `json:"stalls"`
	ContextLosses   int64            `json:"contextLosses"`
	DecodeFallbacks int64            `json:"decodeFallbacks"`
	WorkerFailures  int64            `json:"workerFailures"`
	BrowserExports  int64            `json:"browserExports"`
	ExportFailures  map[string]int64 `json:"exportFailures"`
}

type CapabilityRequest struct {
	Kind         string              `json:"kind" enums:"image,video"`
	Capabilities CapabilitiesRequest `json:"capabilities"`
	Usage        UsageRequest        `json:"usage"`
}

func (b CapabilityRequest) report(sessionID, workspaceID, userID, userAgent string) studio.CapabilityReport {
	c, u := b.Capabilities, b.Usage
	failures := make(map[studio.ExportFailure]int64, len(u.ExportFailures))
	for reason, count := range u.ExportFailures {
		failures[studio.ExportFailure(reason)] = count
	}
	return studio.CapabilityReport{
		SessionID: sessionID, WorkspaceID: workspaceID, UserID: userID, Kind: studio.Kind(b.Kind), UserAgent: userAgent,
		Capabilities: studio.Capabilities{
			Backend: studio.RenderBackend(c.Backend), GPUVendor: c.GPUVendor, GPURenderer: c.GPURenderer, WebGPU: c.WebGPU,
			Decode: c.Decode, EncodeVideo: c.EncodeVideo, EncodeAudio: c.EncodeAudio, PixelRatio: c.PixelRatio, Cores: c.Cores, MemoryGB: c.MemoryGB,
		},
		Usage: studio.Usage{
			Frames: u.Frames, SlowFrames: u.SlowFrames, Stalls: u.Stalls, ContextLosses: u.ContextLosses, DecodeFallbacks: u.DecodeFallbacks,
			WorkerFailures: u.WorkerFailures, BrowserExports: u.BrowserExports, ExportFailures: failures,
		},
	}
}

// @Summary		Registrar o que o navegador do editor consegue fazer
// @Description	Guarda, por sessão do editor do Estúdio, o que o navegador oferece (renderização na GPU, decodificação e codificação de vídeo e áudio, tela e processador) e os contadores de uso da sessão (quadros, quadros lentos, travadas, perdas de contexto da GPU, voltas para o caminho alternativo, exportações concluídas e exportações que falharam, com o motivo de cada falha). Não leva conteúdo de projeto nem mídia. A sessão é um UUID criado pelo editor; enviar de novo a mesma sessão substitui os contadores pelos valores mais recentes. Uma sessão de outra pessoa responde 409 session_taken; valores fora do que um navegador envia respondem 422 invalid_request. O user agent vem do cabeçalho da requisição.
// @Tags			Estúdio
// @Accept			json
// @Param			sessionId	path	string				true	"UUID da sessão do editor"
// @Param			body		body	CapabilityRequest	true	"capacidades e contadores da sessão"
// @Success		204
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/capabilities/{sessionId} [put]
func (h *Handler) ReportCapabilities(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body CapabilityRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if err := h.capabilities.Report(r.Context(), body.report(mux.Vars(r)["sessionId"], workspaceID, requesterOf(r), r.UserAgent())); err != nil {
		writeError(w, err, "Failed to record the editor capabilities")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
