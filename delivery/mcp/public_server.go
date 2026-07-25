package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vozko/brand"
	domainmcp "vozko/domain/agent/mcp"
	ucmcp "vozko/usecases/agent/mcp"
)

const ProtocolVersion = "2025-06-18"

type WorkspaceResolver interface {
	Resolve(ctx context.Context, bearer string) (string, error)
}

type Server struct {
	Registry *ucmcp.Registry
	Call     *ucmcp.CallToolUseCase
	Auth     WorkspaceResolver

	AllowedOrigins []string
}

func New(reg *ucmcp.Registry, call *ucmcp.CallToolUseCase, auth WorkspaceResolver) *Server {
	return &Server{Registry: reg, Call: call, Auth: auth}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && len(s.AllowedOrigins) > 0 {
		ok := false
		for _, o := range s.AllowedOrigins {
			if strings.EqualFold(o, origin) {
				ok = true
				break
			}
		}
		if !ok {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
	}

	if r.Method != http.MethodPost {
		s.wwwAuthenticate(w, r)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	bearer := extractBearer(r.Header.Get("Authorization"))
	if bearer == "" {
		s.wwwAuthenticate(w, r)
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	ws, err := s.Auth.Resolve(r.Context(), bearer)
	if err != nil || ws == "" {
		s.wwwAuthenticate(w, r)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error: "+err.Error())
		return
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, -32600, "invalid jsonrpc version")
		return
	}

	if req.Method != "initialize" && !strings.HasPrefix(req.Method, "notifications/") {
		if v := r.Header.Get("MCP-Protocol-Version"); v != "" && v != ProtocolVersion {
			http.Error(w, "unsupported MCP-Protocol-Version", http.StatusBadRequest)
			return
		}
	}

	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		writeRPCResult(w, req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    brand.Active().Name,
				"title":   brand.Active().Name + " MCP Gateway",
				"version": "1.0.0",
			},
			"instructions": "Tools are namespaced as \"<sourceID>.<name>\".",
		})
	case "tools/list":
		s.handleList(w, r.Context(), req.ID, ws)
	case "tools/call":
		s.handleCall(w, r.Context(), req.ID, ws, req.Params)
	case "ping":
		writeRPCResult(w, req.ID, map[string]any{})
	default:
		writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) handleList(w http.ResponseWriter, ctx context.Context, id json.RawMessage, ws string) {
	sources, err := s.Registry.Sources(ctx, domainmcp.WorkspaceID(ws))
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	type outTool struct {
		Name        string          `json:"name"`
		Title       string          `json:"title,omitempty"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	out := struct {
		Tools []outTool `json:"tools"`
	}{Tools: []outTool{}}
	for _, src := range sources {
		tools, err := src.ListTools(ctx, domainmcp.WorkspaceID(ws))
		if err != nil {
			writeRPCError(w, id, -32000, err.Error())
			return
		}
		for _, t := range tools {
			schema := json.RawMessage(t.InputSchema)
			if len(schema) == 0 {

				schema = json.RawMessage(`{"type":"object"}`)
			}
			out.Tools = append(out.Tools, outTool{
				Name:        src.ID() + "." + t.Name,
				Title:       t.Title,
				Description: t.Description,
				InputSchema: schema,
			})
		}
	}
	writeRPCResult(w, id, out)
}

func (s *Server) handleCall(w http.ResponseWriter, ctx context.Context, id json.RawMessage, ws string, params json.RawMessage) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(params) == 0 {
		writeRPCError(w, id, -32602, "invalid params: missing")
		return
	}
	if err := json.Unmarshal(params, &p); err != nil {
		writeRPCError(w, id, -32602, "invalid params: "+err.Error())
		return
	}
	if p.Name == "" {
		writeRPCError(w, id, -32602, "invalid params: name required")
		return
	}
	res, err := s.Call.Execute(ctx, ucmcp.CallToolInput{WorkspaceID: ws, Name: p.Name, Args: p.Arguments})
	if err != nil {

		code := -32000
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "unknown") || strings.Contains(low, "not found") || strings.Contains(low, "invalid") {
			code = -32602
		}
		writeRPCError(w, id, code, err.Error())
		return
	}
	type outContent struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		Data     string `json:"data,omitempty"`
		MimeType string `json:"mimeType,omitempty"`
		URI      string `json:"uri,omitempty"`
	}
	out := struct {
		Content []outContent `json:"content"`
		IsError bool         `json:"isError,omitempty"`
	}{IsError: res.IsError, Content: []outContent{}}
	for _, c := range res.Content {
		oc := outContent{Type: c.Type}
		switch c.Type {
		case "text":
			oc.Text = c.Text
		case "image", "audio":
			oc.Data = string(c.Data)
			oc.MimeType = c.Text
		case "resource_link":
			oc.URI = c.Text
		default:
			oc.Text = c.Text
			if len(c.Data) > 0 {
				oc.Data = string(c.Data)
			}
		}
		out.Content = append(out.Content, oc)
	}
	writeRPCResult(w, id, out)
}

type WellKnownProtectedResource struct {
	Resource             string
	AuthorizationServers []string
	ScopesSupported      []string
}

func (h *WellKnownProtectedResource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"resource":                 h.Resource,
		"authorization_servers":    h.AuthorizationServers,
		"scopes_supported":         h.ScopesSupported,
		"bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) wwwAuthenticate(w http.ResponseWriter, r *http.Request) {
	scheme := "https"
	host := r.Host
	if r.TLS == nil {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		} else if isLocalhost(host) {
			scheme = "http"
		}
	}
	w.Header().Set(
		"WWW-Authenticate",
		fmt.Sprintf(`Bearer resource_metadata="%s://%s/.well-known/oauth-protected-resource"`, scheme, host),
	)
}

func isLocalhost(host string) bool {
	h := host
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	h = strings.ToLower(h)
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func extractBearer(h string) string {
	h = strings.TrimSpace(h)
	if len(h) < 7 {
		return ""
	}
	if !strings.EqualFold(h[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	env := map[string]any{"jsonrpc": "2.0", "result": result}
	if len(id) > 0 {
		env["id"] = json.RawMessage(id)
	} else {
		env["id"] = nil
	}
	_ = json.NewEncoder(w).Encode(env)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	env := map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": code, "message": msg},
	}
	if len(id) > 0 {
		env["id"] = json.RawMessage(id)
	} else {
		env["id"] = nil
	}
	_ = json.NewEncoder(w).Encode(env)
}

var ErrUnauthorized = errors.New("unauthorized")
