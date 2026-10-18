package node_executors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/workflow"
)

const (
	defaultHTTPRequestTimeout = 15 * time.Second
	MaxHTTPRequestTimeout     = 30 * time.Second
	maxHTTPResponseBytes      = 64 * 1024
)

var errHTTPEgressNotConfigured = errors.New("outbound HTTP is not configured on this server")

type HTTPEgress struct {
	Client    *http.Client
	LegacyTLS *http.Client
}

func normalizeRequestMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}

	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}

	if raw, ok := value.(string); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}

	return nil
}

type httpRequestExecutor struct {
	egress HTTPEgress
}

func NewHTTPRequestExecutor(egress HTTPEgress) workflow.NodeExecutor {
	return &httpRequestExecutor{egress: egress}
}

func isTLSHandshakeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "remote error: tls") ||
		strings.Contains(msg, "tls: handshake failure") ||
		strings.Contains(msg, "tls: protocol version")
}

func httpRequestTimeout(config map[string]interface{}) time.Duration {
	var seconds float64
	switch v := config["timeout_seconds"].(type) {
	case float64:
		seconds = v
	case int:
		seconds = float64(v)
	}
	if seconds <= 0 {
		return defaultHTTPRequestTimeout
	}
	return min(time.Duration(seconds*float64(time.Second)), MaxHTTPRequestTimeout)
}

func httpFailure(ctx *workflow.NodeContext, label, message string) *workflow.NodeResult {
	nextID := resolveEdgeByLabel(ctx.Graph.OutgoingEdges(ctx.Node.ID), label)
	return &workflow.NodeResult{
		NextNodeID: nextID,
		Output: map[string]interface{}{
			"success":      false,
			"error":        message,
			"next_edge":    label,
			"next_node_id": nextID,
		},
	}
}

func (e *httpRequestExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionHTTPRequest,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared, workflow.NodeScopeVoice},
		Label:       "Requisição HTTP",
		Description: "Faz uma requisição HTTP para um serviço externo.",
		Icon:        "Globe",
		Guidance: workflow.NodeGuidance{
			When: "Para chamar uma API externa (ex.: ViaCEP, CRM, etc.).",
			Behavior: "Monte url, headers e corpo com variáveis (ex.: url com {{node.<id>.<arg>}}). " +
				"Se a resposta for JSON, CADA campo do corpo torna-se uma saída de dados deste nó, acessível adiante via {{node.<id>.<campo>}} (ex.: {{node.n4.logradouro}}).",
			Examples: []string{
				"Após um agente em route com tool 'buscar_cep' (arg cep): url=\"https://viacep.com.br/ws/{{node.n2.cep}}/json/\"; depois envie {{node.n4.logradouro}}, {{node.n4.bairro}}.",
			},
		},
		DefaultConfig: map[string]interface{}{
			"method":  "GET",
			"url":     "",
			"headers": map[string]interface{}{},
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "falha", Label: "Falha", Optional: true},
			{ID: "timeout", Label: "Tempo esgotado", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "status_code", Description: "Código HTTP (ex: 200, 404)"},
			{Key: "body", Description: "Corpo bruto da resposta (string)"},
			{Key: "success", Description: "true se status < 400"},
			{Key: "*", Description: "Se a resposta for JSON, cada campo do objeto é extraído automaticamente. Ex: se o body for {\"bairro\":\"Sé\"}, use {{last.bairro}}"},
		},
		ConfigSchema: []workflow.ConfigField{
			{
				Key:   "method",
				Label: "Método",
				Type:  "select",
				Options: []workflow.ConfigFieldOption{
					{Value: "GET", Label: "GET"},
					{Value: "POST", Label: "POST"},
					{Value: "PUT", Label: "PUT"},
					{Value: "PATCH", Label: "PATCH"},
					{Value: "DELETE", Label: "DELETE"},
					{Value: "HEAD", Label: "HEAD"},
					{Value: "OPTIONS", Label: "OPTIONS"},
				},
				Required: true,
			},
			{Key: "url", Label: "URL", Type: "text", Placeholder: "https://api.example.com/endpoint", Required: true},
			{Key: "headers", Label: "Headers", Type: "keyvalue", Placeholder: "Content-Type: application/json", Description: "Cabeçalhos HTTP em formato chave-valor"},
			{Key: "query_params", Label: "Parâmetros de Query", Type: "keyvalue", Placeholder: "key=value", Description: "Parâmetros adicionados à URL (?key=value)"},
			{Key: "body", Label: "Corpo da Requisição", Type: "textarea", Placeholder: "{}", Description: "Corpo JSON da requisição (suporta variáveis)"},
			{Key: "auth_type", Label: "Autenticação", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "", Label: "Nenhuma"},
				{Value: "bearer", Label: "Bearer Token"},
				{Value: "basic", Label: "Basic Auth"},
				{Value: "api_key", Label: "API Key (Header)"},
			}},
			{Key: "auth_token", Label: "Token / Senha", Type: "text", Placeholder: "Token ou senha (suporta variáveis)", Description: "Bearer token, senha do Basic Auth, ou valor da API Key"},
			{Key: "auth_user", Label: "Usuário (Basic Auth)", Type: "text", Placeholder: "username", Description: "Apenas para Basic Auth"},
			{Key: "auth_key_name", Label: "Nome do Header (API Key)", Type: "text", Placeholder: "X-API-Key", Description: "Nome do header para API Key"},
			{Key: "timeout_seconds", Label: "Timeout (segundos)", Type: "number", Placeholder: "15", Description: "Tempo máximo de espera pela resposta, até 30 segundos"},
			{Key: "capture_variable", Label: "Salvar resposta em variável", Type: "text", Placeholder: "nome_variavel", Description: "Salva o corpo da resposta em uma variável para uso em nós posteriores"},
		},
	}
}

func (e *httpRequestExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	method, _ := ctx.Node.Config["method"].(string)
	rawURL, _ := ctx.Node.Config["url"].(string)

	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)

	if rawURL == "" {
		log.Printf("[http_request] node %s: empty URL, skipping", ctx.Node.ID)
		return httpFailure(ctx, "falha", "URL is empty"), nil
	}
	if e.egress.Client == nil {
		log.Printf("[http_request] node %s: refused, %v", ctx.Node.ID, errHTTPEgressNotConfigured)
		return httpFailure(ctx, "falha", errHTTPEgressNotConfigured.Error()), nil
	}

	rawURL = workflow.Interpolate(rawURL, ctx.State, nil)

	if qp := normalizeRequestMap(ctx.Node.Config["query_params"]); len(qp) > 0 {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr == nil {
			q := parsed.Query()
			for k, v := range qp {
				q.Set(k, workflow.Interpolate(fmt.Sprintf("%v", v), ctx.State, nil))
			}
			parsed.RawQuery = q.Encode()
			rawURL = parsed.String()
		}
	}

	log.Printf("[http_request] node %s: %s %s", ctx.Node.ID, method, rawURL)

	bodyStr := ""
	if bs, ok := ctx.Node.Config["body"].(string); ok && bs != "" {
		bodyStr = workflow.Interpolate(bs, ctx.State, nil)
	}

	headers := normalizeRequestMap(ctx.Node.Config["headers"])
	authType, _ := ctx.Node.Config["auth_type"].(string)
	authToken, _ := ctx.Node.Config["auth_token"].(string)
	if authToken != "" {
		authToken = workflow.Interpolate(authToken, ctx.State, nil)
	}

	callCtx, releaseCall := ctx.CallContext()
	defer releaseCall()
	requestCtx, cancel := context.WithTimeout(callCtx, httpRequestTimeout(ctx.Node.Config))
	defer cancel()

	makeReq := func() (*http.Request, error) {
		var body io.Reader
		if bodyStr != "" {
			body = strings.NewReader(bodyStr)
		}
		req, err := http.NewRequestWithContext(requestCtx, method, rawURL, body)
		if err != nil {
			return nil, err
		}
		if bodyStr != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, workflow.Interpolate(fmt.Sprintf("%v", v), ctx.State, nil))
		}
		switch authType {
		case "bearer":
			if authToken != "" {
				token := strings.TrimSpace(authToken)
				if len(token) >= 7 && strings.EqualFold(token[:7], "bearer ") {
					token = strings.TrimSpace(token[7:])
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
		case "basic":
			authUser, _ := ctx.Node.Config["auth_user"].(string)
			if authUser != "" {
				authUser = workflow.Interpolate(authUser, ctx.State, nil)
			}
			if authUser != "" || authToken != "" {
				req.SetBasicAuth(authUser, authToken)
			}
		case "api_key":
			keyName, _ := ctx.Node.Config["auth_key_name"].(string)
			if keyName == "" {
				keyName = "X-API-Key"
			}
			if authToken != "" {
				req.Header.Set(keyName, authToken)
			}
		}
		return req, nil
	}

	req, err := makeReq()
	if err != nil {
		log.Printf("[http_request] node %s: failed to create request: %v", ctx.Node.ID, err)
		return httpFailure(ctx, "falha", fmt.Sprintf("failed to create request: %v", err)), nil
	}

	resp, err := e.egress.Client.Do(req)
	if err != nil && isTLSHandshakeError(err) && e.egress.LegacyTLS != nil {
		log.Printf("[http_request] node %s: TLS handshake failed (%v), retrying capped at TLS 1.2", ctx.Node.ID, err)
		if req2, mkErr := makeReq(); mkErr == nil {
			resp, err = e.egress.LegacyTLS.Do(req2)
		}
	}
	if err != nil {
		log.Printf("[http_request] node %s: transport error: %v", ctx.Node.ID, err)
		label := "falha"
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			label = "timeout"
		}
		return httpFailure(ctx, label, fmt.Sprintf("transport error: %v", err)), nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBytes))

	output := map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
		"success":     resp.StatusCode < 400,
	}

	if len(respBody) > 500 {
		log.Printf("[http_request] node %s: response body (truncated to 500 chars): %s", ctx.Node.ID, string(respBody[:500]))
	} else {
		log.Printf("[http_request] node %s: response body: %s", ctx.Node.ID, string(respBody))
	}

	var jsonResp interface{}
	if err := json.Unmarshal(respBody, &jsonResp); err == nil {
		output["json"] = jsonResp

		if obj, isObj := jsonResp.(map[string]interface{}); isObj {
			reserved := map[string]bool{"status_code": true, "body": true, "success": true, "json": true, "error": true}
			for k, v := range obj {
				if !reserved[k] {
					output[k] = v
				}
			}
		}
	}

	if captureVar, ok := ctx.Node.Config["capture_variable"].(string); ok && captureVar != "" {
		if jsonResp != nil {
			ctx.State.Set(captureVar, jsonResp)
		} else {
			ctx.State.Set(captureVar, string(respBody))
		}

		ctx.State.Set("_httpmeta_"+captureVar, map[string]interface{}{
			"status_code": resp.StatusCode,
			"success":     resp.StatusCode < 400,
		})
	}

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	var label string
	if resp.StatusCode < 400 {
		label = "sucesso"
	} else {
		label = "falha"
		log.Printf("[http_request] node %s: HTTP %d (non-fatal, workflow continues)", ctx.Node.ID, resp.StatusCode)
		output["error"] = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	nextID := resolveEdgeByLabel(edges, label)
	output["next_edge"] = label
	output["next_node_id"] = nextID
	log.Printf("[http_request] node %s: completed, status=%d label=%s", ctx.Node.ID, resp.StatusCode, label)
	return &workflow.NodeResult{NextNodeID: nextID, Output: output}, nil
}
