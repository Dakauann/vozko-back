package handlers

import (
	"net/http"

	"vozko/delivery/http/response"
	ucmcp "vozko/usecases/agent/mcp"
)

type AgentMCPOAuthCallbackHandler struct {
	Complete *ucmcp.CompleteOAuth2UseCase

	SuccessRedirect string
}

func NewAgentMCPOAuthCallbackHandler(complete *ucmcp.CompleteOAuth2UseCase, successRedirect string) *AgentMCPOAuthCallbackHandler {
	return &AgentMCPOAuthCallbackHandler{Complete: complete, SuccessRedirect: successRedirect}
}

func (h *AgentMCPOAuthCallbackHandler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		if h.SuccessRedirect == "" {
			writeOAuthPopupHTML(w, false, errParam)
			return
		}
		response.WriteError(w, http.StatusBadRequest, errParam, nil)
		return
	}
	if code == "" || state == "" {
		if h.SuccessRedirect == "" {
			writeOAuthPopupHTML(w, false, "missing code or state")
			return
		}
		response.WriteError(w, http.StatusBadRequest, "missing code or state", nil)
		return
	}
	if err := h.Complete.Execute(r.Context(), ucmcp.CompleteOAuth2Input{Code: code, State: state}); err != nil {
		if h.SuccessRedirect == "" {
			writeOAuthPopupHTML(w, false, err.Error())
			return
		}
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if h.SuccessRedirect != "" {
		http.Redirect(w, r, h.SuccessRedirect, http.StatusFound)
		return
	}
	writeOAuthPopupHTML(w, true, "")
}

func writeOAuthPopupHTML(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusOK
	if !ok {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	title := "Connected"
	headline := "You can close this window."
	if !ok {
		title = "Authorization failed"
		headline = "Authorization failed. You can close this window."
	}

	safeErr := jsEscape(errMsg)
	payload := `{"source":"mcp-oauth","ok":` + boolStr(ok) + `,"error":"` + safeErr + `"}`
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><meta charset="utf-8"><title>` + title + `</title>
<style>body{font-family:system-ui,-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}.card{padding:2rem 2.5rem;border-radius:1rem;background:#1e293b;text-align:center;max-width:420px;box-shadow:0 10px 30px rgba(0,0,0,.3)}h1{font-size:1.1rem;margin:0 0 .5rem;font-weight:600}p{margin:0;color:#94a3b8;font-size:.9rem}</style>
</head><body>
<div class="card"><h1>` + title + `</h1><p>` + headline + `</p></div>
<script>
try { if (window.opener && !window.opener.closed) { window.opener.postMessage(` + payload + `, "*"); } } catch (e) {}
setTimeout(function(){ try { window.close(); } catch (e) {} }, 400);
</script>
</body></html>`))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func jsEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '"':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '<':
			out = append(out, '\\', 'u', '0', '0', '3', 'c')
		case '>':
			out = append(out, '\\', 'u', '0', '0', '3', 'e')
		case '&':
			out = append(out, '\\', 'u', '0', '0', '2', '6')
		default:
			if c < 0x20 {
				continue
			}
			out = append(out, c)
		}
	}
	return string(out)
}
