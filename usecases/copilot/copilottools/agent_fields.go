package copilottools

import (
	"reflect"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

// agentFields is the copilot's OWN view of the agent surface it may edit, and
// the single place that decides what the assistant can see and change.
//
// It exists because the tool schema used to be reflected straight off
// agent.CreateAgentInput / agent.UpdateAgentInput. Those structs carry json
// tags to control HTTP body binding, and every field the HTTP layer fills from
// its own request DTO is tagged `json:"-"`. structParams skips empty json
// names, so all of them, internalTools included, silently vanished from the
// schema: update_agent had no parameter for tools at all. It reported success
// having changed nothing, and the assistant looped, re-approving a no-op.
//
// Reflecting off a struct owned HERE fixes both failure directions for good:
//
//   - a new agent field stays invisible to the copilot until someone adds it
//     to this struct, so nothing is exposed by accident;
//   - adding it is one line plus a description, so nothing stays unreachable
//     by accident either.
//
// Anything id-shaped (business phone, knowledge base, MCP collection) is still
// verified against the caller's workspace by the agent use cases; this struct
// decides what may be ASKED for, never what is allowed.
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

	// Membership parameters, declared by membershipParams below.
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

// toUpdateInput maps the DTO onto the domain's partial-update input. Nil stays
// nil: ApplyUpdate reads that as "leave this field alone".
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

// toCreateInput maps the DTO onto the domain's create input. WorkspaceID is
// NOT taken from here: the caller stamps it from the authenticated copilot
// context, so the model cannot choose which workspace it creates in.
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

// toolBindingArg is one tool the assistant asks to bind. Kept separate from
// agent.ToolBinding so the model-facing shape can stay minimal: visibility is
// deliberately not exposed, since the tool's own definition already declares
// where it may be used.
type toolBindingArg struct {
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config,omitempty"`
}

func (a toolBindingArg) toBinding() agent.ToolBinding {
	return agent.ToolBinding{Name: a.Name, Config: a.Config}
}

// scalarParams reflects the editable scalars. required is returned separately
// because create needs it and update does not (there, only id is required).
func scalarParams() (map[string]tools.Parameter, []string) {
	return structParams(reflect.TypeOf(agentFields{}), agentFieldDescriptions)
}

// toolListParam is the array-of-objects schema for tool bindings.
//
// Declared by hand because jsonType() flattens every slice to a bare "array":
// the provider converter only emits an items schema when Items is set, so a
// reflected []ToolBinding would reach the model as {"type":"array"} with no
// hint of the element shape, and it would have to guess {name, config}.
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

// mergeStrings applies an add/remove pair to the current set, preserving order
// and ignoring duplicates. Returns nil when neither list changes anything, so
// the caller can leave the field untouched (ApplyUpdate treats nil as "keep").
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

// mergeToolBindings is the same merge for tools, matched on name. An added
// tool that is already bound REPLACES the existing binding, so re-adding with
// a corrected config is how the assistant fixes a misconfiguration.
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
