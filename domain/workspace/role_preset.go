package workspace

import (
	"fmt"
	"sort"
)

type RolePresetKey string

const (
	PresetOperator   RolePresetKey = "operator"
	PresetSupervisor RolePresetKey = "supervisor"
	PresetManager    RolePresetKey = "manager"
	PresetSales      RolePresetKey = "sales"
	PresetAnalyst    RolePresetKey = "analyst"
	PresetMarketing  RolePresetKey = "marketing"
	PresetAutomation RolePresetKey = "automation"
	PresetFinance    RolePresetKey = "finance"
)

type RolePreset struct {
	Key          RolePresetKey   `json:"key"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Highlights   []string        `json:"highlights"`
	Capabilities []CapabilityKey `json:"capabilities"`
}

var operatorCapabilities = []CapabilityKey{
	"inbox.view", "inbox.reply", "inbox.labels", "inbox.stage", "inbox.contact_edit", "inbox.roulette",
	"crm_board.view", "crm_board.move_stage",
	"labels.view", "funnels.view", "message_shortcuts.view",
	"media.view", "media.upload",
	"leads.view",
	"calls.use", "call_history.view",
	"call_lists.view", "call_lists.work",
	"ai_chat.view", "ai_chat.chat",
}

var supervisorCapabilities = append(append([]CapabilityKey{}, operatorCapabilities...),
	"inbox.view_others", "inbox.assign", "inbox.delegate", "inbox.automation", "inbox.reopen",
	"inbox.analysis", "inbox.ops_panel", "inbox.move_funnel", "inbox.contact_block",
	"crm_board.move_funnel", "crm_board.move_owner", "crm_board.bulk",
	"message_shortcuts.create", "message_shortcuts.edit",
	"attendance.view", "attendance.targets_view",
	"call_history.view_team", "call_history.listen", "calls.presence",
	"team.members", "departments.view",
)

var RolePresets = []RolePreset{
	{
		Key:          PresetOperator,
		Name:         "Operador de atendimento",
		Description:  "Atende as próprias conversas: responde, envia imagens e arquivos, usa atalhos e move o cliente pelo funil.",
		Highlights:   []string{"Responde clientes", "Envia mídia", "Vê só as próprias conversas"},
		Capabilities: operatorCapabilities,
	},
	{
		Key:          PresetSupervisor,
		Name:         "Supervisor de atendimento",
		Description:  "Acompanha a equipe: vê todas as conversas, redistribui, liga ou pausa a IA e acompanha as métricas da operação.",
		Highlights:   []string{"Vê toda a equipe", "Redistribui conversas", "Métricas de atendimento"},
		Capabilities: supervisorCapabilities,
	},
	{
		Key:         PresetManager,
		Name:        "Gerente",
		Description: "Conduz a operação: tudo do supervisor, mais metas, relatórios, funis, oportunidades, leads e convites para a equipe.",
		Highlights:  []string{"Metas e relatórios", "Funis e oportunidades", "Convida a equipe"},
		Capabilities: append(append([]CapabilityKey{}, supervisorCapabilities...),
			"attendance.targets_edit", "attendance.campaign_report", "reports.view", "reports.create",
			"funnels.create", "funnels.edit", "labels.create", "labels.edit",
			"deals.view", "deals.create", "deals.edit", "deals.assign",
			"leads.import", "leads.edit", "leads.block", "leads.assign", "leads.read_sensitive", "leads.read_addresses", "leads.configure",
			"leads.bulk_edit", "leads.export", "leads.meta_audience", "leads.send_template", "leads.send_unofficial",
			"call_lists.manage",
			"whatsapp_campaigns.view", "whatsapp_campaigns.crm",
			"team.invite", "departments.edit",
		),
	},
	{
		Key:         PresetSales,
		Name:        "Vendedor",
		Description: "Atende e vende: tudo do operador, mais oportunidades, cadastro de leads e reabertura de conversas.",
		Highlights:  []string{"Oportunidades", "Cadastra leads", "Reabre conversas"},
		Capabilities: append(append([]CapabilityKey{}, operatorCapabilities...),
			"deals.view", "deals.create", "deals.edit",
			"leads.import", "inbox.reopen",
		),
	},
	{
		Key:         PresetAnalyst,
		Name:        "Analista",
		Description: "Acompanha resultados sem alterar nada: conversas, funis, oportunidades, métricas, campanhas, anúncios e relatórios.",
		Highlights:  []string{"Somente leitura", "Métricas e relatórios", "Campanhas e anúncios"},
		Capabilities: []CapabilityKey{
			"inbox.view", "inbox.view_others", "inbox.analysis",
			"crm_board.view", "funnels.view", "deals.view", "leads.view",
			"attendance.view", "attendance.targets_view", "attendance.campaign_report",
			"reports.view", "reports.create", "audience.view",
			"whatsapp_campaigns.view", "unofficial_campaigns.view", "ads.view",
			"ai_chat.view", "ai_chat.chat",
		},
	},
	{
		Key:         PresetMarketing,
		Name:        "Marketing",
		Description: "Cuida de campanhas, modelos de mensagem, redes sociais, anúncios da Meta e links.",
		Highlights:  []string{"Campanhas e disparos", "Instagram e Facebook", "Anúncios da Meta"},
		Capabilities: []CapabilityKey{
			"whatsapp_campaigns.view", "whatsapp_campaigns.create", "whatsapp_campaigns.edit",
			"whatsapp_campaigns.start", "whatsapp_campaigns.stop",
			"whatsapp_templates.view", "whatsapp_templates.create", "whatsapp_templates.edit",
			"unofficial_campaigns.view", "unofficial_campaigns.create", "unofficial_campaigns.edit",
			"unofficial_campaigns.start", "unofficial_campaigns.stop",
			"instagram.view", "instagram.manage", "facebook.view", "facebook.manage",
			"ads.view", "ads.toggle", "ads.edit", "audience.view",
			"links.view", "links.create", "links.edit",
			"leads.view", "leads.import", "media.view", "media.upload", "reports.view",
		},
	},
	{
		Key:         PresetAutomation,
		Name:        "Especialista em IA e automações",
		Description: "Configura agentes de IA, bases de conhecimento, automações e os servidores MCP que eles usam.",
		Highlights:  []string{"Agentes de IA", "Bases de conhecimento", "Automações"},
		Capabilities: []CapabilityKey{
			"agents.view", "agents.details", "agents.create", "agents.edit",
			"knowledge_bases.view", "knowledge_bases.create", "knowledge_bases.edit",
			"workflows.view", "workflows.create", "workflows.edit",
			"mcp.view", "ai_chat.view", "ai_chat.chat", "ai_chat.manage",
		},
	},
	{
		Key:          PresetFinance,
		Name:         "Financeiro",
		Description:  "Acompanha saldo, faturas e a assinatura, contrata planos e recargas e baixa relatórios.",
		Highlights:   []string{"Saldo e faturas", "Planos e recargas", "Relatórios"},
		Capabilities: []CapabilityKey{"billing.balance", "billing.plans", "billing.purchase", "reports.view"},
	},
}

func RolePresetByKey(key RolePresetKey) (RolePreset, bool) {
	for _, p := range RolePresets {
		if p.Key == key {
			return p, true
		}
	}
	return RolePreset{}, false
}

func CapabilityByKey(key CapabilityKey) (Capability, bool) {
	for _, f := range Features {
		for _, c := range f.Capabilities {
			if c.Key == key {
				return c, true
			}
		}
	}
	return Capability{}, false
}

func CapabilityRequires(key CapabilityKey) ([]PermissionEntry, bool) {
	c, ok := CapabilityByKey(key)
	if !ok {
		return nil, false
	}
	return append([]PermissionEntry{}, c.Requires...), true
}

func (p RolePreset) Permissions() ([]PermissionEntry, error) {
	seen := map[PermissionEntry]bool{}
	for _, key := range p.Capabilities {
		capability, ok := CapabilityByKey(key)
		if !ok {
			return nil, fmt.Errorf("role preset %q: unknown capability %q", p.Key, key)
		}
		for _, need := range capability.Requires {
			seen[need] = true
		}
	}
	out := make([]PermissionEntry, 0, len(seen))
	for entry := range seen {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}

func (r *CustomRole) ValidatePreset() error {
	if r.PresetKey == "" {
		if r.Linked {
			return ErrUnknownRolePreset
		}
		return nil
	}
	if _, ok := RolePresetByKey(r.PresetKey); !ok {
		return ErrUnknownRolePreset
	}
	return nil
}

func (r *CustomRole) SyncWithPreset() (bool, error) {
	if !r.Linked {
		return false, nil
	}
	if err := r.ValidatePreset(); err != nil {
		return false, err
	}
	preset, _ := RolePresetByKey(r.PresetKey)
	permissions, err := preset.Permissions()
	if err != nil {
		return false, err
	}
	if SamePermissions(r.Permissions, permissions) {
		return false, nil
	}
	r.Permissions = permissions
	return true, nil
}

func SamePermissions(a, b []PermissionEntry) bool {
	set := make(map[PermissionEntry]bool, len(a))
	for _, p := range a {
		set[p] = true
	}
	other := make(map[PermissionEntry]bool, len(b))
	for _, p := range b {
		if !set[p] {
			return false
		}
		other[p] = true
	}
	return len(set) == len(other)
}
