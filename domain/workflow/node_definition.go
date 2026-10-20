package workflow

import "fmt"

type NodeCategory string

const (
	NodeCategoryTrigger    NodeCategory = "trigger"
	NodeCategoryAction     NodeCategory = "action"
	NodeCategoryAI         NodeCategory = "ai"
	NodeCategoryMessaging  NodeCategory = "messaging"
	NodeCategoryLogic      NodeCategory = "logic"
	NodeCategoryDecoration NodeCategory = "decoration"
	NodeCategoryWait       NodeCategory = "wait"
	NodeCategoryCondition  NodeCategory = "condition"
	NodeCategoryEnd        NodeCategory = "end"
)

type NodeScope string

const (
	NodeScopeShared   NodeScope = "shared"
	NodeScopeWhatsApp NodeScope = "whatsapp"
	NodeScopeVoice    NodeScope = "voice"
)

type HandleDefinition struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Optional bool   `json:"optional,omitempty"`
}

type OutputKeyDefinition struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

type NodeDefinition struct {
	Type           NodeType               `json:"type"`
	Category       NodeCategory           `json:"category"`
	Scopes         []NodeScope            `json:"scopes,omitempty"`
	Label          string                 `json:"label"`
	Description    string                 `json:"description"`
	Icon           string                 `json:"icon"`
	Outputs        []HandleDefinition     `json:"outputs,omitempty"`
	DynamicHandles bool                   `json:"dynamicHandles,omitempty"`
	OutputKeys     []OutputKeyDefinition  `json:"outputKeys,omitempty"`
	DefaultConfig  map[string]interface{} `json:"defaultConfig"`
	ConfigSchema   []ConfigField          `json:"configSchema"`
	Guidance       NodeGuidance           `json:"guidance"`
	TestBehavior   NodeTestBehavior       `json:"testBehavior"`

	ChannelLimits map[string]ChannelInteractiveLimits `json:"channelLimits,omitempty"`
}

type ChannelInteractiveLimits struct {
	MaxOptionsButtons    int  `json:"maxOptionsButtons"`
	MaxOptionsList       int  `json:"maxOptionsList"`
	MaxLabelRunes        int  `json:"maxLabelRunes"`
	MaxPayloadBytes      int  `json:"maxPayloadBytes"`
	SupportsDescriptions bool `json:"supportsDescriptions"`
}

type NodeGuidance struct {
	When     string   `json:"when"`
	Behavior string   `json:"behavior,omitempty"`
	Examples []string `json:"examples,omitempty"`
}

type ConfigFieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ConfigField struct {
	Key           string              `json:"key"`
	Label         string              `json:"label"`
	Type          string              `json:"type"`
	Placeholder   string              `json:"placeholder,omitempty"`
	Description   string              `json:"description,omitempty"`
	Required      bool                `json:"required,omitempty"`
	Options       []ConfigFieldOption `json:"options,omitempty"`
	OptionsSource string              `json:"optionsSource,omitempty"`
	Min           *float64            `json:"min,omitempty"`
	Max           *float64            `json:"max,omitempty"`
	Step          *float64            `json:"step,omitempty"`
	VisibleWhen   *FieldRule          `json:"visibleWhen,omitempty"`
	RequiredWhen  *FieldRule          `json:"requiredWhen,omitempty"`
}

type FieldRule struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

func (r *FieldRule) Matches(config map[string]interface{}) bool {
	if r == nil {
		return false
	}
	current, _ := config[r.Field].(string)
	for _, v := range r.Values {
		if v == current {
			return true
		}
	}
	return false
}

func (f ConfigField) VisibleFor(config map[string]interface{}) bool {
	return f.VisibleWhen == nil || f.VisibleWhen.Matches(config)
}

func (f ConfigField) RequiredFor(config map[string]interface{}) bool {
	return f.VisibleFor(config) && (f.Required || f.RequiredWhen.Matches(config))
}

type NodeDefiner interface {
	Definition() NodeDefinition
}

func (t WorkflowType) AcceptedScopes() []NodeScope {
	if t == WorkflowTypeVoice {
		return []NodeScope{NodeScopeVoice}
	}
	return []NodeScope{NodeScopeShared, NodeScopeWhatsApp}
}

func (t WorkflowType) Accepts(scope NodeScope) bool {
	for _, accepted := range t.AcceptedScopes() {
		if accepted == scope {
			return true
		}
	}
	return false
}

func DefinitionAllowedForType(def NodeDefinition, wfType WorkflowType) bool {
	def = NormalizeNodeDefinition(def)
	if def.Type.IsTrigger() {
		return TriggerType(def.Type).WorkflowType() == wfType
	}
	return def.acceptedBy(wfType)
}

func (d NodeDefinition) acceptedBy(t WorkflowType) bool {
	for _, scope := range d.Scopes {
		if t.Accepts(scope) {
			return true
		}
	}
	return false
}

func NormalizeNodeDefinition(def NodeDefinition) NodeDefinition {

	for _, field := range def.ConfigSchema {
		if field.Type != "range" {
			continue
		}
		if field.Min == nil || field.Max == nil || field.Step == nil {
			panic(fmt.Sprintf(
				"workflow: range field %q on node %q must define Min, Max and Step",
				field.Key, def.Type,
			))
		}
		if *field.Min >= *field.Max {
			panic(fmt.Sprintf(
				"workflow: range field %q on node %q has Min (%v) >= Max (%v)",
				field.Key, def.Type, *field.Min, *field.Max,
			))
		}
		if *field.Step <= 0 {
			panic(fmt.Sprintf(
				"workflow: range field %q on node %q must have Step > 0 (got %v)",
				field.Key, def.Type, *field.Step,
			))
		}
	}

	if len(def.Scopes) == 0 {
		return def
	}
	seen := make(map[NodeScope]struct{}, len(def.Scopes))
	normalized := make([]NodeScope, 0, len(def.Scopes))
	for _, scope := range def.Scopes {
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	def.Scopes = normalized
	return def
}

func (n NodeType) Category() NodeCategory {
	switch {
	case n.IsDecoration():
		return NodeCategoryDecoration
	case n.IsTrigger():
		return NodeCategoryTrigger
	case n.IsWait(), n.WaitsForCaller():
		return NodeCategoryWait
	case n.IsCondition():
		return NodeCategoryCondition
	case n.IsEnd():
		return NodeCategoryEnd
	case n.IsLogic():
		return NodeCategoryLogic
	case n.IsAI():
		return NodeCategoryAI
	case n.IsMessaging():
		return NodeCategoryMessaging
	default:
		return NodeCategoryAction
	}
}

func BuiltinDefinitions() []NodeDefinition {
	return []NodeDefinition{

		{
			Type:          NodeTypeTriggerFirstMessage,
			Category:      NodeCategoryTrigger,
			TestBehavior:  NodeTestTrigger,
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Primeira Mensagem",
			Description:   "Dispara quando o contato envia a primeira mensagem.",
			Icon:          "ChatCircleDots",
			DefaultConfig: map[string]interface{}{},
			ConfigSchema:  nil,
			Guidance: NodeGuidance{
				When:     "Inicia o fluxo na PRIMEIRA mensagem do contato (uma vez por contato). Ideal para boas-vindas/atendimento inicial.",
				Behavior: "Disponibiliza {{message}} com o texto recebido e {{contact_number}} com o endereço do contato no canal (telefone no WhatsApp, id do chat no Telegram/Instagram; ausente em grupos).",
			},
		},
		{
			Type:          NodeTypeTriggerMessageReceived,
			Category:      NodeCategoryTrigger,
			TestBehavior:  NodeTestTrigger,
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Mensagem Recebida",
			Description:   "Dispara a cada mensagem recebida no chat.",
			Icon:          "ChatCircle",
			DefaultConfig: map[string]interface{}{},
			ConfigSchema:  nil,
			Guidance: NodeGuidance{
				When:     "Inicia o fluxo a CADA mensagem recebida. Use para um atendente que sempre responde.",
				Behavior: "Disponibiliza {{message}} com o texto recebido e {{contact_number}} com o endereço do contato no canal (telefone no WhatsApp, id do chat no Telegram/Instagram; ausente em grupos).",
			},
		},
		{
			Type:          NodeTypeTriggerWebhook,
			Category:      NodeCategoryTrigger,
			TestBehavior:  NodeTestTrigger,
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Webhook",
			Description:   "Dispara quando um sistema externo faz um POST na URL do webhook.",
			Icon:          "Webhooks",
			DefaultConfig: map[string]interface{}{},
			ConfigSchema:  nil,
			Guidance: NodeGuidance{
				When:     "Inicia o fluxo a partir de um evento externo (POST HTTP). Configure a URL, o segredo e o modo de autenticação no painel do nó.",
				Behavior: "Disponibiliza {{var.webhook.body.*}} com o corpo recebido e {{var.webhook.method}}. O payload precisa conter entry_id e entry_type.",
			},
		},
		{
			Type:         NodeTypeTriggerCallReceived,
			Category:     NodeCategoryTrigger,
			TestBehavior: NodeTestTrigger,
			Scopes:       []NodeScope{NodeScopeVoice},
			Label:        "Ligação Recebida",
			Description: "Dispara quando uma ligação chega pelo tronco SIP escolhido. O fluxo atende a " +
				"ligação no lugar de chamar a equipe.",
			Icon:          "PhoneCall",
			DefaultConfig: map[string]interface{}{"trunk_id": ""},
			ConfigSchema: []ConfigField{
				{Key: "trunk_id", Label: "Tronco SIP", Type: "select", OptionsSource: "sip_trunks", Required: true},
			},
			OutputKeys: []OutputKeyDefinition{
				{Key: "caller_number", Description: "Número de quem ligou"},
				{Key: "called_number", Description: "Número discado pelo cliente"},
			},
			Guidance: NodeGuidance{
				When: "Inicia um fluxo de voz (URA) quando uma ligação chega por um tronco SIP. Só existe em fluxos do tipo voice.",
				Behavior: "Atende a ligação e disponibiliza {{caller_number}} e {{called_number}}. Um tronco só pode ter um " +
					"fluxo de voz ativo. Chegar em Fim desliga a ligação.",
			},
		},
		{
			Type:          NodeTypeTriggerFormSubmitted,
			Category:      NodeCategoryTrigger,
			TestBehavior:  NodeTestTrigger,
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Formulário respondido",
			Description:   "Dispara quando um contato envia as respostas de um formulário do WhatsApp.",
			Icon:          "ClipboardText",
			DefaultConfig: map[string]interface{}{FormTriggerFormIDsKey: []interface{}{}},
			ConfigSchema: []ConfigField{
				{Key: FormTriggerFormIDsKey, Label: "Formulários", Type: "multi-select", OptionsSource: "whatsapp_forms", Description: "Vazio: qualquer formulário."},
			},
			OutputKeys: []OutputKeyDefinition{
				{Key: DataKeyFormID, Description: "ID do formulário respondido"},
				{Key: DataKeyFormResponseID, Description: "ID da resposta"},
				{Key: "form.<campo>", Description: "Resposta de cada campo pela chave do campo; campos sensíveis chegam mascarados"},
			},
			Guidance: NodeGuidance{
				When: "Inicia o fluxo a cada resposta enviada a um formulário do WhatsApp, por exemplo para etiquetar a conversa, criar uma oportunidade ou avisar a equipe.",
				Behavior: "Cada resposta inicia a sua própria execução, mesmo quando a mesma pessoa responde o formulário de novo. " +
					"Disponibiliza {{form_id}}, {{form_response_id}} e {{form.<chave_do_campo>}} com o valor exibido de cada resposta (campos sensíveis mascarados). " +
					"Sem formulários escolhidos, dispara para qualquer formulário.",
			},
		},
		{
			Type:          NodeTypeTriggerAgentCall,
			Category:      NodeCategoryTrigger,
			TestBehavior:  NodeTestTrigger,
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Chamado por agente IA",
			Description:   "Dispara quando um agente de IA executa este fluxo na conversa em que está atendendo.",
			Icon:          "Robot",
			DefaultConfig: map[string]interface{}{AgentCallInputsKey: []interface{}{}},
			ConfigSchema: []ConfigField{
				{Key: AgentCallInputsKey, Label: "Campos recebidos do agente", Type: "agent_call_inputs",
					Description: "Cada campo vira um parâmetro da ferramenta Executar fluxo: nome (minúsculas, dígitos e _), tipo (string, number, boolean, object, array), descrição, obrigatório e exemplo."},
			},
			OutputKeys: []OutputKeyDefinition{
				{Key: DataKeyAgentInput + ".<campo>", Description: "Valor de cada campo enviado pelo agente"},
				{Key: DataKeyAgent + ".id", Description: "ID do agente que executou o fluxo"},
				{Key: DataKeyAgent + ".name", Description: "Nome do agente que executou o fluxo"},
			},
			Guidance: NodeGuidance{
				When: "Inicia o fluxo quando um agente de IA chama a ferramenta Executar fluxo durante um atendimento, sempre na conversa atual.",
				Behavior: "Disponibiliza {{input.<campo>}} com os campos declarados (validados antes de iniciar) e {{agent.id}} / {{agent.name}}. " +
					"Use o nó Responder ao agente para devolver um resultado ao agente.",
			},
		},
		{
			Type:          NodeTypeEnd,
			Category:      NodeCategoryEnd,
			TestBehavior:  NodeTestEnd,
			Scopes:        []NodeScope{NodeScopeShared, NodeScopeVoice},
			Label:         "Fim",
			Description:   "Finaliza a execução do fluxo. Em fluxos de voz, desliga a ligação.",
			Icon:          "FlagCheckered",
			DefaultConfig: map[string]interface{}{},
			ConfigSchema:  nil,
			Guidance: NodeGuidance{
				When: "Para encerrar um caminho do fluxo. Todo grafo precisa de ao menos um caminho que chegue a um 'end'.",
			},
		},
	}
}

func NodeCatalogMap(catalog []NodeDefinition) map[NodeType]NodeDefinition {
	m := make(map[NodeType]NodeDefinition, len(catalog))
	for _, def := range catalog {
		m[def.Type] = NormalizeNodeDefinition(def)
	}
	return m
}
