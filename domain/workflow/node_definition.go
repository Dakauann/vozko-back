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
	Type        NodeType     `json:"type"`
	Category    NodeCategory `json:"category"`
	Scopes      []NodeScope  `json:"scopes,omitempty"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	Icon        string       `json:"icon"`
	// Outputs are the node's BASE/static output handles, those that don't depend
	// on config. They ship in the catalog so the frontend renders them instantly.
	Outputs []HandleDefinition `json:"outputs,omitempty"`
	// DynamicHandles marks a node whose full handle set depends on its config
	// (e.g. ai_agent tool routes, text_match cases). The frontend learns which
	// node types are dynamic FROM THIS FLAG (not a hardcoded list) and, for those,
	// asks the backend (POST /workflows/resolve-handles) for the resolved set.
	DynamicHandles bool                   `json:"dynamicHandles,omitempty"`
	OutputKeys     []OutputKeyDefinition  `json:"outputKeys,omitempty"`
	DefaultConfig  map[string]interface{} `json:"defaultConfig"`
	ConfigSchema   []ConfigField          `json:"configSchema"`
	// Guidance is authored by each executor (or builtin) and travels with the
	// node in the catalog so the AI Workflow Builder always sees how each node
	// works and behaves. Required, every node must describe itself.
	Guidance NodeGuidance `json:"guidance"`

	// ChannelLimits reports, per channel, what this node will actually render.
	//
	// Only the interactive prompt sets it today. It exists because the option
	// list an author writes is ONE list rendered by several channels with
	// different caps: three buttons on WhatsApp, thirteen on Instagram, no
	// practical limit on Telegram. Without this the editor cannot tell the
	// author that options four through thirteen will silently not appear on
	// WhatsApp, and the first anyone learns of it is a customer who never saw
	// the option.
	ChannelLimits map[string]ChannelInteractiveLimits `json:"channelLimits,omitempty"`
}

// ChannelInteractiveLimits is the JSON shape of one channel's option limits.
//
// It mirrors channel.InteractiveLimits rather than reusing it so the domain's
// internal type is free to change without altering an API the frontend parses.
type ChannelInteractiveLimits struct {
	MaxOptionsButtons int `json:"maxOptionsButtons"`
	MaxOptionsList    int `json:"maxOptionsList"`
	// MaxLabelRunes is 0 when the channel documents no label limit.
	MaxLabelRunes int `json:"maxLabelRunes"`
	// MaxPayloadBytes bounds the option id. Bytes, not characters.
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	// SupportsDescriptions is false for every channel except WhatsApp lists.
	SupportsDescriptions bool `json:"supportsDescriptions"`
}

// NodeGuidance is per-node usage guidance for the AI Workflow Builder, authored
// alongside each node's Definition(). It describes WHEN to use the node and any
// non-obvious runtime BEHAVIOR. It deliberately does NOT list output handles,
// output keys, or config fields, those are dynamic/structured data exposed by
// Outputs, OutputKeys, and ConfigSchema, which the builder already sees.
type NodeGuidance struct {
	When string `json:"when"`
	// Behavior describes non-obvious runtime behavior only (e.g. "the node sends
	// the message itself in segmented mode"). Empty when there is nothing the
	// structured fields don't already convey.
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
}

type NodeDefiner interface {
	Definition() NodeDefinition
}

func (t TriggerType) PrimaryNodeScope() NodeScope {
	return NodeScopeWhatsApp
}

func (t WorkflowType) PrimaryNodeScope() NodeScope {
	return NodeScopeWhatsApp
}

func DefinitionAllowedForType(def NodeDefinition, wfType WorkflowType) bool {
	def = NormalizeNodeDefinition(def)
	if def.Type.IsTrigger() {
		return TriggerType(def.Type).WorkflowType() == wfType
	}
	primaryScope := wfType.PrimaryNodeScope()
	for _, scope := range def.Scopes {
		if scope == NodeScopeShared || scope == primaryScope {
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

func DefinitionAllowedForTrigger(def NodeDefinition, triggerType TriggerType) bool {
	def = NormalizeNodeDefinition(def)
	if def.Type.IsTrigger() {
		return def.Type == NodeType(triggerType)
	}
	return DefinitionAllowedForType(def, triggerType.WorkflowType())
}

func (n NodeType) Category() NodeCategory {
	switch {
	case n.IsDecoration():
		return NodeCategoryDecoration
	case n.IsTrigger():
		return NodeCategoryTrigger
	case n.IsWait():
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
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Primeira Mensagem",
			Description:   "Dispara quando o contato envia a primeira mensagem.",
			Icon:          "ChatCircleDots",
			DefaultConfig: map[string]interface{}{},
			ConfigSchema:  nil,
			Guidance: NodeGuidance{
				When:     "Inicia o fluxo na PRIMEIRA mensagem do contato (uma vez por contato). Ideal para boas-vindas/atendimento inicial.",
				Behavior: "Disponibiliza {{message}} com o texto recebido do contato.",
			},
		},
		{
			Type:          NodeTypeTriggerMessageReceived,
			Category:      NodeCategoryTrigger,
			Scopes:        []NodeScope{NodeScopeWhatsApp},
			Label:         "Mensagem Recebida",
			Description:   "Dispara a cada mensagem recebida no chat.",
			Icon:          "ChatCircle",
			DefaultConfig: map[string]interface{}{},
			ConfigSchema:  nil,
			Guidance: NodeGuidance{
				When:     "Inicia o fluxo a CADA mensagem recebida. Use para um atendente que sempre responde.",
				Behavior: "Disponibiliza {{message}} com o texto recebido do contato.",
			},
		},
		{
			Type:          NodeTypeTriggerWebhook,
			Category:      NodeCategoryTrigger,
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
			Type:          NodeTypeEnd,
			Category:      NodeCategoryEnd,
			Scopes:        []NodeScope{NodeScopeShared},
			Label:         "Fim",
			Description:   "Finaliza a execução do fluxo.",
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
