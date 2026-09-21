package copilottools

import (
	"reflect"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

type agentFields struct {
	Name              *string `json:"name" req:"true"`
	Description       *string `json:"description"`
	InitialMessage    *string `json:"initialMessage"`
	UseInitialMessage *bool   `json:"useInitialMessage"`
	MessagingPrompt   *string `json:"messagingPrompt" req:"true"`
	MessagingModel    *string `json:"messagingModel" req:"true"`
	AvatarURL         *string `json:"avatarUrl"`
	Provider          *string `json:"provider" req:"true"`
	BusinessPhoneID   *string `json:"businessPhoneId"`
	IsActive          *bool   `json:"isActive"`
}

var agentFieldDescriptions = map[string]string{
	"name":              "nome do agente",
	"description":       "descrição interna do agente",
	"initialMessage":    "mensagem inicial enviada ao iniciar a conversa",
	"useInitialMessage": "liga/desliga o envio da mensagem inicial",
	"messagingPrompt":   "prompt de mensagens (comportamento do agente por texto)",
	"messagingModel":    "id do modelo de LLM para mensagens (use list_models para um id válido)",
	"avatarUrl":         "URL do avatar do agente",
	"provider":          "provedor do agente, ex.: platform",
	"businessPhoneId":   "id do número de WhatsApp Business vinculado ao agente",
	"isActive":          "se o agente está ativo",

	"internalTools":          "ferramentas internas do agente (lista COMPLETA; use list_agent_tools para nomes válidos e o config exigido por cada uma)",
	"knowledgeBaseIds":       "ids das bases de conhecimento a vincular (devem ser deste workspace)",
	"mcpCollectionIds":       "ids das coleções MCP a vincular (devem ser deste workspace)",
	"addTools":               "ferramentas internas a ADICIONAR, preservando as atuais. Use list_agent_tools para o nome exato e o config exigido (ex.: http_request exige url e method)",
	"removeTools":            "nomes das ferramentas internas a REMOVER do agente",
	"addKnowledgeBaseIds":    "ids de bases de conhecimento a ADICIONAR, preservando as atuais",
	"removeKnowledgeBaseIds": "ids de bases de conhecimento a REMOVER",
	"addMcpCollectionIds":    "ids de coleções MCP a ADICIONAR, preservando as atuais",
	"removeMcpCollectionIds": "ids de coleções MCP a REMOVER",
}

func (f agentFields) toUpdateInput() agent.UpdateAgentInput {
	in := agent.UpdateAgentInput{
		Name:              f.Name,
		Description:       f.Description,
		InitialMessage:    f.InitialMessage,
		UseInitialMessage: f.UseInitialMessage,
		MessagingPrompt:   f.MessagingPrompt,
		MessagingModel:    f.MessagingModel,
		AvatarURL:         f.AvatarURL,
		BusinessPhoneID:   f.BusinessPhoneID,
		IsActive:          f.IsActive,
	}
	if f.Provider != nil {
		p := agent.AgentProvider(*f.Provider)
		in.Provider = &p
	}
	return in
}

func (f agentFields) toCreateInput() agent.CreateAgentInput {
	in := agent.CreateAgentInput{
		UseInitialMessage: f.UseInitialMessage,
		IsActive:          f.IsActive,
	}
	if f.Name != nil {
		in.Name = *f.Name
	}
	if f.Description != nil {
		in.Description = *f.Description
	}
	if f.InitialMessage != nil {
		in.InitialMessage = *f.InitialMessage
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
	return structParams(reflect.TypeOf(agentFields{}), agentFieldDescriptions)
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
		if id = trimLower(id); id != "" {
			drop[id] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(current)+len(add))
	out := make([]string, 0, len(current)+len(add))
	for _, id := range append(append([]string{}, current...), add...) {
		key := trimLower(id)
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

func mergeToolBindings(current []agent.ToolBinding, add []toolBindingArg, remove []string) []agent.ToolBinding {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	drop := make(map[string]struct{}, len(remove))
	for _, name := range remove {
		if name = trimLower(name); name != "" {
			drop[name] = struct{}{}
		}
	}
	replace := make(map[string]agent.ToolBinding, len(add))
	order := make([]string, 0, len(add))
	for _, a := range add {
		key := trimLower(a.Name)
		if key == "" {
			continue
		}
		if _, exists := replace[key]; !exists {
			order = append(order, key)
		}
		replace[key] = a.toBinding()
	}

	out := make([]agent.ToolBinding, 0, len(current)+len(add))
	kept := make(map[string]struct{}, len(current))
	for _, tb := range current {
		key := trimLower(tb.Name)
		if _, dropped := drop[key]; dropped {
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
		if _, dropped := drop[key]; dropped {
			continue
		}
		out = append(out, replace[key])
	}
	return out
}
