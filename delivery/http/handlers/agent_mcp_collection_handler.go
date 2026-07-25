package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/http/middleware"
	ucmcp "vozko/usecases/agent/mcp"
)

type AgentMCPCollectionHandler struct {
	Service *ucmcp.CollectionService
}

func NewAgentMCPCollectionHandler(svc *ucmcp.CollectionService) *AgentMCPCollectionHandler {
	if svc.NewID == nil {
		svc.NewID = uuid.NewString
	}
	return &AgentMCPCollectionHandler{Service: svc}
}

type collectionMemberDTO struct {
	Kind  string `json:"kind"`
	RefID string `json:"refId"`
}

type collectionDTO struct {
	ID          string                `json:"id"`
	WorkspaceID string                `json:"workspaceId"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Members     []collectionMemberDTO `json:"members"`
	CreatedAt   string                `json:"createdAt"`
	UpdatedAt   string                `json:"updatedAt"`
}

func collectionToDTO(c *domainmcp.MCPCollection) collectionDTO {
	members := make([]collectionMemberDTO, 0, len(c.Members))
	for _, m := range c.Members {
		members = append(members, collectionMemberDTO{Kind: string(m.Kind), RefID: m.RefID})
	}
	return collectionDTO{
		ID:          c.ID,
		WorkspaceID: c.WorkspaceID,
		Name:        c.Name,
		Description: c.Description,
		Members:     members,
		CreatedAt:   c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

type collectionRequest struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Members     []collectionMemberDTO `json:"members"`
}

func (req collectionRequest) domainMembers() []domainmcp.CollectionMember {
	out := make([]domainmcp.CollectionMember, 0, len(req.Members))
	for _, m := range req.Members {
		out = append(out, domainmcp.CollectionMember{
			Kind:  domainmcp.CollectionMemberKind(m.Kind),
			RefID: m.RefID,
		})
	}
	return out
}

func (h *AgentMCPCollectionHandler) List(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	list, err := h.Service.List(r.Context(), ws)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	out := make([]collectionDTO, len(list))
	for i, c := range list {
		out[i] = collectionToDTO(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *AgentMCPCollectionHandler) Get(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	c, err := h.Service.Get(r.Context(), ws, id)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, collectionToDTO(c))
}

func (h *AgentMCPCollectionHandler) Create(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	var req collectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	c, err := h.Service.Create(r.Context(), ucmcp.CreateCollectionInput{
		WorkspaceID: ws,
		Name:        req.Name,
		Description: req.Description,
		Members:     req.domainMembers(),
	})
	if err != nil {
		h.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, collectionToDTO(c))
}

func (h *AgentMCPCollectionHandler) Update(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	var req collectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	c, err := h.Service.Update(r.Context(), ucmcp.UpdateCollectionInput{
		WorkspaceID: ws,
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		Members:     req.domainMembers(),
	})
	if err != nil {
		h.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, collectionToDTO(c))
}

func (h *AgentMCPCollectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.Service.Delete(r.Context(), ws, id); err != nil {
		h.writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AgentMCPCollectionHandler) writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domainmcp.ErrCollectionNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, domainmcp.ErrWorkspaceRequired),
		errors.Is(err, domainmcp.ErrNameRequired),
		errors.Is(err, domainmcp.ErrInvalidCollectionMemberKind),
		errors.Is(err, domainmcp.ErrCollectionMemberRefIDRequired),
		errors.Is(err, domainmcp.ErrCollectionMemberNotFound):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, domainmcp.ErrDuplicate):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
	}
}
