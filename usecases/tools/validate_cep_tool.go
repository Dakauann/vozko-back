package tools_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"vozko/domain/cep"
	"vozko/domain/tools"
)

const ValidateCEPToolName = "validate_cep"

type validateCEPTool struct {
	cepSearchUseCase cep.CEPSearchUseCase
}

func NewValidateCEPToolUseCase(cepSearch cep.CEPSearchUseCase) tools.Handler {
	return &validateCEPTool{
		cepSearchUseCase: cepSearch,
	}
}

func (t *validateCEPTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               ValidateCEPToolName,
		DisplayName:        "Validar CEP",
		Description:        "Verifica e normaliza um CEP (Código de Endereçamento Postal). Formato correto: 8 dígitos (NNNNN-NNN). Aceita com ou sem hífen e espaços. Retorna todas as informações disponíveis (logradouro, bairro, cidade, UF, IBGE, GIA, DDD, SIAFI, complemento).",
		DisplayDescription: "Verifica e normaliza um CEP, retornando logradouro, bairro, cidade e estado.",
		Parameters: map[string]tools.Parameter{
			"cep": {
				Type:               "string",
				Description:        "CEP a ser validado (8 dígitos, com ou sem hífen, ex: 01310-100 ou 01310100)",
				DisplayName:        "CEP",
				DisplayDescription: "CEP a ser validado (ex: 01310-100)",
			},
		},
		Required:   []string{"cep"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *validateCEPTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	raw, ok := params["cep"].(string)
	if !ok {
		return tools.ExecutionResult{Result: "parâmetro 'cep' inválido ou ausente", IsError: true}, nil
	}

	cleaned := cleanCEP(raw)
	formatGuide := "CEP deve conter 8 dígitos: NNNNN-NNN. Exemplos: 01310-100, 20031-170. Aceita com ou sem hífen; pontos e espaços são ignorados."

	if !isValidCEP(cleaned) {
		return tools.ExecutionResult{
			Result: map[string]interface{}{
				"isValid":       false,
				"input":         raw,
				"normalizedCEP": "",
				"formatGuide":   formatGuide,
				"error":         "Formato de CEP inválido",
			},
			IsError: false,
		}, nil
	}

	normalized := normalizeCEP(cleaned)

	var basicInfo *cep.CEPInfo
	if t.cepSearchUseCase != nil {
		info, err := t.cepSearchUseCase.Execute(cleaned)
		if err == nil {
			basicInfo = info
		}
	}

	via, err := fetchViaCEP(ctx, cleaned)
	if err != nil {

		result := map[string]interface{}{
			"isValid":       true,
			"normalizedCEP": normalized,
			"formatGuide":   formatGuide,
			"error":         fmt.Sprintf("Falha ao consultar ViaCEP: %v", err),
		}
		if basicInfo != nil {
			result["info"] = map[string]interface{}{
				"cep":         basicInfo.Cep,
				"logradouro":  basicInfo.Logradouro,
				"complemento": basicInfo.Complement,
				"bairro":      basicInfo.Bairro,
				"localidade":  basicInfo.Localidade,
				"uf":          basicInfo.Uf,
			}
		}
		return tools.ExecutionResult{Result: result, IsError: false}, nil
	}

	return tools.ExecutionResult{
		Result: map[string]interface{}{
			"isValid":       true,
			"normalizedCEP": normalized,
			"formatGuide":   formatGuide,
			"viaCepData": map[string]interface{}{
				"cep":         via.Cep,
				"logradouro":  via.Logradouro,
				"complemento": via.Complemento,
				"bairro":      via.Bairro,
				"localidade":  via.Localidade,
				"uf":          via.Uf,
				"ibge":        via.IBGE,
				"gia":         via.GIA,
				"ddd":         via.DDD,
				"siafi":       via.SIAFI,
			},
		},
		IsError: false,
	}, nil
}

func (t *validateCEPTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.Execute(ctx, params)
}

func cleanCEP(s string) string {
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func isValidCEP(s string) bool {
	return regexp.MustCompile(`^\d{8}$`).MatchString(s)
}

func normalizeCEP(s string) string {
	if len(s) != 8 {
		return s
	}
	return s[:5] + "-" + s[5:]
}

type viaCepResponse struct {
	Cep         string `json:"cep"`
	Logradouro  string `json:"logradouro"`
	Complemento string `json:"complemento"`
	Bairro      string `json:"bairro"`
	Localidade  string `json:"localidade"`
	Uf          string `json:"uf"`
	IBGE        string `json:"ibge"`
	GIA         string `json:"gia"`
	DDD         string `json:"ddd"`
	SIAFI       string `json:"siafi"`
	Erro        bool   `json:"erro,omitempty"`
}

func fetchViaCEP(ctx context.Context, cleanedCEP string) (*viaCepResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://viacep.com.br/ws/%s/json/", cleanedCEP), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("consulta ViaCEP falhou: status %d", resp.StatusCode)
	}
	var data viaCepResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.Erro {
		return nil, fmt.Errorf("CEP não encontrado")
	}
	return &data, nil
}
