package workspace

import (
	"time"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) IsValid() bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleMember
}

func (r Role) CanManageMembers() bool {
	return r == RoleOwner || r == RoleAdmin
}

type InviteStatus string

const (
	InviteStatusPending   InviteStatus = "pending"
	InviteStatusAccepted  InviteStatus = "accepted"
	InviteStatusDeclined  InviteStatus = "declined"
	InviteStatusExpired   InviteStatus = "expired"
	InviteStatusCancelled InviteStatus = "cancelled"
)

type Resource string

var Resources = make(map[Resource]struct{})

func registerResource(res string) Resource {
	resource := Resource(res)
	Resources[resource] = struct{}{}

	return resource
}

var (
	ResourceAgents            = registerResource("agents")
	ResourceCampaigns         = registerResource("campaigns")
	ResourceWhatsAppCampaigns = registerResource("whatsapp_campaigns")
	ResourceWhatsAppTemplates = registerResource("whatsapp_templates")
	ResourceBusinessPhones    = registerResource("business_phones")
	ResourceStages            = registerResource("stages")
	ResourceStageGroups       = registerResource("stage_groups")
	ResourceLabels            = registerResource("labels")
	ResourceBalance           = registerResource("balance")
	ResourceConversations     = registerResource("conversations")
	ResourceMedia             = registerResource("media")
	ResourceLeads             = registerResource("leads")
	ResourceAnalysis          = registerResource("analysis")
	ResourceSIPTrunks         = registerResource("sip_trunks")
	ResourceCallRecordings    = registerResource("call_recordings")
	ResourceMembers           = registerResource("members")
	ResourceAssignments       = registerResource("assignments")
	ResourceAttendance        = registerResource("attendance")
	ResourceKnowledgeBases    = registerResource("knowledge_bases")
	ResourceUsage             = registerResource("usage")
	ResourceRoles             = registerResource("roles")
	ResourceSupportInboxes    = registerResource("support_inboxes")
	ResourceIssues            = registerResource("issues")
	ResourceWorkflows         = registerResource("workflows")
	ResourceCalendar          = registerResource("calendar")
	ResourceDepartments       = registerResource("departments")
	ResourceMessageShortcuts  = registerResource("message_shortcuts")
	ResourceDialer            = registerResource("dialer")
	ResourceAffiliate         = registerResource("affiliate")
	ResourceMCP               = registerResource("mcp")
	ResourcePlans             = registerResource("plans")
	ResourceAIChat            = registerResource("ai_chat")
	ResourceBranches          = registerResource("branches")
	ResourceShortLinks        = registerResource("short_links")
)

func (r Resource) IsValid() bool {
	_, exists := Resources[r]
	return exists
}

type Action string
type ActionDefinition struct {
	ActionName  Action            `json:"actionName"`
	Description string            `json:"description"`
	Requires    []PermissionEntry `json:"requires,omitempty"`
}

var Actions = make(map[Action]struct{})

func registerAction(action string) Action {
	a := Action(action)
	Actions[a] = struct{}{}
	return a
}

var (
	ActionCreate      = registerAction("create")
	ActionRead        = registerAction("read")
	ActionReadDetails = registerAction("read_details")
	ActionUpdate      = registerAction("update")
	ActionDelete      = registerAction("delete")
	ActionStart       = registerAction("start")
	ActionStop        = registerAction("stop")
	ActionSend        = registerAction("send")
	ActionReopen      = registerAction("reopen")
	ActionAssign      = registerAction("assign")
	ActionViewOthers  = registerAction("view_others")
	ActionRoulette    = registerAction("roulette")
	ActionUse         = registerAction("use")
	ActionBlock       = registerAction("block")
	ActionCall        = registerAction("call")

	ActionTransfer = registerAction("transfer")

	ActionListMembers = registerAction("list_members")
)

func (a Action) IsValid() bool {
	_, exists := Actions[a]
	return exists
}

var ResourceActions = map[Resource][]ActionDefinition{
	ResourceAgents: {
		{ActionName: ActionCreate, Description: "Criar novos agentes de IA"},
		{ActionName: ActionRead, Description: "Visualizar agentes"},
		{ActionName: ActionReadDetails, Description: "Visualizar as configurações do agente", Requires: []PermissionEntry{
			{Resource: ResourceAgents, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar configurações de agentes"},
		{ActionName: ActionDelete, Description: "Excluir agentes"},
	},
	ResourceAIChat: {
		{ActionName: ActionRead, Description: "Visualizar e usar o chat de IA"},
		{ActionName: ActionCreate, Description: "Criar conversas e enviar mensagens no chat de IA"},
		{ActionName: ActionUpdate, Description: "Renomear conversas do chat de IA"},
		{ActionName: ActionDelete, Description: "Excluir conversas do chat de IA"},
	},
	ResourceCampaigns: {
		{ActionName: ActionCreate, Description: "Criar campanhas de chamadas telefônicas"},
		{ActionName: ActionRead, Description: "Visualizar campanhas e seus resultados"},
		{ActionName: ActionUpdate, Description: "Editar configurações de campanhas"},
		{ActionName: ActionDelete, Description: "Excluir campanhas"},
		{ActionName: ActionStart, Description: "Iniciar execução de campanhas"},
		{ActionName: ActionStop, Description: "Parar campanhas em execução"},
	},
	ResourceWhatsAppCampaigns: {
		{ActionName: ActionCreate, Description: "Criar campanhas de WhatsApp"},
		{ActionName: ActionRead, Description: "Visualizar campanhas de WhatsApp"},
		{ActionName: ActionUpdate, Description: "Editar campanhas de WhatsApp"},
		{ActionName: ActionDelete, Description: "Excluir campanhas de WhatsApp"},
		{ActionName: ActionStart, Description: "Iniciar envio de campanhas WhatsApp"},
		{ActionName: ActionStop, Description: "Parar campanhas WhatsApp em execução"},
	},
	ResourceWhatsAppTemplates: {
		{ActionName: ActionCreate, Description: "Criar modelos de mensagem WhatsApp"},
		{ActionName: ActionRead, Description: "Visualizar modelos de mensagem"},
		{ActionName: ActionUpdate, Description: "Editar modelos de mensagem"},
		{ActionName: ActionDelete, Description: "Excluir modelos de mensagem"},
	},
	ResourceBusinessPhones: {
		{ActionName: ActionRead, Description: "Visualizar telefones comerciais do workspace"},
	},
	ResourceStages: {
		{ActionName: ActionCreate, Description: "Criar novas etapas"},
		{ActionName: ActionRead, Description: "Visualizar etapas existentes"},
		{ActionName: ActionUpdate, Description: "Editar etapas"},
		{ActionName: ActionDelete, Description: "Excluir etapas"},
		{ActionName: ActionAssign, Description: "Atribuir etapas a contatos e conversas"},
	},
	ResourceStageGroups: {
		{ActionName: ActionCreate, Description: "Criar funis de etapas"},
		{ActionName: ActionRead, Description: "Visualizar funis de etapas"},
		{ActionName: ActionUpdate, Description: "Editar funis de etapas"},
		{ActionName: ActionDelete, Description: "Excluir funis de etapas"},
	},
	ResourceLabels: {
		{ActionName: ActionCreate, Description: "Criar novas etiquetas"},
		{ActionName: ActionRead, Description: "Visualizar etiquetas existentes"},
		{ActionName: ActionUpdate, Description: "Editar etiquetas"},
		{ActionName: ActionDelete, Description: "Excluir etiquetas"},
		{ActionName: ActionAssign, Description: "Atribuir etiquetas a itens"},
	},
	ResourceMessageShortcuts: {
		{ActionName: ActionCreate, Description: "Criar atalhos de mensagem"},
		{ActionName: ActionRead, Description: "Visualizar atalhos de mensagem"},
		{ActionName: ActionUpdate, Description: "Editar atalhos de mensagem"},
		{ActionName: ActionDelete, Description: "Excluir atalhos de mensagem"},
	},
	ResourceBalance: {
		{ActionName: ActionRead, Description: "Visualizar saldo e créditos da conta"},
	},
	ResourceConversations: {
		{ActionName: ActionCreate, Description: "Iniciar novas conversas com contatos"},
		{ActionName: ActionRead, Description: "Visualizar conversas e histórico de mensagens"},
		{ActionName: ActionUpdate, Description: "Editar informações de conversas"},
		{ActionName: ActionSend, Description: "Enviar mensagens em conversas"},
		{ActionName: ActionReopen, Description: "Reabrir conversas encerradas"},
		{ActionName: ActionAssign, Description: "Passar a conversa para outro membro da equipe", Requires: []PermissionEntry{
			{Resource: ResourceMembers, Action: ActionRead},
		}},
		{ActionName: ActionViewOthers, Description: "Visualizar conversas atribuídas a outros membros da equipe"},
		{ActionName: ActionCall, Description: "Solicitar ao cliente permissão para receber ligações pelo WhatsApp. A ligação em si usa a permissão do discador.", Requires: []PermissionEntry{
			{Resource: ResourceConversations, Action: ActionRead},
			{Resource: ResourceDialer, Action: ActionUse},
		}},
		{ActionName: ActionRoulette, Description: "Participar da roleta de atribuição automática de conversas", Requires: []PermissionEntry{
			{Resource: ResourceConversations, Action: ActionRead},
			{Resource: ResourceConversations, Action: ActionSend},
		}},
	},
	ResourceMedia: {
		{ActionName: ActionCreate, Description: "Enviar arquivos de mídia (imagens, áudios, documentos)"},
		{ActionName: ActionRead, Description: "Visualizar e baixar arquivos de mídia"},
		{ActionName: ActionDelete, Description: "Excluir arquivos de mídia"},
	},
	ResourceLeads: {
		{ActionName: ActionCreate, Description: "Cadastrar novos leads e contatos"},
		{ActionName: ActionRead, Description: "Visualizar leads e informações de contato"},
		{ActionName: ActionBlock, Description: "Bloquear um lead"},
		{ActionName: ActionUpdate, Description: "Editar dados de leads"},
		{ActionName: ActionDelete, Description: "Excluir leads"},
	},
	ResourceAnalysis: {
		{ActionName: ActionRead, Description: "Visualizar relatórios e análises de desempenho"},
	},
	ResourceSIPTrunks: {
		{ActionName: ActionRead, Description: "Visualizar canais de telefonia disponíveis para o workspace"},
		{ActionName: ActionCreate, Description: "Criar novos canais de telefonia para o workspace"},
		{ActionName: ActionUpdate, Description: "Editar canais de telefonia do próprio workspace"},
		{ActionName: ActionDelete, Description: "Excluir canais de telefonia do próprio workspace"},
	},
	ResourceCallRecordings: {
		{ActionName: ActionRead, Description: "Ouvir e baixar gravações de chamadas"},
	},
	ResourceBranches: {
		{ActionName: ActionRead, Description: "Visualizar branches (extensões SIP) do workspace"},
		{ActionName: ActionCreate, Description: "Criar branches para membros do workspace"},
		{ActionName: ActionUpdate, Description: "Editar branches do workspace"},
		{ActionName: ActionDelete, Description: "Excluir branches do workspace"},
		{ActionName: ActionUse, Description: "Usar o próprio branch (registrar um softphone ou telefone IP)", Requires: []PermissionEntry{
			{Resource: ResourceDialer, Action: ActionUse},
		}},
	},
	ResourceMembers: {
		{ActionName: ActionCreate, Description: "Convidar novos membros ao workspace"},
		{ActionName: ActionRead, Description: "Visualizar membros e suas funções"},
		{ActionName: ActionViewOthers, Description: "Visualizar membros de outros departamentos", Requires: []PermissionEntry{
			{Resource: ResourceMembers, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Alterar funções e permissões de membros"},
		{ActionName: ActionDelete, Description: "Remover membros do workspace"},
	},
	ResourceAssignments: {
		{ActionName: ActionCreate, Description: "Atribuir recursos a membros específicos"},
		{ActionName: ActionRead, Description: "Visualizar atribuições de recursos"},
		{ActionName: ActionDelete, Description: "Remover atribuições de recursos"},
	},
	ResourceAttendance: {
		{ActionName: ActionRead, Description: "Visualizar métricas de atendimento e telefonia (volume, tempos, canais, fila, ocupação, equipe e IA)"},
	},
	ResourceKnowledgeBases: {
		{ActionName: ActionCreate, Description: "Criar bases de conhecimento"},
		{ActionName: ActionRead, Description: "Visualizar bases de conhecimento"},
		{ActionName: ActionUpdate, Description: "Editar bases de conhecimento"},
		{ActionName: ActionDelete, Description: "Excluir bases de conhecimento"},
	},
	ResourceShortLinks: {
		{ActionName: ActionCreate, Description: "Criar links curtos"},
		{ActionName: ActionRead, Description: "Visualizar links curtos e suas métricas de acesso"},
		{ActionName: ActionUpdate, Description: "Editar links curtos"},
		{ActionName: ActionDelete, Description: "Excluir links curtos"},
	},
	ResourceUsage: {
		{ActionName: ActionRead, Description: "Visualizar consumo e custos do workspace"},
	},
	ResourceRoles: {
		{ActionName: ActionCreate, Description: "Criar cargos personalizados"},
		{ActionName: ActionRead, Description: "Visualizar cargos"},
		{ActionName: ActionUpdate, Description: "Editar cargos e suas permissões"},
		{ActionName: ActionDelete, Description: "Excluir cargos"},
	},
	ResourceSupportInboxes: {
		{ActionName: ActionCreate, Description: "Criar caixas de entrada de suporte"},
		{ActionName: ActionRead, Description: "Visualizar caixas de entrada de suporte"},
		{ActionName: ActionUpdate, Description: "Editar caixas de entrada de suporte"},
		{ActionName: ActionDelete, Description: "Excluir caixas de entrada de suporte"},
	},
	ResourceIssues: {
		{ActionName: ActionCreate, Description: "Criar issues"},
		{ActionName: ActionRead, Description: "Visualizar issues"},
		{ActionName: ActionUpdate, Description: "Fechar issues"},
	},
	ResourceWorkflows: {
		{ActionName: ActionCreate, Description: "Criar workflows de automação"},
		{ActionName: ActionRead, Description: "Visualizar workflows e execuções"},
		{ActionName: ActionReadDetails, Description: "Visualizar detalhes de configurações e execuções de workflows", Requires: []PermissionEntry{
			{Resource: ResourceWorkflows, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar workflows"},
		{ActionName: ActionDelete, Description: "Excluir workflows"},
	},
	ResourceCalendar: {
		{ActionName: ActionCreate, Description: "Criar eventos no calendário"},
		{ActionName: ActionRead, Description: "Visualizar eventos do calendário"},
		{ActionName: ActionUpdate, Description: "Editar eventos do calendário"},
		{ActionName: ActionDelete, Description: "Excluir eventos do calendário"},
	},
	ResourceDepartments: {
		{ActionName: ActionCreate, Description: "Criar departamentos"},
		{ActionName: ActionRead, Description: "Visualizar departamentos e seus membros"},
		{ActionName: ActionUpdate, Description: "Editar departamentos"},
		{ActionName: ActionDelete, Description: "Excluir departamentos"},
	},
	ResourceDialer: {
		{ActionName: ActionUse, Description: "Realizar chamadas pelo discador (SIP e ligações WhatsApp). Não inclui métricas — use a permissão de atendimento para dashboards.", Requires: []PermissionEntry{
			{Resource: ResourceSIPTrunks, Action: ActionRead},
		}},
		{ActionName: ActionListMembers, Description: "Visualizar membros conectados ao discador em tempo real (necessário para selecionar destino de transferência)", Requires: []PermissionEntry{
			{Resource: ResourceDialer, Action: ActionUse},
		}},
		{ActionName: ActionTransfer, Description: "Transferir chamadas ativas para outro atendente (cega ou atendida)", Requires: []PermissionEntry{
			{Resource: ResourceDialer, Action: ActionUse},
			{Resource: ResourceDialer, Action: ActionListMembers},
		}},
	},
	ResourceAffiliate: {
		{ActionName: ActionUse, Description: "Acessar o programa de afiliados, visualizar indicações e comissões"},
	},
	ResourceMCP: {
		{ActionName: ActionRead, Description: "Visualizar servidores MCP integrados e remotos do workspace"},
		{ActionName: ActionCreate, Description: "Habilitar servidores MCP integrados e registrar servidores MCP remotos"},
		{ActionName: ActionUpdate, Description: "Configurar credenciais (API key / OAuth) de servidores MCP"},
		{ActionName: ActionDelete, Description: "Remover servidores MCP integrados ou remotos"},
	},
	ResourcePlans: {
		{ActionName: ActionRead, Description: "Visualizar planos disponíveis e a assinatura atual do workspace"},
		{ActionName: ActionCreate, Description: "Contratar planos e gerar faturas de cobrança"},
		{ActionName: ActionDelete, Description: "Cancelar a assinatura atual do workspace"},
	},
}

func EnforceDependencies(permissions []PermissionEntry) []PermissionEntry {
	permSet := make(map[string]bool, len(permissions))
	for _, p := range permissions {
		permSet[string(p.Resource)+":"+string(p.Action)] = true
	}

	result := make([]PermissionEntry, 0, len(permissions))
	for _, p := range permissions {
		strip := false
		if defs, ok := ResourceActions[p.Resource]; ok {
			for _, def := range defs {
				if def.ActionName == p.Action && len(def.Requires) > 0 {
					for _, req := range def.Requires {
						if !permSet[string(req.Resource)+":"+string(req.Action)] {
							strip = true
							break
						}
					}
					break
				}
			}
		}
		if !strip {
			result = append(result, p)
		}
	}
	return result
}

func ValidActionForResource(r Resource, a Action) bool {
	actions, ok := ResourceActions[r]
	if !ok {
		return false
	}
	for _, valid := range actions {
		if valid.ActionName == a {
			return true
		}
	}
	return false
}

type Workspace struct {
	ID                 string    `json:"id"`
	OwnerID            string    `json:"ownerId"`
	Name               string    `json:"name"`
	IsDefault          bool      `json:"isDefault"`
	MemberCount        int       `json:"memberCount,omitempty"`
	OwnerName          string    `json:"ownerName,omitempty"`
	OwnerEmail         string    `json:"ownerEmail,omitempty"`
	CurrentUserRole    Role      `json:"currentUserRole,omitempty"`
	PlanName           string    `json:"planName,omitempty"`
	SubscriptionStatus string    `json:"subscriptionStatus,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type Member struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	UserID      string `json:"userId"`
	Role        Role   `json:"role"`
	RoleID      string `json:"roleId,omitempty"`
	RoleName    string `json:"roleName,omitempty"`
	Email       string `json:"email,omitempty"`
	Username    string `json:"username,omitempty"`
	// RingChannels is the member's AOR-level set of endpoint channels that ring on
	// offers/transfers: a session rings only if its channel is in this set. It is
	// intentionally extensible; today the channels are browser (web dialer) and
	// branch (SIP extension). Empty means the default (all channels). Registered
	// branches additionally honor their per-branch enabled/DND on top of this.
	RingChannels []RingChannel `json:"ringChannels"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

// RingChannel identifies a member endpoint kind that can ring on offers and
// transfers. The set is intentionally open for future channels (mobile app, etc.).
type RingChannel string

const (
	RingChannelBrowser RingChannel = "browser" // the web dialer session
	RingChannelBranch  RingChannel = "branch"  // a registered SIP extension (branch)
)

// DefaultRingChannels rings every supported channel: the default for new members
// and the fallback when a member has made no explicit selection.
func DefaultRingChannels() []RingChannel {
	return []RingChannel{RingChannelBrowser, RingChannelBranch}
}

// IsValidRingChannel reports whether c is a known ring channel.
func IsValidRingChannel(c RingChannel) bool {
	return c == RingChannelBrowser || c == RingChannelBranch
}

type Permission struct {
	ID        string    `json:"id"`
	MemberID  string    `json:"memberId"`
	Resource  Resource  `json:"resource"`
	Action    Action    `json:"action"`
	CreatedAt time.Time `json:"createdAt"`
}

type Invite struct {
	ID            string            `json:"id"`
	WorkspaceID   string            `json:"workspaceId"`
	InviterID     string            `json:"inviterId"`
	Email         string            `json:"email"`
	Role          Role              `json:"role"`
	RoleID        string            `json:"roleId,omitempty"`
	Status        InviteStatus      `json:"status"`
	Token         string            `json:"token,omitempty"`
	Permissions   []PermissionEntry `json:"permissions,omitempty"`
	DepartmentIDs []string          `json:"departmentIds,omitempty"`
	ExpiresAt     time.Time         `json:"expiresAt"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`

	WorkspaceName string `json:"workspaceName,omitempty"`
	InviterEmail  string `json:"inviterEmail,omitempty"`
	RoleName      string `json:"roleName,omitempty"`
}

type ResourceAssignment struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspaceId"`
	ResourceType Resource  `json:"resourceType"`
	ResourceID   string    `json:"resourceId"`
	MemberID     string    `json:"memberId"`
	CreatedAt    time.Time `json:"createdAt"`

	MemberEmail    string `json:"memberEmail,omitempty"`
	MemberUsername string `json:"memberUsername,omitempty"`
}
