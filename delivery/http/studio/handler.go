package studiohttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	mediagenhttp "vozko/delivery/http/mediagen"
	"vozko/delivery/http/response"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/infra/http/middleware"
)

const defaultPageSize = 30

type Service interface {
	Create(ctx context.Context, workspaceID, userID string, kind studio.Kind, name string, document json.RawMessage) (*studio.Project, error)
	List(ctx context.Context, q studio.ListQuery) ([]studio.Summary, int64, error)
	Get(ctx context.Context, workspaceID, id string) (*studio.Project, error)
	Save(ctx context.Context, workspaceID, id string, expectedVersion int64, change studio.Change) (*studio.Project, error)
	Archive(ctx context.Context, workspaceID, id string) error
	Export(ctx context.Context, workspaceID, userID, id string, version int64, rasters map[string]string) (*mediagen.Job, error)
}

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

type CreateRequest struct {
	Kind     string          `json:"kind" enums:"image,video"`
	Name     string          `json:"name" example:"Post de segunda"`
	Document json.RawMessage `json:"document" swaggertype:"object"`
}

type SaveRequest struct {
	Name     *string         `json:"name,omitempty"`
	Document json.RawMessage `json:"document,omitempty" swaggertype:"object"`
}

type ExportRequest struct {
	Version int64             `json:"version"`
	Rasters map[string]string `json:"rasters"`
}

type ProjectResponse struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind" enums:"image,video"`
	Name      string          `json:"name"`
	Document  json.RawMessage `json:"document" swaggertype:"object"`
	Version   int64           `json:"version"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type SummaryResponse struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind" enums:"image,video"`
	Name      string    `json:"name"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConflictResponse struct {
	Code    string          `json:"code" example:"version_conflict"`
	Message string          `json:"message"`
	Current ProjectResponse `json:"current"`
}

type ListResponse struct {
	Items []SummaryResponse `json:"items"`
	Total int64             `json:"total"`
}

func presentProject(p *studio.Project) ProjectResponse {
	return ProjectResponse{ID: p.ID, Kind: string(p.Kind), Name: p.Name, Document: p.Document, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// @Summary		Listar projetos do Estúdio
// @Description	Projetos de imagem e vídeo do workspace, do editado mais recentemente para o mais antigo, sem o documento. kind filtra por tipo.
// @Tags			Estúdio
// @Produce		json
// @Param			kind	query		string	false	"tipo"	Enums(image, video)
// @Param			limit	query		int		false	"até 100, padrão 30"
// @Param			offset	query		int		false	"deslocamento"
// @Success		200		{object}	ListResponse
// @Security		BearerAuth
// @Router			/studio/projects [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit == 0 {
		limit = defaultPageSize
	}
	offset, _ := strconv.Atoi(query.Get("offset"))
	items, total, err := h.svc.List(r.Context(), studio.ListQuery{WorkspaceID: workspaceID, Kind: studio.Kind(query.Get("kind")), Limit: limit, Offset: offset})
	if err != nil {
		writeError(w, err, "Failed to list the projects")
		return
	}
	out := ListResponse{Items: make([]SummaryResponse, 0, len(items)), Total: total}
	for _, s := range items {
		out.Items = append(out.Items, SummaryResponse{ID: s.ID, Kind: string(s.Kind), Name: s.Name, Version: s.Version, UpdatedAt: s.UpdatedAt})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar projeto do Estúdio
// @Description	Cria um projeto de imagem (documento studio.image) ou de vídeo (documento studio.video). O documento é validado inteiro: campos desconhecidos, fontes fora da lista, clipes sobrepostos na mesma faixa ou valores fora dos limites respondem 422 com o código por campo. Até 512 KB.
// @Tags			Estúdio
// @Accept			json
// @Produce		json
// @Param			body	body		CreateRequest	true	"projeto"
// @Success		201		{object}	ProjectResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body CreateRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	p, err := h.svc.Create(r.Context(), workspaceID, requesterOf(r), studio.Kind(body.Kind), body.Name, body.Document)
	if err != nil {
		writeError(w, err, "Failed to create the project")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentProject(p))
}

// @Summary		Abrir projeto do Estúdio
// @Description	O projeto com o documento e a versão atual, que vai no If-Match ao salvar.
// @Tags			Estúdio
// @Produce		json
// @Param			id	path		string	true	"id do projeto"
// @Success		200	{object}	ProjectResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Get(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to load the project")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentProject(p))
}

// @Summary		Salvar projeto do Estúdio
// @Description	Salva o nome e ou o documento a partir da versão informada no cabeçalho If-Match. Se o projeto foi salvo em outro lugar depois dessa versão, responde 409 version_conflict com o projeto atual em current, sem sobrescrever.
// @Tags			Estúdio
// @Accept			json
// @Produce		json
// @Param			id			path		string		true	"id do projeto"
// @Param			If-Match	header		string		true	"versão carregada"
// @Param			body		body		SaveRequest	true	"mudanças"
// @Success		200			{object}	ProjectResponse
// @Failure		409			{object}	ConflictResponse
// @Failure		422			{object}	response.ErrorResponse
// @Failure		428			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects/{id} [patch]
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	version, ok := versionOf(w, r)
	if !ok {
		return
	}
	var body SaveRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	id := mux.Vars(r)["id"]
	p, err := h.svc.Save(r.Context(), workspaceID, id, version, studio.Change{Name: body.Name, Document: body.Document})
	if errors.Is(err, studio.ErrVersionConflict) {
		h.writeConflict(w, r, workspaceID, id)
		return
	}
	if err != nil {
		writeError(w, err, "Failed to save the project")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentProject(p))
}

// @Summary		Arquivar projeto do Estúdio
// @Tags			Estúdio
// @Param			id	path	string	true	"id do projeto"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects/{id} [delete]
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.svc.Archive(r.Context(), workspaceID, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to archive the project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Exportar vídeo do Estúdio
// @Description	Renderiza a versão salva informada (version) como MP4 na fila de processamento. Cada clipe de sobreposição precisa da imagem PNG já gerada pelo editor em rasters (id do clipe para media_id da biblioteca); faltando uma, responde 422 not_rasterized. Responde 202 com o job; acompanhe por GET /media/generations/{id}. Uma exportação igual em andamento devolve o mesmo job.
// @Tags			Estúdio
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"id do projeto"
// @Param			body	body		ExportRequest	true	"versão e sobreposições"
// @Success		202		{object}	mediagenhttp.JobResponse
// @Failure		409		{object}	ConflictResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects/{id}/export [post]
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body ExportRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	id := mux.Vars(r)["id"]
	job, err := h.svc.Export(r.Context(), workspaceID, requesterOf(r), id, body.Version, body.Rasters)
	if errors.Is(err, studio.ErrVersionConflict) {
		h.writeConflict(w, r, workspaceID, id)
		return
	}
	if err != nil {
		writeError(w, err, "Failed to export the project")
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, mediagenhttp.PresentJob(job))
}

func (h *Handler) writeConflict(w http.ResponseWriter, r *http.Request, workspaceID, id string) {
	current, err := h.svc.Get(r.Context(), workspaceID, id)
	if err != nil {
		writeError(w, err, "Failed to load the project")
		return
	}
	response.WriteSuccess(w, http.StatusConflict, ConflictResponse{
		Code: "version_conflict", Message: "O projeto foi salvo em outra aba ou por outra pessoa", Current: presentProject(current),
	})
}

func versionOf(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\"")
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version < 1 {
		response.WriteErrorWithCode(w, http.StatusPreconditionRequired, "version_required", "Informe a versão carregada no cabeçalho If-Match", nil)
		return 0, false
	}
	return version, true
}

func requesterOf(r *http.Request) string {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return ""
	}
	return claims.UserID
}

func writeError(w http.ResponseWriter, err error, fallback string) {
	var invalid *studio.ValidationError
	switch {
	case errors.As(err, &invalid):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_request", "Há campos a corrigir", invalid.Codes())
	case errors.Is(err, studio.ErrProjectNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", "Projeto não encontrado", nil)
	case errors.Is(err, studio.ErrNotRasterized):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "not_rasterized", "Uma sobreposição não foi preparada para a exportação", nil)
	case errors.Is(err, studio.ErrNotVideo):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "not_video", "Só projetos de vídeo são exportados como vídeo", nil)
	case errors.Is(err, studio.ErrCreatorRequired):
		response.WriteErrorWithCode(w, http.StatusUnauthorized, "unauthenticated", "Usuário não identificado", nil)
	default:
		mediagenhttp.WriteError(w, err, fallback)
	}
}
