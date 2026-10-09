package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/tools"
	lead_usecase "vozko/usecases/lead"
)

const UpdateLeadProfileToolName = "update_lead_profile"

type LeadProfiles interface {
	Update(ctx context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error)
	WritableFields(workspaceID string) ([]*customfield.Definition, error)
}

type updateLeadProfileTool struct {
	profiles LeadProfiles
}

func NewUpdateLeadProfileTool(profiles LeadProfiles) tools.Handler {
	if profiles == nil {
		return nil
	}
	return &updateLeadProfileTool{profiles: profiles}
}

func (t *updateLeadProfileTool) Definition() tools.Definition {
	text := func(description, display, displayDescription string) tools.Parameter {
		return tools.Parameter{Type: "string", Description: description, DisplayName: display, DisplayDescription: displayDescription}
	}
	return tools.Definition{
		Name:               UpdateLeadProfileToolName,
		DisplayName:        "Atualizar cadastro do lead",
		DisplayDescription: "Salva no cadastro do lead o endereço, o bairro, a data de nascimento e campos personalizados que ele informou na conversa.",
		Description: `Salva no cadastro deste lead dados que ele mesmo informou na conversa: endereço, bairro, data de nascimento e campos personalizados de lead.

QUANDO USAR:
- O lead informou o CEP, o endereço ou o bairro onde mora.
- O lead informou a data de nascimento.
- O lead respondeu algo que corresponde a um campo personalizado de lead do workspace.

REGRAS:
- Envie só o que o lead disse; nunca invente nem complete por conta própria.
- Com CEP, o CEP é conferido e cidade, UF e logradouro vêm dele; informe também número e complemento quando o lead disser.
- Sem CEP, informe cidade e UF junto com o bairro ou o logradouro.
- Esta ferramenta só preenche o que está vazio. Se o cadastro já tem outro valor, nada é substituído e um atendente precisa confirmar a mudança.
- Campos sensíveis nunca são gravados por esta ferramenta.`,
		Parameters: map[string]tools.Parameter{
			"cep":             text("CEP com 8 dígitos, com ou sem hífen (ex.: 01310-100).", "CEP", "CEP do endereço"),
			"logradouro":      text("Rua, avenida ou praça, sem o número.", "Logradouro", "Rua ou avenida"),
			"numero":          text("Número do imóvel.", "Número", "Número do imóvel"),
			"complemento":     text("Complemento (apartamento, bloco, casa).", "Complemento", "Apartamento, bloco"),
			"bairro":          text("Bairro onde o lead mora.", "Bairro", "Bairro"),
			"cidade":          text("Cidade. Obrigatória sem CEP.", "Cidade", "Cidade"),
			"uf":              text("UF com duas letras (ex.: SP). Obrigatória sem CEP.", "UF", "Estado"),
			"data_nascimento": text("Data de nascimento no formato DD/MM/AAAA.", "Data de nascimento", "Data de nascimento do lead"),
			"campos": {
				Type:               "object",
				Description:        "Campos personalizados de lead, como objeto chave: valor (ex.: {\"interesse\": \"alto\"}). Use as chaves dos campos do workspace; seleção aceita só as opções do campo; sim ou não para campos booleanos.",
				DisplayName:        "Campos personalizados",
				DisplayDescription: "Campos personalizados de lead",
			},
		},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentAction,
	}
}

func (t *updateLeadProfileTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *updateLeadProfileTool) ExecuteWithConfig(ctx context.Context, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID, leadID := configString(config, "__workspace_id"), configString(config, "__lead_id")
	if workspaceID == "" {
		return errResult("Não foi possível identificar o contexto da conversa. Esta ferramenta só funciona durante uma conversa."), nil
	}
	if leadID == "" {
		return errResult("Esta conversa ainda não está vinculada a um lead; não é possível atualizar o cadastro."), nil
	}
	if outsideProfileScope(config, params) {
		return errResult("Nesta tarefa só a data de nascimento pode ir para o cadastro do lead; os outros dados ficam para a equipe."), nil
	}
	fields, ok := profileFields(params["campos"])
	if !ok {
		return errResult("O parâmetro 'campos' deve ser um objeto chave: valor, ex.: {\"interesse\": \"alto\"}."), nil
	}
	profile := lead.Profile{
		Address: address.Postal{
			ZipCode: paramText(params, "cep"), Street: paramText(params, "logradouro"), Number: paramText(params, "numero"),
			Complement: paramText(params, "complemento"), District: paramText(params, "bairro"),
			City: paramText(params, "cidade"), State: paramText(params, "uf"),
		},
		BirthDate:    paramText(params, "data_nascimento"),
		CustomFields: fields,
	}
	if profile.Empty() {
		return errResult("Informe ao menos um dado do lead: CEP, endereço, bairro, data de nascimento ou um campo personalizado."), nil
	}
	result, err := t.profiles.Update(ctx, lead_usecase.ProfileUpdate{
		WorkspaceID: workspaceID, LeadID: leadID, Actor: automationActor(ctx, config),
		Source: lead.ProfileFromConversation, Profile: profile,
	})
	if err != nil {
		return t.refusal(workspaceID, err), nil
	}
	return profileAnswer(result), nil
}

func paramText(params map[string]interface{}, key string) string {
	value, _ := params[key].(string)
	return strings.TrimSpace(value)
}

func profileFields(raw interface{}) (map[string]string, bool) {
	if raw == nil {
		return nil, true
	}
	object, ok := raw.(map[string]interface{})
	if !ok {
		return nil, false
	}
	fields := make(map[string]string, len(object))
	for key, value := range object {
		fields[key] = customfield.FormatValue(value)
	}
	return fields, true
}

func profileAnswer(result lead_usecase.ProfileResult) tools.ExecutionResult {
	var parts []string
	if len(result.Changed) > 0 {
		parts = append(parts, "Cadastro do lead atualizado: "+profileFieldNames(result.Changed)+".")
	}
	if len(result.Conflicts) > 0 {
		parts = append(parts, "O cadastro já tinha outro valor em: "+profileFieldNames(result.Conflicts)+". Nada foi substituído; se o lead confirmou a mudança, diga que um atendente vai atualizar o cadastro.")
	}
	if len(parts) == 0 {
		return tools.ExecutionResult{Result: "Nada mudou: o cadastro do lead já tinha essas informações."}
	}
	answer := tools.ExecutionResult{Result: strings.Join(parts, " ")}
	if len(result.Changed) > 0 {
		answer.ContextUpdateText = "Cadastro do lead atualizado"
	}
	return answer
}

func profileFieldNames(fields []string) string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case lead.FieldAddresses:
			names = append(names, "endereço")
		case lead.FieldBirthDate:
			names = append(names, "data de nascimento")
		default:
			key := strings.TrimPrefix(field, lead.CustomFieldName(""))
			names = append(names, "campo "+key)
		}
	}
	return strings.Join(names, ", ")
}

func (t *updateLeadProfileTool) refusal(workspaceID string, err error) tools.ExecutionResult {
	explained, known := lead_usecase.ExplainProfileRefusal(err)
	if !known {
		log.Printf("[UpdateLeadProfile] unexpected error: %v", err)
		return errResult("Não foi possível atualizar o cadastro do lead. Tente novamente.")
	}
	var valueErr *customfield.ValueError
	if errors.As(err, &valueErr) && !errors.Is(err, customfield.ErrValueForbidden) && !errors.Is(err, customfield.ErrValueRequired) {
		explained += " Campos aceitos: " + t.acceptedFields(workspaceID) + "."
	}
	return errResult(explained)
}

func (t *updateLeadProfileTool) acceptedFields(workspaceID string) string {
	writable, err := t.profiles.WritableFields(workspaceID)
	if err != nil {
		log.Printf("[UpdateLeadProfile] listing lead fields failed: %v", err)
		return "indisponível no momento"
	}
	return fieldHints(writable)
}

func fieldHints(defs []*customfield.Definition) string {
	if len(defs) == 0 {
		return "nenhum"
	}
	byKey := make(map[string]*customfield.Definition, len(defs))
	for _, def := range defs {
		if def != nil {
			byKey[def.Key] = def
		}
	}
	hints := make([]string, 0, len(byKey))
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		def := byKey[key]
		switch {
		case len(def.Options) > 0:
			hints = append(hints, fmt.Sprintf("%s (%s)", key, strings.Join(def.Options, ", ")))
		case def.Type == customfield.TypeBoolean:
			hints = append(hints, key+" (sim ou não)")
		case def.Type == customfield.TypeDate:
			hints = append(hints, key+" (DD/MM/AAAA)")
		case def.Type == customfield.TypeNumber:
			hints = append(hints, key+" (número)")
		default:
			hints = append(hints, key)
		}
	}
	return strings.Join(hints, ", ")
}

var _ tools.Handler = (*updateLeadProfileTool)(nil)
