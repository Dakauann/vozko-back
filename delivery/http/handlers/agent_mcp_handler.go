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

type AgentMCPHandler struct {
	Catalog     ucmcp.BuiltinCatalog
	EnableUC    *ucmcp.EnableBuiltinUseCase
	ConfigureUC *ucmcp.ConfigureBuiltinAuthUseCase
	Bindings    domainmcp.BuiltinBindingRepository
	StartOAuth  *ucmcp.StartOAuth2UseCase
}

func NewAgentMCPHandler(
	cat ucmcp.BuiltinCatalog,
	enable *ucmcp.EnableBuiltinUseCase,
	configure *ucmcp.ConfigureBuiltinAuthUseCase,
	bindings domainmcp.BuiltinBindingRepository,
	startOAuth *ucmcp.StartOAuth2UseCase,
) *AgentMCPHandler {
	return &AgentMCPHandler{
		Catalog:     cat,
		EnableUC:    enable,
		ConfigureUC: configure,
		Bindings:    bindings,
		StartOAuth:  startOAuth,
	}
}

type builtinDTO struct {
	Key         string   `json:"key"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	AuthMode    string   `json:"authMode"`
	Scopes      []string `json:"scopes,omitempty"`
}

type bindingDTO struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	ServerKey   string `json:"serverKey"`
	DisplayName string `json:"displayName"`
	Label       string `json:"label,omitempty"`
	Status      string `json:"status"`
}

func bindingToDTO(b *domainmcp.BuiltinBinding) bindingDTO {
	return bindingDTO{
		ID:          b.ID,
		WorkspaceID: b.WorkspaceID,
		ServerKey:   b.ServerKey,
		DisplayName: b.DisplayName,
		Label:       b.Label,
		Status:      string(b.Status),
	}
}

func (h *AgentMCPHandler) ListCatalog(w http.ResponseWriter, _ *http.Request) {
	descs := h.Catalog.All()
	out := make([]builtinDTO, len(descs))
	for i, d := range descs {
		out[i] = builtinDTO{
			Key:         d.Key,
			DisplayName: d.DisplayName,
			Description: d.Description,
			AuthMode:    string(d.AuthSpec.Mode),
			Scopes:      d.AuthSpec.Scopes,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *AgentMCPHandler) ListBindings(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	list, err := h.Bindings.ListByWorkspace(r.Context(), ws)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	out := make([]bindingDTO, len(list))
	for i, b := range list {
		out[i] = bindingToDTO(b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type enableBuiltinRequest struct {
	ServerKey string `json:"serverKey"`
	Label     string `json:"label,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
}

func (h *AgentMCPHandler) Enable(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	var req enableBuiltinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServerKey == "" {
		response.WriteError(w, http.StatusBadRequest, "invalid body", map[string]string{"serverKey": "string (required)"})
		return
	}
	b, err := h.EnableUC.Execute(r.Context(), ucmcp.EnableBuiltinInput{
		WorkspaceID: ws,
		ServerKey:   req.ServerKey,
		BindingID:   uuid.NewString(),
		Label:       req.Label,
		APIKey:      req.APIKey,
	})
	if err != nil {
		h.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bindingToDTO(b))
}

type configureAPIKeyRequest struct {
	APIKey string `json:"apiKey"`
}

func (h *AgentMCPHandler) ConfigureAPIKey(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	var req configureAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		response.WriteError(w, http.StatusBadRequest, "invalid body", map[string]string{"apiKey": "string (required)"})
		return
	}
	b, err := h.ConfigureUC.ExecuteAPIKey(r.Context(), ucmcp.ConfigureBuiltinAPIKeyInput{
		WorkspaceID: ws,
		BindingID:   id,
		APIKey:      req.APIKey,
	})
	if err != nil {
		h.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bindingToDTO(b))
}

func (h *AgentMCPHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.Bindings.Delete(r.Context(), ws, id); err != nil {
		h.writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AgentMCPHandler) StartOAuth2(w http.ResponseWriter, r *http.Request) {
	ws := middleware.GetWorkspaceID(r)
	if ws == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	id := mux.Vars(r)["id"]
	out, err := h.StartOAuth.Execute(r.Context(), ucmcp.StartOAuth2Input{
		Kind:        "builtin",
		WorkspaceID: ws,
		BindingID:   id,
	})
	if err != nil {
		h.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizeUrl": out.AuthorizeURL, "state": out.State})
}

func (h *AgentMCPHandler) writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domainmcp.ErrBindingNotFound), errors.Is(err, domainmcp.ErrRemoteServerNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, domainmcp.ErrWorkspaceRequired),
		errors.Is(err, domainmcp.ErrServerKeyRequired),
		errors.Is(err, domainmcp.ErrURLRequired),
		errors.Is(err, domainmcp.ErrURLNotHTTPS),
		errors.Is(err, domainmcp.ErrUnknownAuthMode),
		errors.Is(err, domainmcp.ErrCredentialRequired),
		errors.Is(err, domainmcp.ErrNameRequired),
		errors.Is(err, domainmcp.ErrToolNameMalformed):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, domainmcp.ErrDuplicate):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
