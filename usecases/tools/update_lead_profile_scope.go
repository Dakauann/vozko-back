package tools_usecase

import (
	"strings"

	"vozko/domain/tools"
)

const (
	ProfileBirthDateParam  = "data_nascimento"
	profileParamsConfigKey = "__profile_params"
)

func BirthDateOnlyProfileConfig() map[string]interface{} {
	return map[string]interface{}{profileParamsConfigKey: []string{ProfileBirthDateParam}}
}

func BirthDateOnlyProfileDefinition(def tools.Definition) tools.Definition {
	out := def
	out.Parameters = map[string]tools.Parameter{}
	if param, ok := def.Parameters[ProfileBirthDateParam]; ok {
		out.Parameters[ProfileBirthDateParam] = param
	}
	out.Required = []string{ProfileBirthDateParam}
	out.DisplayDescription = "Salva no cadastro do lead a data de nascimento que ele informou na conversa."
	out.Description = `Salva no cadastro deste lead a data de nascimento que ele mesmo informou na conversa.

REGRAS:
- Envie só a data que o lead disse; nunca invente nem complete por conta própria.
- Esta ferramenta só preenche a data quando o cadastro está vazio. Se o cadastro já tem outra data, nada é substituído e um atendente precisa confirmar a mudança.`
	return out
}

func profileScope(config map[string]interface{}) (map[string]bool, bool, bool) {
	raw, scoped := config[profileParamsConfigKey]
	if !scoped {
		return nil, false, true
	}
	allowed := map[string]bool{}
	switch params := raw.(type) {
	case []string:
		for _, param := range params {
			allowed[param] = true
		}
	case []interface{}:
		for _, param := range params {
			name, ok := param.(string)
			if !ok {
				return nil, true, false
			}
			allowed[name] = true
		}
	default:
		return nil, true, false
	}
	return allowed, true, true
}

func outsideProfileScope(config, params map[string]interface{}) bool {
	allowed, scoped, valid := profileScope(config)
	if !scoped {
		return false
	}
	if !valid {
		return true
	}
	for key, value := range params {
		if allowed[key] || blankParam(value) {
			continue
		}
		return true
	}
	return false
}

func blankParam(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case map[string]interface{}:
		return len(v) == 0
	}
	return false
}
