package platformtools

import (
	"reflect"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

type agentFields struct {
	Name            *string `json:"name" req:"true"`
	Description     *string `json:"description"`
	MessagingPrompt *string `json:"messagingPrompt" req:"true"`
	MessagingModel  *string `json:"messagingModel" req:"true"`
	AvatarURL       *string `json:"avatarUrl"`
	Provider        *string `json:"provider" req:"true"`
	BusinessPhoneID *string `json:"businessPhoneId"`
	IsActive        *bool   `json:"isActive"`
}

var agentFieldDescriptions = map[string]string{
	"name":            "nome do agente",
	"description":     "descrição interna do agente",
	"messagingPrompt": "prompt de mensagens (comportamento do agente por texto)",
	"messagingModel":  "id do modelo de LLM para mensagens (use list_models para um id válido)",
	"avatarUrl":       "URL do avatar do agente",
	"provider":        "provedor do agente, ex.: platform",
	"businessPhoneId": "id do número de WhatsApp Business vinculado ao agente",
	"isActive":        "se o agente está ativo",

	"internalTools":          "ferramentas internas do agente (lista COMPLETA; use list_agent_tools para nomes válidos e o config exigido por cada uma). Cabeçalhos (headers) e outros valores protegidos: envie só os NOMES com valor vazio (ex.: {\"Authorization\": \"\"}); o usuário digita os valores no cartão de aprovação e você nunca os vê.",
	"knowledgeBaseIds":       "ids das bases de conhecimento a vincular (devem ser deste workspace)",
	"mcpCollectionIds":       "ids das coleções MCP a vincular (devem ser deste workspace)",
	"addTools":               "ferramentas internas a ADICIONAR ou substituir, preservando as demais. Use list_agent_tools para o nome exato e o config exigido (ex.: http_request exige url e method). Ao substituir, a configuração inteira é trocada: repita os nomes dos cabeçalhos que devem continuar. Cabeçalhos (headers) e outros valores protegidos: envie só os NOMES com valor vazio (ex.: {\"Authorization\": \"\"}); o usuário digita os valores no cartão de aprovação e você nunca os vê. run_workflow (Executar fluxo) vale uma vez por fluxo: cada item com um workflow_id diferente é uma ferramenta própria e os outros fluxos do agente são preservados.",
	"removeTools":            "nomes das ferramentas internas a REMOVER do agente. Para tirar só um fluxo de run_workflow, use run_workflow_<id do fluxo>; run_workflow sozinho remove todos os fluxos",
	"addKnowledgeBaseIds":    "ids de bases de conhecimento a ADICIONAR, preservando as atuais",
	"removeKnowledgeBaseIds": "ids de bases de conhecimento a REMOVER",
	"addMcpCollectionIds":    "ids de coleções MCP a ADICIONAR, preservando as atuais",
	"removeMcpCollectionIds": "ids de coleções MCP a REMOVER",
}

func (f agentFields) toUpdateInput() agent.UpdateAgentInput {
	in := agent.UpdateAgentInput{
		Name:            f.Name,
		Description:     f.Description,
		MessagingPrompt: f.MessagingPrompt,
		MessagingModel:  f.MessagingModel,
		AvatarURL:       f.AvatarURL,
		BusinessPhoneID: f.BusinessPhoneID,
		IsActive:        f.IsActive,
	}
	if f.Provider != nil {
		p := agent.AgentProvider(*f.Provider)
		in.Provider = &p
	}
	return in
}

func (f agentFields) toCreateInput() agent.CreateAgentInput {
	in := agent.CreateAgentInput{
		IsActive: f.IsActive,
	}
	if f.Name != nil {
		in.Name = *f.Name
	}
	if f.Description != nil {
		in.Description = *f.Description
	}
	if f.MessagingPrompt != nil {
		in.MessagingPrompt = *f.MessagingPrompt
	}
	if f.MessagingModel != nil {
		in.MessagingModel = *f.MessagingModel
	}
	if f.AvatarURL != nil {
		in.AvatarURL = *f.AvatarURL
	}
	if f.Provider != nil {
		in.Provider = agent.AgentProvider(*f.Provider)
	}
	if f.BusinessPhoneID != nil {
		in.BusinessPhoneID = *f.BusinessPhoneID
	}
	return in
}

type toolBindingArg struct {
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config,omitempty"`
}

func (a toolBindingArg) toBinding() agent.ToolBinding {
	return agent.ToolBinding{Name: a.Name, Config: a.Config}
}

func scalarParams() (map[string]tools.Parameter, []string) {
	return copilottools.StructParams(reflect.TypeOf(agentFields{}), agentFieldDescriptions)
}

func toolListParam(description string) tools.Parameter {
	return tools.Parameter{
		Type:        "array",
		Description: description,
		Items: &tools.ParameterItems{
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]tools.Parameter{
				"name": {
					Type:        "string",
					Description: "nome exato da ferramenta, como retornado por list_agent_tools",
				},
				"config": {
					Type:        "object",
					Description: "configuração da ferramenta, quando ela exigir (ex.: http_request: {\"url\": \"https://viacep.com.br/ws/{cep}/json/\", \"method\": \"GET\"})",
				},
			},
		},
	}
}

func stringListParam(description string) tools.Parameter {
	return tools.Parameter{
		Type:        "array",
		Description: description,
		Items:       &tools.ParameterItems{Type: "string"},
	}
}

func mergeStrings(current, add, remove []string) []string {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	drop := make(map[string]struct{}, len(remove))
	for _, id := range remove {
		if id = copilottools.TrimLower(id); id != "" {
			drop[id] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(current)+len(add))
	out := make([]string, 0, len(current)+len(add))
	for _, id := range append(append([]string{}, current...), add...) {
		key := copilottools.TrimLower(id)
		if key == "" {
			continue
		}
		if _, dropped := drop[key]; dropped {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	return out
}

type bindingIdentities map[string]tools.Definition

func bindingIdentitiesOf(catalog ToolCatalog) bindingIdentities {
	ids := bindingIdentities{}
	if catalog == nil {
		return ids
	}
	for _, def := range catalog.Definitions() {
		ids[copilottools.TrimLower(def.Name)] = def
	}
	return ids
}

func (ids bindingIdentities) of(name string, config map[string]interface{}) string {
	if def, ok := ids[copilottools.TrimLower(name)]; ok {
		return def.BindingIdentity(config)
	}
	return copilottools.TrimLower(name)
}

func dropped(drop map[string]struct{}, name, identity string) bool {
	_, byIdentity := drop[identity]
	_, byName := drop[copilottools.TrimLower(name)]
	return byIdentity || byName
}

func mergeToolBindings(catalog ToolCatalog, current []agent.ToolBinding, add []toolBindingArg, remove []string) []agent.ToolBinding {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	ids := bindingIdentitiesOf(catalog)
	drop := make(map[string]struct{}, len(remove))
	for _, name := range remove {
		if name = copilottools.TrimLower(name); name != "" {
			drop[name] = struct{}{}
		}
	}
	replace := make(map[string]agent.ToolBinding, len(add))
	order := make([]string, 0, len(add))
	for _, a := range add {
		if copilottools.TrimLower(a.Name) == "" {
			continue
		}
		key := ids.of(a.Name, a.Config)
		if _, exists := replace[key]; !exists {
			order = append(order, key)
		}
		replace[key] = a.toBinding()
	}

	out := make([]agent.ToolBinding, 0, len(current)+len(add))
	kept := make(map[string]struct{}, len(current))
	for _, tb := range current {
		key := ids.of(tb.Name, tb.Config)
		if dropped(drop, tb.Name, key) {
			continue
		}
		if updated, ok := replace[key]; ok {
			out = append(out, updated)
			kept[key] = struct{}{}
			continue
		}
		out = append(out, tb)
		kept[key] = struct{}{}
	}
	for _, key := range order {
		if _, already := kept[key]; already {
			continue
		}
		if dropped(drop, replace[key].Name, key) {
			continue
		}
		out = append(out, replace[key])
	}
	return out
}

func argToolBindings(args map[string]interface{}, key string) []toolBindingArg {
	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]toolBindingArg, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		binding := toolBindingArg{Name: name}
		if cfg, ok := m["config"].(map[string]interface{}); ok && len(cfg) > 0 {
			binding.Config = cfg
		}
		out = append(out, binding)
	}
	return out
}
