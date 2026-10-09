package tools_usecase

import (
	"context"
	"errors"

	"vozko/domain/cep"
	"vozko/domain/tools"
)

const ValidateCEPToolName = "validate_cep"

const validateCEPFormatGuide = "CEP deve conter 8 dígitos: NNNNN-NNN. Exemplos: 01310-100, 20031-170. Aceita com ou sem hífen; pontos e espaços são ignorados."

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
		Description:        "Verifica e normaliza um CEP (Código de Endereçamento Postal). Formato correto: 8 dígitos (NNNNN-NNN). Aceita com ou sem hífen e espaços. Retorna o endereço encontrado (logradouro, complemento, bairro, cidade, UF e código IBGE do município).",
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
	if t.cepSearchUseCase == nil {
		return tools.ExecutionResult{Result: "consulta de CEP não configurada", IsError: true}, nil
	}

	code, err := cep.Parse(raw)
	if err != nil {
		return tools.ExecutionResult{
			Result: map[string]interface{}{
				"isValid":       false,
				"input":         raw,
				"normalizedCEP": "",
				"formatGuide":   validateCEPFormatGuide,
				"error":         "Formato de CEP inválido",
			},
		}, nil
	}

	result := map[string]interface{}{
		"isValid":       true,
		"normalizedCEP": cep.Format(code),
		"formatGuide":   validateCEPFormatGuide,
	}
	info, err := t.cepSearchUseCase.Execute(ctx, code)
	switch {
	case errors.Is(err, cep.ErrNotFound):
		result["found"] = false
		result["error"] = "CEP não encontrado"
	case err != nil:
		result["error"] = "Consulta de CEP indisponível no momento"
	default:
		result["address"] = map[string]interface{}{
			"cep":         cep.Format(info.Cep),
			"logradouro":  info.Logradouro,
			"complemento": info.Complement,
			"bairro":      info.Bairro,
			"localidade":  info.Localidade,
			"uf":          info.Uf,
			"ibge":        info.IBGE,
		}
	}
	return tools.ExecutionResult{Result: result}, nil
}

func (t *validateCEPTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.Execute(ctx, params)
}
