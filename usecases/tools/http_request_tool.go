package tools_usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vozko/domain/tools"
)

const httpRequestToolName = "http_request"

type httpRequestTool struct {
	httpClient *http.Client
}

func NewHTTPRequestToolUseCase() tools.Handler {
	return &httpRequestTool{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *httpRequestTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               httpRequestToolName,
		DisplayName:        "Requisição HTTP",
		DisplayDescription: "Realiza requisições HTTP para APIs externas, webhooks ou serviços configurados.",
		Description: `Realiza requisições HTTP para um endpoint configurado. Esta ferramenta deve ser configurada no nível do agente com a URL de destino, método, cabeçalhos e schemas de parâmetros.

A IA irá gerar dinamicamente parâmetros de caminho, consulta e corpo baseado nos schemas configurados e no contexto da conversa.

Use esta ferramenta para integrar com APIs externas, webhooks ou serviços.`,
		Parameters: map[string]tools.Parameter{
			"path_values": {
				Type:               "object",
				Description:        "Valores para parâmetros de caminho definidos na URL (ex: {id} -> {\"id\": \"123\"}). Usado para substituir placeholders na URL configurada.",
				DisplayName:        "Parâmetros de Caminho",
				DisplayDescription: "Valores para substituir placeholders na URL",
			},
			"query_values": {
				Type:               "object",
				Description:        "Valores para parâmetros de consulta conforme definido no query_schema. Estes serão adicionados como string de query na URL.",
				DisplayName:        "Parâmetros de Consulta",
				DisplayDescription: "Valores para os parâmetros de consulta da URL",
			},
			"body_values": {
				Type:               "object",
				Description:        "Valores para parâmetros do corpo da requisição conforme definido no body_schema. Será enviado como JSON no corpo da requisição.",
				DisplayName:        "Corpo da Requisição",
				DisplayDescription: "Valores para o corpo da requisição (JSON)",
			},
			"reason": {
				Type:               "string",
				Description:        "Breve descrição do motivo pela qual esta requisição está sendo feita (para registro e contexto).",
				DisplayName:        "Motivo",
				DisplayDescription: "Descrição do motivo da requisição",
			},
		},
		Required:   []string{},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentAction,

		ConfigSchema: map[string]tools.ConfigParameter{
			"url": {
				Type:               "string",
				Description:        "URL do endpoint HTTP. Use {param} para parâmetros de caminho (ex: /api/users/{user_id}). A URL pode conter múltiplos placeholders que serão substituídos dinamicamente.",
				DisplayName:        "URL do endpoint",
				DisplayDescription: "Endereço completo da API, webhook ou serviço externo.",
				Required:           true,
			},
			"method": {
				Type:               "string",
				Description:        "Método HTTP a ser utilizado (GET, POST, PUT, PATCH, DELETE)",
				DisplayName:        "Método HTTP",
				DisplayDescription: "Operação usada em todas as chamadas dessa ferramenta.",
				Default:            "GET",
				Options: []tools.ConfigParameterOption{
					{Value: "GET", Label: "GET"},
					{Value: "POST", Label: "POST"},
					{Value: "PUT", Label: "PUT"},
					{Value: "PATCH", Label: "PATCH"},
					{Value: "DELETE", Label: "DELETE"},
				},
				Required: true,
			},
			"headers": {
				Type:               "object",
				Description:        "Cabeçalhos HTTP estáticos enviados com cada requisição (ex: Authorization, Content-Type, Accept)",
				DisplayName:        "Cabeçalhos fixos",
				DisplayDescription: "Headers enviados automaticamente em cada requisição.",
				Default:            map[string]interface{}{},
				Required:           false,
			},
			"timeout_seconds": {
				Type:               "number",
				Description:        "Tempo limite para a requisição em segundos (padrão: 30 segundos)",
				DisplayName:        "Tempo limite",
				DisplayDescription: "Máximo de segundos antes de cancelar a chamada.",
				Default:            30,
				Required:           false,
			},
			"path_params": {
				Type:               "array",
				Description:        "Define parâmetros de caminho que a IA deve fornecer. Cada item: {name, type, description, required, example}",
				DisplayName:        "Parâmetros de caminho",
				DisplayDescription: "Campos que substituem placeholders como {lead_id} na URL.",
				Default:            []interface{}{},
				Required:           false,
			},
			"query_schema": {
				Type:               "array",
				Description:        "Define parâmetros de consulta que a IA deve fornecer. Cada item: {name, type, description, required, example}",
				DisplayName:        "Parâmetros de consulta",
				DisplayDescription: "Campos que a IA pode preencher na query string.",
				Default:            []interface{}{},
				Required:           false,
			},
			"body_schema": {
				Type:               "array",
				Description:        "Define parâmetros do corpo que a IA deve fornecer. Cada item: {name, type, description, required, example}",
				DisplayName:        "Campos do corpo",
				DisplayDescription: "Campos enviados como JSON no corpo da requisição.",
				Default:            []interface{}{},
				Required:           false,
			},
		},
		RequiredConfig: []string{"url", "method"},
		RequiresConfig: true,
	}
}

func (t *httpRequestTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{
		Result:  nil,
		IsError: true,
	}, fmt.Errorf("http_request tool requires agent-level configuration - use ExecuteWithConfig instead")
}

func (t *httpRequestTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	baseURL, _ := config["url"].(string)
	method, _ := config["method"].(string)
	method = strings.ToUpper(method)

	validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	if !validMethods[method] {
		return tools.ExecutionResult{
			Result:  nil,
			IsError: true,
		}, fmt.Errorf("invalid HTTP method: %s", method)
	}

	url := baseURL
	if pathValues, ok := params["path_values"].(map[string]interface{}); ok {
		for key, val := range pathValues {
			placeholder := "{" + key + "}"
			url = strings.ReplaceAll(url, placeholder, fmt.Sprintf("%v", val))
		}
	}

	if strings.Contains(url, "{") && strings.Contains(url, "}") {
		return tools.ExecutionResult{
			Result:  nil,
			IsError: true,
		}, fmt.Errorf("missing path parameter values in URL: %s", url)
	}

	if queryValues, ok := params["query_values"].(map[string]interface{}); ok && len(queryValues) > 0 {
		queryString := buildQueryString(queryValues)
		if strings.Contains(url, "?") {
			url += "&" + queryString
		} else {
			url += "?" + queryString
		}
	}

	var bodyReader io.Reader
	if bodyValues, ok := params["body_values"].(map[string]interface{}); ok && len(bodyValues) > 0 {
		jsonBody, err := json.Marshal(bodyValues)
		if err != nil {
			return tools.ExecutionResult{
				Result:  nil,
				IsError: true,
			}, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return tools.ExecutionResult{
			Result:  nil,
			IsError: true,
		}, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if headers, ok := config["headers"].(map[string]interface{}); ok {
		for key, val := range headers {
			if strVal, ok := val.(string); ok {
				req.Header.Set(key, strVal)
			}
		}
	}

	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := t.httpClient
	if timeoutSec, ok := config["timeout_seconds"].(float64); ok && timeoutSec > 0 {
		client = &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return tools.ExecutionResult{
			Result:            nil,
			IsError:           true,
			ContextUpdateText: fmt.Sprintf("HTTP request to %s failed: %v", url, err),
		}, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ExecutionResult{
			Result:  nil,
			IsError: true,
		}, fmt.Errorf("failed to read response body: %w", err)
	}

	var responseData interface{}
	if err := json.Unmarshal(respBody, &responseData); err != nil {
		responseData = string(respBody)
	}

	isSuccess := resp.StatusCode >= 200 && resp.StatusCode < 300
	contextUpdate := fmt.Sprintf("HTTP %s request to %s completed with status %d", method, url, resp.StatusCode)

	if reason, ok := params["reason"].(string); ok && reason != "" {
		contextUpdate += fmt.Sprintf(" - Reason: %s", reason)
	}

	return tools.ExecutionResult{
		Result: map[string]interface{}{
			"status_code": resp.StatusCode,
			"headers":     resp.Header,
			"body":        responseData,
			"success":     isSuccess,
		},
		IsError:           !isSuccess,
		ContextUpdateText: contextUpdate,
	}, nil
}

func buildQueryString(params map[string]interface{}) string {
	var parts []string
	for key, val := range params {
		parts = append(parts, fmt.Sprintf("%s=%v", key, val))
	}
	return strings.Join(parts, "&")
}

var _ tools.Handler = (*httpRequestTool)(nil)
