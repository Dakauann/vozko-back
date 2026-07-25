package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/http/middleware"
	ucmcp "vozko/usecases/agent/mcp"
)

type AgentMCPRemoteHandler struct {
	RegisterUC *ucmcp.RegisterRemoteUseCase
	StartOAuth *ucmcp.StartOAuth2UseCase
	Remotes    domainmcp.RemoteServerRepository
	Cache      domainmcp.ToolCacheRepository

	Refresh *ucmcp.RefreshOAuth2UseCase
}

func NewAgentMCPRemoteHandler(
	register *ucmcp.RegisterRemoteUseCase,
	start *ucmcp.StartOAuth2UseCase,
	remotes domainmcp.RemoteServerRepository,
	cache domainmcp.ToolCacheRepository,
) *AgentMCPRemoteHandler {
	return &AgentMCPRemoteHandler{RegisterUC: register, StartOAuth: start, Remotes: remotes, Cache: cache}
}

type remoteDTO struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Transport   string `json:"transport"`
	Status      string `json:"status"`
	AuthMode    string `json:"authMode,omitempty"`
}

func remoteToDTO(s *domainmcp.RemoteMCPServer) remoteDTO {
	d := remoteDTO{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Name:        s.Name,
		URL:         s.URL,
		Transport:   string(s.Transport),
		Status:      string(s.Status),
	}
	if s.Credential != nil {
		d.AuthMode = string(s.Credential.Mode)
	}
	return d
}

type registerRemoteRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	AuthMode string `json:"authMode"`
	APIKey   string `json:"apiKey,omitempty"`
}

func (h *AgentMCPRemoteHandler) Register(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	var req registerRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	mode := domainmcp.AuthMode(req.AuthMode)
	if !mode.Valid() {
		response.WriteError(w, http.StatusBadRequest, "invalid authMode", map[string]string{"authMode": "none|api_key|oauth2"})
		return
	}
	s, err := h.RegisterUC.Execute(r.Context(), ucmcp.RegisterRemoteInput{
		ID:          uuid.NewString(),
		WorkspaceID: ws,
		Name:        req.Name,
		URL:         req.URL,
		AuthMode:    mode,
		APIKey:      req.APIKey,
	})
	if err != nil {
		(&AgentMCPHandler{}).writeDomainError(w, err)
		return
	}
	resp := map[string]any{"server": remoteToDTO(s.Server)}
	if s.AuthorizeURL != "" {
		resp["authorizeUrl"] = s.AuthorizeURL
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *AgentMCPRemoteHandler) List(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	list, err := h.Remotes.ListByWorkspace(r.Context(), ws)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	if h.Refresh != nil {
		now := domainmcp.Now()
		for _, s := range list {
			if s == nil || s.Credential == nil {
				continue
			}
			if s.Credential.Mode != domainmcp.AuthOAuth2 {
				continue
			}
			if !s.Credential.ShouldRefresh(now) {
				continue
			}
			ref := h.Refresh
			wsID, id := ws, s.ID
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				_, _ = ref.RefreshRemote(ctx, wsID, id)
			}()
		}
	}
	out := make([]remoteDTO, len(list))
	for i, s := range list {
		out[i] = remoteToDTO(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *AgentMCPRemoteHandler) Get(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	s, err := h.Remotes.Get(r.Context(), ws, id)
	if err != nil {
		(&AgentMCPHandler{}).writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, remoteToDTO(s))
}

func (h *AgentMCPRemoteHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.Cache.Purge(r.Context(), "remote:"+id, ws); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	if err := h.Remotes.Delete(r.Context(), ws, id); err != nil {
		(&AgentMCPHandler{}).writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AgentMCPRemoteHandler) StartOAuthFlow(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.StartOAuth == nil {
		response.WriteError(w, http.StatusInternalServerError, "oauth not configured", nil)
		return
	}
	id := mux.Vars(r)["id"]
	out, err := h.StartOAuth.Execute(r.Context(), ucmcp.StartOAuth2Input{
		Kind:        "remote",
		WorkspaceID: ws,
		BindingID:   id,
	})
	if err != nil {
		(&AgentMCPHandler{}).writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizeUrl": out.AuthorizeURL, "state": out.State})
}
