package advertisinghttp

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type DraftUpdateRequest struct {
	Draft   advertising.AdDraft `json:"draft"`
	Version int                 `json:"version"`
}

type DraftPublishRequest struct {
	Version int `json:"version"`
}

type DraftCopyRequest struct {
	Name string `json:"name"`
}

type DraftRowResponse struct {
	Key          string                       `json:"key"`
	Level        string                       `json:"level"`
	Name         string                       `json:"name"`
	ParentKey    string                       `json:"parentKey,omitempty"`
	ParentMetaID string                       `json:"parentMetaId,omitempty"`
	Budget       *advertising.Budget          `json:"budget,omitempty"`
	Objective    advertising.Objective        `json:"objective,omitempty"`
	Destination  advertising.Destination      `json:"destination,omitempty"`
	Goal         advertising.OptimizationGoal `json:"goal,omitempty"`
}

type DraftResponse struct {
	ID          string              `json:"id"`
	AdAccountID string              `json:"adAccountId"`
	Draft       advertising.AdDraft `json:"draft"`
	Version     int                 `json:"version"`
	State       string              `json:"state"`
	Job         *JobResponse        `json:"job,omitempty"`
	Rows        []DraftRowResponse  `json:"rows"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

type DraftListResponse struct {
	Drafts      []DraftResponse `json:"drafts"`
	ObjectCount int             `json:"objectCount"`
}

type DiscardResponse struct {
	Discarded int `json:"discarded"`
}

// @Summary		Listar rascunhos da conta
// @Description	Rascunhos guardados no Vozko (a Meta não expõe os rascunhos dela pela API). Cada rascunho é uma árvore de campanha, conjunto e anúncios; rows traz as linhas que entram na tabela como "Em rascunho" e objectCount soma essas linhas, o número de "Conferir e publicar". Rascunhos já publicados saem da lista.
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{object}	DraftListResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/drafts [get]
func (h *Handler) ListDrafts(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.Drafts.List(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list the ad drafts")
		return
	}
	response.WriteSuccess(w, http.StatusOK, DraftListResponse{Drafts: presentAll(list.Drafts, presentDraft), ObjectCount: list.ObjectCount})
}

// @Summary		Detalhe de rascunho
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID do rascunho"
// @Success		200	{object}	DraftResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts/{id} [get]
func (h *Handler) GetDraft(w http.ResponseWriter, r *http.Request) {
	view, err := h.d.Drafts.Get(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to load the ad draft")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentDraft(*view))
}

// @Summary		Criar rascunho
// @Description	Guarda a árvore como está, mesmo incompleta; só confere a conta do workspace e os limites de tamanho. A validação completa fica em POST /ads/drafts/validate e na publicação.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		DraftRequest	true	"rascunho"
// @Success		201		{object}	DraftResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts [post]
func (h *Handler) CreateDraft(w http.ResponseWriter, r *http.Request) {
	var req DraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.d.Drafts.Create(r.Context(), workspaceOf(r), personOf(r).UserID, req.Draft)
	if err != nil {
		writeError(w, err, "Failed to create the ad draft")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentDraft(*view))
}

// @Summary		Salvar rascunho
// @Description	Substitui a árvore. version é a versão lida; se outra pessoa salvou antes, volta 409 draft_changed. Um rascunho sendo publicado volta 409 draft_publishing.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do rascunho"
// @Param			body	body		DraftUpdateRequest	true	"rascunho e versão"
// @Success		200		{object}	DraftResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts/{id} [put]
func (h *Handler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	var req DraftUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.d.Drafts.Update(r.Context(), workspaceOf(r), personOf(r).UserID, mux.Vars(r)["id"], req.Version, req.Draft)
	if err != nil {
		writeError(w, err, "Failed to save the ad draft")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentDraft(*view))
}

// @Summary		Duplicar rascunho
// @Description	Cria outro rascunho com a mesma árvore. name renomeia o nível mais alto que o rascunho cria (campanha, conjunto ou o anúncio único); vazio mantém os nomes.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do rascunho"
// @Param			body	body		DraftCopyRequest	true	"nome da cópia"
// @Success		201		{object}	DraftResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts/{id}/copies [post]
func (h *Handler) DuplicateDraft(w http.ResponseWriter, r *http.Request) {
	var req DraftCopyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.d.Drafts.Duplicate(r.Context(), workspaceOf(r), personOf(r).UserID, mux.Vars(r)["id"], req.Name)
	if err != nil {
		writeError(w, err, "Failed to duplicate the ad draft")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentDraft(*view))
}

// @Summary		Excluir rascunho
// @Tags			Anúncios
// @Param			id	path	string	true	"ID do rascunho"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts/{id} [delete]
func (h *Handler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Drafts.Delete(r.Context(), workspaceOf(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to delete the ad draft")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Descartar rascunhos da conta
// @Description	Exclui todos os rascunhos da conta, menos os que estão sendo publicados.
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{object}	DiscardResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/drafts/discard [post]
func (h *Handler) DiscardDrafts(w http.ResponseWriter, r *http.Request) {
	discarded, err := h.d.Drafts.Discard(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to discard the ad drafts")
		return
	}
	response.WriteSuccess(w, http.StatusOK, DiscardResponse{Discarded: discarded})
}

// @Summary		Publicar rascunho
// @Description	Publica a versão conferida (version; outra versão volta 409 draft_changed) pelo mesmo caminho de POST /ads/publish (taxa por anúncio, conta pronta, criação pausada e ligação no fim). Enquanto publica, o rascunho não muda nem é publicado de novo; publicado, ele sai da lista; com falha, volta a ser editável.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do rascunho"
// @Param			body	body		DraftPublishRequest	true	"versão conferida"
// @Success		201	{object}	JobResponse
// @Failure		402	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts/{id}/publish [post]
func (h *Handler) PublishDraft(w http.ResponseWriter, r *http.Request) {
	var req DraftPublishRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	job, err := h.d.Drafts.Publish(r.Context(), workspaceOf(r), personOf(r).UserID, mux.Vars(r)["id"], req.Version, advertising.ActorPerson)
	if err != nil {
		writeError(w, err, "Failed to publish the ad draft")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentJob(job))
}

func presentDraft(v adsuc.DraftView) DraftResponse {
	d := v.Draft
	out := DraftResponse{
		ID: d.ID, AdAccountID: d.AdAccountID, Draft: d.Content, Version: d.Version, State: string(v.State),
		Rows: presentAll(v.Rows(), presentDraftRow), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	if v.Job != nil {
		job := presentJob(v.Job)
		out.Job = &job
	}
	return out
}

func presentDraftRow(row advertising.DraftRow) DraftRowResponse {
	return DraftRowResponse{
		Key: row.Key, Level: string(row.Level), Name: row.Name, ParentKey: row.ParentKey, ParentMetaID: row.ParentMetaID,
		Budget: row.Budget, Objective: row.Objective, Destination: row.Destination, Goal: row.Goal,
	}
}
