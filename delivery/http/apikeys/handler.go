package apikeys

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/apikey"
	"vozko/infra/http/middleware"
	apikey_usecase "vozko/usecases/apikey"
)

const (
	CodeNameInvalid      = "api_key_name_invalid"
	CodeScopesInvalid    = "api_key_scopes_invalid"
	CodeLimitReached     = "api_key_limit_reached"
	CodeNotFound         = "api_key_not_found"
	CodeCacheUnavailable = "api_key_cache_unavailable"
	CodeFailed           = "api_keys_failed"
)

var manageRefusals = []response.ErrorRefusal{
	{Err: apikey.ErrNameInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: CodeNameInvalid, Message: "Dê um nome de 1 a 80 caracteres para a chave."}},
	{Err: apikey.ErrScopesInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: CodeScopesInvalid, Message: "Escolha ao menos uma permissão conhecida."}},
	{Err: apikey.ErrLimitReached, Refusal: response.Refusal{Status: http.StatusConflict, Code: CodeLimitReached, Message: "O workspace já tem o máximo de chaves ativas; revogue uma antes de criar outra."}},
	{Err: apikey.ErrNotFound, Refusal: response.Refusal{Status: http.StatusNotFound, Code: CodeNotFound, Message: "Chave não encontrada."}},
	{Err: apikey_usecase.ErrCacheUnavailable, Refusal: response.Refusal{Status: http.StatusServiceUnavailable, Code: CodeCacheUnavailable, Message: "A chave foi revogada, mas a confirmação não terminou; revogue de novo."}},
}

var refusalFailed = response.Refusal{Status: http.StatusInternalServerError, Code: CodeFailed, Message: "Não foi possível concluir a operação com as chaves de API."}

func ManageRefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(manageRefusals), []response.Refusal{refusalFailed})
}

type Keys interface {
	Create(ctx context.Context, in apikey_usecase.CreateInput) (apikey_usecase.Created, error)
	List(ctx context.Context, workspaceID string) ([]apikey.Key, error)
	Revoke(ctx context.Context, workspaceID, id, userID string) (apikey.Key, error)
}

type Handler struct {
	keys Keys
}

func NewHandler(keys Keys) *Handler {
	return &Handler{keys: keys}
}

type CreateKeyRequest struct {
	Name   string   `json:"name" example:"Servidor de produção"`
	Scopes []string `json:"scopes" example:"email:send,email:read"`
}

type KeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix" example:"wsk_k4n2q7xp3m5a"`
	Scopes     []string   `json:"scopes"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

type CreatedKeyResponse struct {
	KeyResponse
	Key string `json:"key" example:"wsk_k4n2q7xp3m5a_Yb2..."`
}

type ScopeResponse struct {
	Scope       string `json:"scope" example:"email:send"`
	Description string `json:"description"`
}

type ProductResponse struct {
	Key    string          `json:"key" example:"email"`
	Scopes []ScopeResponse `json:"scopes"`
}

type KeyListResponse struct {
	Keys     []KeyResponse     `json:"keys"`
	Products []ProductResponse `json:"products"`
}

func keyResponse(k apikey.Key) KeyResponse {
	scopes := make([]string, len(k.Scopes))
	for i, s := range k.Scopes {
		scopes[i] = string(s)
	}
	return KeyResponse{ID: k.ID, Name: k.Name, Prefix: k.Prefix(), Scopes: scopes, CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt}
}

func catalog() []ProductResponse {
	out := make([]ProductResponse, len(apikey.Catalog))
	for i, p := range apikey.Catalog {
		scopes := make([]ScopeResponse, len(p.Scopes))
		for j, s := range p.Scopes {
			scopes[j] = ScopeResponse{Scope: string(s.Scope), Description: s.Description}
		}
		out[i] = ProductResponse{Key: p.Key, Scopes: scopes}
	}
	return out
}

func writeManageRefusal(w http.ResponseWriter, workspaceID string, err error) {
	if response.WriteErrorRefusal(w, err, manageRefusals) {
		return
	}
	log.Printf("[api-keys] workspace %s: %v", workspaceID, err)
	response.WriteRefusal(w, refusalFailed)
}

// @Summary		Listar as chaves de API
// @Description	Lista as chaves de API do workspace, ativas e revogadas, com o prefixo visível, as permissões, quem criou, o último uso (gravado no máximo uma vez por minuto) e a revogação. A chave completa nunca é devolvida depois da criação. `products` traz o catálogo de permissões por produto que uma chave pode receber.
// @Tags			Chaves de API
// @Produce		json
// @Success		200	{object}	KeyListResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse	"api_keys_failed"
// @Security		BearerAuth
// @Router			/api-keys [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	keys, err := h.keys.List(r.Context(), workspaceID)
	if err != nil {
		writeManageRefusal(w, workspaceID, err)
		return
	}
	out := KeyListResponse{Keys: make([]KeyResponse, len(keys)), Products: catalog()}
	for i, k := range keys {
		out.Keys[i] = keyResponse(k)
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar uma chave de API
// @Description	Cria uma chave de API do workspace com as permissões escolhidas (`email:send`, `email:read`). A chave age pelo workspace, nunca por uma pessoa. O valor completo (`key`) aparece só nesta resposta; guarde-o em um cofre, porque o servidor guarda apenas o hash. Recusas: 400 `api_key_name_invalid`, `api_key_scopes_invalid`; 409 `api_key_limit_reached` (no máximo 25 chaves ativas por workspace).
// @Tags			Chaves de API
// @Accept			json
// @Produce		json
// @Param			request	body		CreateKeyRequest	true	"Nome e permissões"
// @Success		201		{object}	CreatedKeyResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse	"api_key_limit_reached"
// @Failure		500		{object}	response.ErrorResponse	"api_keys_failed"
// @Security		BearerAuth
// @Router			/api-keys [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req CreateKeyRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	created, err := h.keys.Create(r.Context(), apikey_usecase.CreateInput{WorkspaceID: workspaceID, UserID: userID, Name: req.Name, Scopes: req.Scopes})
	if err != nil {
		writeManageRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, CreatedKeyResponse{KeyResponse: keyResponse(created.Key), Key: created.Plain})
}

// @Summary		Revogar uma chave de API
// @Description	Revoga a chave na hora: a próxima requisição com ela recebe 401 `api_key_revoked`. Revogar de novo uma chave já revogada responde a mesma chave. Recusas: 404 `api_key_not_found`; 503 `api_key_cache_unavailable` quando a revogação foi gravada mas a confirmação não terminou (repita a chamada).
// @Tags			Chaves de API
// @Produce		json
// @Param			id	path		string	true	"Id da chave"
// @Success		200	{object}	KeyResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse	"api_key_not_found"
// @Failure		503	{object}	response.ErrorResponse	"api_key_cache_unavailable"
// @Security		BearerAuth
// @Router			/api-keys/{id} [delete]
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	id := mux.Vars(r)["id"]
	if _, err := uuid.Parse(id); err != nil {
		writeManageRefusal(w, workspaceID, apikey.ErrNotFound)
		return
	}
	k, err := h.keys.Revoke(r.Context(), workspaceID, id, userID)
	if err != nil {
		writeManageRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, keyResponse(k))
}
