package node_executors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/domain/workflowsecret"
	"vozko/usecases/workflow/httptrace"
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

var normalizeRequestMap = workflow.RequestMap

type httpRequestExecutor struct {
	egress  HTTPEgress
	secrets workflowsecret.Resolver
}

func NewHTTPRequestExecutor(egress HTTPEgress, secrets workflowsecret.Resolver) workflow.NodeExecutor {
	return &httpRequestExecutor{egress: egress, secrets: secrets}
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

func httpFailure(ctx *workflow.NodeContext, label, message string, kind workflow.ErrorKind) *workflow.NodeResult {
	result := routeOutput(&workflow.NodeResult{
		ErrorKind: kind,
		Output: map[string]interface{}{
			"success":   false,
			"error":     message,
			"next_edge": label,
		},
	}, outgoingEdges(ctx), label, timeoutFallback(label)...)
	result.Output["next_node_id"] = result.NextNodeID
	return result
}

func (e *httpRequestExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionHTTPRequest,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared, workflow.NodeScopeVoice},
		Label:        "Requisição HTTP",
		Description:  "Faz uma requisição HTTP para um serviço externo.",
		Icon:         "Globe",
		Guidance: workflow.NodeGuidance{
			When: "Para chamar uma API externa (ex.: ViaCEP, CRM, etc.).",
			Behavior: "Monte url, headers e corpo com variáveis (ex.: url com {{node.<id>.<arg>}}). " +
				"Se a resposta for JSON, CADA campo do corpo torna-se uma saída de dados deste nó, acessível adiante via {{node.<id>.<campo>}} (ex.: {{node.n4.logradouro}}). " +
				"Credenciais (senha, token, chave de API) ficam nos segredos do workspace: use {{secret.<nome>}} na url, nos headers, no corpo ou no token; o valor só é lido ao montar a requisição e nunca vai para o estado nem para os logs.",
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
			{Key: "status_code", Description: "Código HTTP (ex: 200, 404)", Label: "Código HTTP", Type: workflow.VariableTypeNumber},
			{Key: "body", Description: "Corpo bruto da resposta (string)", Label: "Corpo da resposta", Type: workflow.VariableTypeText},
			{Key: "success", Description: "true se status < 400", Label: "Deu certo", Type: workflow.VariableTypeBoolean},
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
		slog.Warn("workflow.http_request.empty_url", nodeLogAttrs(ctx)...)
		return httpFailure(ctx, "falha", "URL is empty", workflow.ErrorKindConfig), nil
	}
	if e.egress.Client == nil {
		slog.Warn("workflow.http_request.refused", append(nodeLogAttrs(ctx), slog.String("reason", errHTTPEgressNotConfigured.Error()))...)
		return httpFailure(ctx, "falha", errHTTPEgressNotConfigured.Error(), workflow.ErrorKindConfig), nil
	}

	secretNames := workflow.SecretRefs(ctx.Node.Config)
	secrets, err := resolveNodeSecrets(ctx, e.secrets, secretNames)
	if err == nil {
		err = requireAllSecrets(secretNames, secrets)
	}
	if err != nil {
		return secretFailure(err)
	}
	render := func(text string) string {
		return workflow.InterpolateWithSecrets(text, ctx.State, nil, secrets.values)
	}

	rawURL = render(rawURL)

	if qp := normalizeRequestMap(ctx.Node.Config["query_params"]); len(qp) > 0 {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr == nil {
			q := parsed.Query()
			for k, v := range qp {
				q.Set(k, render(workflow.TemplateText(v)))
			}
			parsed.RawQuery = q.Encode()
			rawURL = parsed.String()
		}
	}

	bodyStr := ""
	if bs, ok := ctx.Node.Config["body"].(string); ok && bs != "" {
		bodyStr = render(bs)
	}

	headers := normalizeRequestMap(ctx.Node.Config["headers"])
	authType, _ := ctx.Node.Config["auth_type"].(string)
	authToken, _ := ctx.Node.Config["auth_token"].(string)
	if authToken != "" {
		authToken = render(authToken)
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
			req.Header.Set(k, render(workflow.TemplateText(v)))
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
				authUser = render(authUser)
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
		slog.Warn("workflow.http_request.invalid_request", append(nodeLogAttrs(ctx), slog.String("method", method), slog.String("error", secrets.scrub.String(transportCause(err))))...)
		return httpFailure(ctx, "falha", secrets.scrub.String(fmt.Sprintf("failed to create request: %v", err)), workflow.ErrorKindConfig), nil
	}
	target := requestTarget(ctx, req, method, secrets)
	exchange := workflow.HTTPExchange{
		Method:         method,
		URL:            req.URL.String(),
		RequestHeaders: httptrace.Headers(req.Header),
		RequestBody:    bodyStr,
	}
	sentAt := time.Now()

	resp, err := e.egress.Client.Do(req)
	if err != nil && isTLSHandshakeError(err) && e.egress.LegacyTLS != nil {
		slog.Info("workflow.http_request.tls_fallback", append(target, slog.String("error", secrets.scrub.String(transportCause(err))))...)
		if req2, mkErr := makeReq(); mkErr == nil {
			exchange.TLSFallback = true
			resp, err = e.egress.LegacyTLS.Do(req2)
		}
	}
	exchange.LatencyMs = time.Since(sentAt).Milliseconds()
	if err != nil {
		label, kind := outputFailure, workflow.ErrorKindTransport
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			label, kind = outputTimeout, workflow.ErrorKindTimeout
		}
		exchange.Error = transportCause(err)
		if errors.Is(err, shared.ErrEgressBlocked) {
			exchange.EgressRefusal = exchange.Error
		}
		ctx.Trace.RecordHTTP(exchange)
		slog.Warn("workflow.http_request.transport_error", append(target, slog.String("label", label), slog.String("error", secrets.scrub.String(transportCause(err))))...)
		return httpFailure(ctx, label, secrets.scrub.String(fmt.Sprintf("transport error: %v", err)), kind), nil
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBytes+1))
	if len(rawBody) > maxHTTPResponseBytes {
		rawBody = rawBody[:maxHTTPResponseBytes]
		exchange.Truncated = true
	}
	respBody := secrets.scrub.String(string(rawBody))
	exchange.URL = resp.Request.URL.String()
	exchange.Status = resp.StatusCode
	exchange.ResponseHeaders = httptrace.Headers(resp.Header)
	exchange.ResponseBody = string(rawBody)
	exchange.Redirects = httptrace.Redirects(resp)
	ctx.Trace.RecordHTTP(exchange)

	output := map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        respBody,
		"success":     resp.StatusCode < 400,
	}

	var jsonResp interface{}
	if err := json.Unmarshal([]byte(respBody), &jsonResp); err == nil {
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
		var captured interface{} = respBody
		if jsonResp != nil {
			captured = jsonResp
		}
		workflow.RecordHTTPCapture(ctx.State, captureVar, captured, resp.StatusCode)
	}

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	var label string
	var kind workflow.ErrorKind
	if resp.StatusCode < 400 {
		label = "sucesso"
	} else {
		label, kind = outputFailure, workflow.ErrorKindHTTPStatus
		output["error"] = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	output["next_edge"] = label
	result := routeOutput(&workflow.NodeResult{Output: output, ErrorKind: kind}, edges, label)
	output["next_node_id"] = result.NextNodeID
	slog.Info("workflow.http_request", append(target, slog.Int("status", resp.StatusCode), slog.String("label", label))...)
	return result, nil
}

func requestTarget(ctx *workflow.NodeContext, req *http.Request, method string, secrets nodeSecrets) []any {
	return append(nodeLogAttrs(ctx),
		slog.String("method", method),
		slog.String("host", secrets.scrub.String(req.URL.Host)),
		slog.String("path", secrets.scrub.String(req.URL.Path)),
	)
}

func transportCause(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
