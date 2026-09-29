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
	ResourceAgents                      = registerResource("agents")
	ResourceWhatsAppCampaigns           = registerResource("whatsapp_campaigns")
	ResourceWhatsAppTemplates           = registerResource("whatsapp_templates")
	ResourceBusinessPhones              = registerResource("business_phones")
	ResourceStages                      = registerResource("stages")
	ResourceStageGroups                 = registerResource("stage_groups")
	ResourceLabels                      = registerResource("labels")
	ResourceBalance                     = registerResource("balance")
	ResourceConversations               = registerResource("conversations")
	ResourceMedia                       = registerResource("media")
	ResourceLeads                       = registerResource("leads")
	ResourceCallRecordings              = registerResource("call_recordings")
	ResourceMembers                     = registerResource("members")
	ResourceAssignments                 = registerResource("assignments")
	ResourceAttendance                  = registerResource("attendance")
	ResourceAttendanceTargets           = registerResource("attendance_targets")
	ResourceReports                     = registerResource("reports")
	ResourceKnowledgeBases              = registerResource("knowledge_bases")
	ResourceRoles                       = registerResource("roles")
	ResourceIssues                      = registerResource("issues")
	ResourceWorkflows                   = registerResource("workflows")
	ResourceCalendar                    = registerResource("calendar")
	ResourceDepartments                 = registerResource("departments")
	ResourceMessageShortcuts            = registerResource("message_shortcuts")
	ResourceCallSession                 = registerResource("call_session")
	ResourceMCP                         = registerResource("mcp")
	ResourcePlans                       = registerResource("plans")
	ResourceAIChat                      = registerResource("ai_chat")
	ResourceShortLinks                  = registerResource("short_links")
	ResourceInstagramAccounts           = registerResource("instagram_accounts")
	ResourceFacebookPages               = registerResource("facebook_pages")
	ResourceAudience                    = registerResource("audience")
	ResourceTelegramAccounts            = registerResource("telegram_accounts")
	ResourceUnofficialWhatsAppInstances = registerResource("unofficial_whatsapp_instances")
	ResourceUnofficialWhatsAppCampaigns = registerResource("unofficial_whatsapp_campaigns")
	ResourceSIPTrunks                   = registerResource("sip_trunks")
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
	Risks       []RiskKind        `json:"risks,omitempty"`
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
		{ActionName: ActionCreate, Description: "Criar novos agentes de IA", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionRead, Description: "Visualizar agentes"},
		{ActionName: ActionReadDetails, Description: "Visualizar as configurações do agente", Requires: []PermissionEntry{
			{Resource: ResourceAgents, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar configurações de agentes", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionDelete, Description: "Excluir agentes", Risks: []RiskKind{RiskDeletesData, RiskChangesAutomation}},
	},
	ResourceAIChat: {
		{ActionName: ActionRead, Description: "Visualizar e usar o chat de IA"},
		{ActionName: ActionCreate, Description: "Criar conversas e enviar mensagens no chat de IA", Risks: []RiskKind{RiskSpendsBalance}},
		{ActionName: ActionUpdate, Description: "Renomear conversas do chat de IA"},
		{ActionName: ActionDelete, Description: "Excluir conversas do chat de IA"},
	},
	ResourceWhatsAppCampaigns: {
		{ActionName: ActionCreate, Description: "Criar campanhas de WhatsApp"},
		{ActionName: ActionRead, Description: "Visualizar campanhas de WhatsApp"},
		{ActionName: ActionUpdate, Description: "Editar campanhas de WhatsApp"},
		{ActionName: ActionDelete, Description: "Excluir campanhas de WhatsApp", Risks: []RiskKind{RiskDeletesData}},
		{ActionName: ActionStart, Description: "Iniciar envio de campanhas WhatsApp", Risks: []RiskKind{RiskSpendsBalance, RiskContactsCustomers}},
		{ActionName: ActionStop, Description: "Parar campanhas WhatsApp em execução"},
	},
	ResourceWhatsAppTemplates: {
		{ActionName: ActionCreate, Description: "Criar modelos de mensagem WhatsApp"},
		{ActionName: ActionRead, Description: "Visualizar modelos de mensagem"},
		{ActionName: ActionUpdate, Description: "Editar modelos de mensagem"},
		{ActionName: ActionDelete, Description: "Excluir modelos de mensagem", Risks: []RiskKind{RiskDeletesData}},
		{ActionName: ActionSend, Description: "Iniciar conversa com um número novo enviando um modelo pelo WhatsApp oficial (consome saldo)", Risks: []RiskKind{RiskSpendsBalance, RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppTemplates, Action: ActionRead},
			{Resource: ResourceBusinessPhones, Action: ActionRead},
			{Resource: ResourceConversations, Action: ActionRead},
		}},
	},
	ResourceBusinessPhones: {
		{ActionName: ActionRead, Description: "Visualizar telefones comerciais do workspace"},
	},
	ResourceStages: {
		{ActionName: ActionCreate, Description: "Criar novas etapas"},
		{ActionName: ActionRead, Description: "Visualizar etapas existentes"},
		{ActionName: ActionUpdate, Description: "Editar etapas"},
		{ActionName: ActionDelete, Description: "Excluir etapas", Risks: []RiskKind{RiskDeletesData}},
		{ActionName: ActionAssign, Description: "Atribuir etapas a contatos e conversas"},
		{ActionName: ActionTransfer, Description: "Mover conversas para outro funil", Requires: []PermissionEntry{
			{Resource: ResourceStages, Action: ActionAssign},
		}},
	},
	ResourceStageGroups: {
		{ActionName: ActionCreate, Description: "Criar funis de etapas"},
		{ActionName: ActionRead, Description: "Visualizar funis de etapas"},
		{ActionName: ActionUpdate, Description: "Editar funis de etapas"},
		{ActionName: ActionDelete, Description: "Excluir funis de etapas", Risks: []RiskKind{RiskDeletesData}},
	},
	ResourceLabels: {
		{ActionName: ActionCreate, Description: "Criar novas etiquetas"},
		{ActionName: ActionRead, Description: "Visualizar etiquetas existentes"},
		{ActionName: ActionUpdate, Description: "Editar etiquetas"},
		{ActionName: ActionDelete, Description: "Excluir etiquetas", Risks: []RiskKind{RiskDeletesData}},
		{ActionName: ActionAssign, Description: "Atribuir etiquetas a itens"},
	},
	ResourceMessageShortcuts: {
		{ActionName: ActionCreate, Description: "Criar atalhos de mensagem"},
		{ActionName: ActionRead, Description: "Visualizar atalhos de mensagem"},
		{ActionName: ActionUpdate, Description: "Editar atalhos de mensagem"},
		{ActionName: ActionDelete, Description: "Excluir atalhos de mensagem", Risks: []RiskKind{RiskDeletesData}},
	},
	ResourceBalance: {
		{ActionName: ActionRead, Description: "Visualizar saldo, créditos e faturas da conta", Risks: []RiskKind{RiskSensitiveData}},
	},
	ResourceConversations: {
		{ActionName: ActionCreate, Description: "Iniciar novas conversas com contatos", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionRead, Description: "Visualizar conversas e histórico de mensagens"},
		{ActionName: ActionUpdate, Description: "Editar informações de conversas"},
		{ActionName: ActionSend, Description: "Enviar mensagens em conversas"},
		{ActionName: ActionReopen, Description: "Reabrir conversas encerradas", Risks: []RiskKind{RiskSpendsBalance}},
		{ActionName: ActionAssign, Description: "Passar a conversa para outro membro da equipe", Requires: []PermissionEntry{
			{Resource: ResourceMembers, Action: ActionRead},
		}},
		{ActionName: ActionViewOthers, Description: "Visualizar conversas atribuídas a outros membros da equipe", Risks: []RiskKind{RiskSensitiveData}},
		{ActionName: ActionCall, Description: "Solicitar ao cliente permissão para receber ligações pelo WhatsApp. A ligação em si usa a permissão de chamadas.", Requires: []PermissionEntry{
			{Resource: ResourceConversations, Action: ActionRead},
			{Resource: ResourceCallSession, Action: ActionUse},
		}},
		{ActionName: ActionRoulette, Description: "Participar da roleta de atribuição automática de conversas", Requires: []PermissionEntry{
			{Resource: ResourceConversations, Action: ActionRead},
			{Resource: ResourceConversations, Action: ActionSend},
		}},
	},
	ResourceMedia: {
		{ActionName: ActionCreate, Description: "Enviar arquivos de mídia (imagens, áudios, documentos)"},
		{ActionName: ActionRead, Description: "Visualizar e baixar arquivos de mídia"},
		{ActionName: ActionDelete, Description: "Excluir arquivos de mídia", Risks: []RiskKind{RiskDeletesData}},
	},
	ResourceLeads: {
		{ActionName: ActionCreate, Description: "Cadastrar novos leads e contatos"},
		{ActionName: ActionRead, Description: "Visualizar leads e informações de contato"},
		{ActionName: ActionBlock, Description: "Bloquear um lead"},
		{ActionName: ActionUpdate, Description: "Editar dados de leads"},
		{ActionName: ActionDelete, Description: "Excluir leads", Risks: []RiskKind{RiskDeletesData}},
	},
	ResourceCallRecordings: {
		{ActionName: ActionRead, Description: "Ouvir e baixar gravações de chamadas", Risks: []RiskKind{RiskSensitiveData}},
	},
	ResourceMembers: {
		{ActionName: ActionCreate, Description: "Convidar novos membros ao workspace", Risks: []RiskKind{RiskManagesAccess}},
		{ActionName: ActionRead, Description: "Visualizar membros e suas funções"},
		{ActionName: ActionViewOthers, Description: "Visualizar membros de outros departamentos", Requires: []PermissionEntry{
			{Resource: ResourceMembers, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Alterar funções e permissões de membros", Risks: []RiskKind{RiskManagesAccess}},
		{ActionName: ActionDelete, Description: "Remover membros do workspace", Risks: []RiskKind{RiskManagesAccess}},
	},
	ResourceAssignments: {
		{ActionName: ActionCreate, Description: "Atribuir recursos a membros específicos", Risks: []RiskKind{RiskManagesAccess}},
		{ActionName: ActionRead, Description: "Visualizar atribuições de recursos"},
		{ActionName: ActionDelete, Description: "Remover atribuições de recursos", Risks: []RiskKind{RiskManagesAccess}},
	},
	ResourceAttendance: {
		{ActionName: ActionRead, Description: "Visualizar métricas de atendimento: conversas, tempos, filas, ocupação, canais, equipe e atendimentos por IA nos canais de mensagens, além do volume de telefonia"},
	},
	ResourceReports: {
		{ActionName: ActionRead, Description: "Visualizar e baixar relatórios gerados", Risks: []RiskKind{RiskSensitiveData}},
		{ActionName: ActionCreate, Description: "Solicitar a geração de relatórios", Risks: []RiskKind{RiskSensitiveData}},
	},
	ResourceAttendanceTargets: {
		{ActionName: ActionRead, Description: "Visualizar as metas de atendimento do período", Requires: []PermissionEntry{
			{Resource: ResourceAttendance, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Definir as metas de atendimento do período", Requires: []PermissionEntry{
			{Resource: ResourceAttendance, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Remover metas de atendimento", Requires: []PermissionEntry{
			{Resource: ResourceAttendance, Action: ActionRead},
		}},
	},
	ResourceKnowledgeBases: {
		{ActionName: ActionCreate, Description: "Criar bases de conhecimento", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionRead, Description: "Visualizar bases de conhecimento"},
		{ActionName: ActionUpdate, Description: "Editar bases de conhecimento", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionDelete, Description: "Excluir bases de conhecimento", Risks: []RiskKind{RiskDeletesData, RiskChangesAutomation}},
	},
	ResourceShortLinks: {
		{ActionName: ActionCreate, Description: "Criar links curtos"},
		{ActionName: ActionRead, Description: "Visualizar links curtos e suas métricas de acesso"},
		{ActionName: ActionUpdate, Description: "Editar links curtos"},
		{ActionName: ActionDelete, Description: "Excluir links curtos", Risks: []RiskKind{RiskDeletesData}},
	},
	ResourceRoles: {
		{ActionName: ActionCreate, Description: "Criar cargos personalizados", Risks: []RiskKind{RiskManagesAccess}},
		{ActionName: ActionRead, Description: "Visualizar cargos"},
		{ActionName: ActionUpdate, Description: "Editar cargos e suas permissões", Risks: []RiskKind{RiskManagesAccess}},
		{ActionName: ActionDelete, Description: "Excluir cargos", Risks: []RiskKind{RiskManagesAccess}},
	},
	ResourceFacebookPages: {
		{ActionName: ActionCreate, Description: "Conectar páginas do Facebook", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar páginas, publicações e comentários do Facebook"},
		{ActionName: ActionUpdate, Description: "Editar configurações, publicar e moderar comentários do Facebook", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionDelete, Description: "Desconectar páginas do Facebook", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceInstagramAccounts: {

		{ActionName: ActionCreate, Description: "Conectar contas do Instagram", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar contas, publicações e comentários do Instagram"},
		{ActionName: ActionUpdate, Description: "Editar configurações, publicar e moderar comentários do Instagram", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionDelete, Description: "Desconectar contas do Instagram", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceAudience: {
		{ActionName: ActionRead, Description: "Visualizar a análise de comentários e conversas (audiência, temas, assuntos, autores)"},
		{ActionName: ActionUpdate, Description: "Configurar a análise de comentários e conversas (teto de análises, tempo de silêncio, temas), moderar autores e iniciar reprocessamentos", Risks: []RiskKind{RiskSpendsBalance}},
		{ActionName: ActionSend, Description: "Encaminhar comentários, responder publicamente e configurar alertas automáticos por WhatsApp", Risks: []RiskKind{RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceAudience, Action: ActionRead},
			{Resource: ResourceConversations, Action: ActionSend},
		}},
	},
	ResourceSIPTrunks: {
		{ActionName: ActionCreate, Description: "Cadastrar troncos SIP com as credenciais do provedor de telefonia", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar troncos SIP, o estado do registro e as chamadas em andamento"},
		{ActionName: ActionUpdate, Description: "Editar troncos SIP e suas credenciais", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionDelete, Description: "Remover troncos SIP, encerrando as chamadas em andamento", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionCall, Description: "Fazer e atender ligações pelos troncos SIP no discador", Risks: []RiskKind{RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceSIPTrunks, Action: ActionRead},
			{Resource: ResourceCallSession, Action: ActionUse},
		}},
	},
	ResourceTelegramAccounts: {
		{ActionName: ActionCreate, Description: "Conectar bots do Telegram", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar bots e links de atribuição do Telegram"},
		{ActionName: ActionUpdate, Description: "Editar configurações e links do Telegram"},
		{ActionName: ActionDelete, Description: "Desconectar bots do Telegram", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceUnofficialWhatsAppInstances: {
		{ActionName: ActionCreate, Description: "Conectar números de WhatsApp por QR Code", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar números de WhatsApp conectados por QR Code"},
		{ActionName: ActionUpdate, Description: "Editar configurações e reconectar números de WhatsApp por QR Code", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionDelete, Description: "Desconectar números de WhatsApp por QR Code", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionSend, Description: "Iniciar conversa com um número novo pelo WhatsApp não oficial", Risks: []RiskKind{RiskContactsCustomers}},
	},
	ResourceUnofficialWhatsAppCampaigns: {
		{ActionName: ActionCreate, Description: "Criar campanhas de WhatsApp não oficial"},
		{ActionName: ActionRead, Description: "Visualizar campanhas de WhatsApp não oficial"},
		{ActionName: ActionUpdate, Description: "Editar campanhas de WhatsApp não oficial"},
		{ActionName: ActionDelete, Description: "Excluir campanhas de WhatsApp não oficial", Risks: []RiskKind{RiskDeletesData}},
		{ActionName: ActionStart, Description: "Iniciar disparos de campanhas de WhatsApp não oficial", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionStop, Description: "Pausar ou parar campanhas de WhatsApp não oficial em execução"},
	},
	ResourceIssues: {
		{ActionName: ActionCreate, Description: "Criar issues"},
		{ActionName: ActionRead, Description: "Visualizar issues"},
		{ActionName: ActionUpdate, Description: "Fechar issues"},
	},
	ResourceWorkflows: {
		{ActionName: ActionCreate, Description: "Criar workflows de automação", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionRead, Description: "Visualizar workflows e execuções"},
		{ActionName: ActionReadDetails, Description: "Visualizar detalhes de configurações e execuções de workflows", Requires: []PermissionEntry{
			{Resource: ResourceWorkflows, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar workflows", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionDelete, Description: "Excluir workflows", Risks: []RiskKind{RiskDeletesData, RiskChangesAutomation}},
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
		{ActionName: ActionUpdate, Description: "Editar departamentos", Risks: []RiskKind{RiskManagesAccess}},
		{ActionName: ActionDelete, Description: "Excluir departamentos", Risks: []RiskKind{RiskManagesAccess}},
	},
	ResourceCallSession: {
		{ActionName: ActionUse, Description: "Realizar e atender chamadas de WhatsApp nas conversas. Não inclui métricas, use a permissão de atendimento para dashboards.", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionListMembers, Description: "Visualizar membros conectados às chamadas em tempo real", Requires: []PermissionEntry{
			{Resource: ResourceCallSession, Action: ActionUse},
		}},
		{ActionName: ActionTransfer, Description: "Transferir chamadas ativas para outro atendente (cega ou atendida)", Requires: []PermissionEntry{
			{Resource: ResourceCallSession, Action: ActionUse},
			{Resource: ResourceCallSession, Action: ActionListMembers},
		}},
	},
	ResourceMCP: {
		{ActionName: ActionRead, Description: "Visualizar servidores MCP integrados e remotos do workspace"},
		{ActionName: ActionCreate, Description: "Habilitar servidores MCP integrados e registrar servidores MCP remotos", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionUpdate, Description: "Configurar credenciais (API key / OAuth) de servidores MCP", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionDelete, Description: "Remover servidores MCP integrados ou remotos", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourcePlans: {
		{ActionName: ActionRead, Description: "Visualizar planos disponíveis e a assinatura atual do workspace"},
		{ActionName: ActionCreate, Description: "Contratar planos e gerar faturas de cobrança", Risks: []RiskKind{RiskChangesBilling}},
		{ActionName: ActionDelete, Description: "Cancelar a assinatura atual do workspace", Risks: []RiskKind{RiskChangesBilling}},
	},
}

func DropRetiredResources(permissions []PermissionEntry) ([]PermissionEntry, []Resource) {
	kept := make([]PermissionEntry, 0, len(permissions))
	var dropped []Resource
	seen := make(map[Resource]bool)
	for _, p := range permissions {
		if p.Resource.IsValid() {
			kept = append(kept, p)
			continue
		}
		if !seen[p.Resource] {
			seen[p.Resource] = true
			dropped = append(dropped, p.Resource)
		}
	}
	return kept, dropped
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
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	UserID      string    `json:"userId"`
	Role        Role      `json:"role"`
	RoleID      string    `json:"roleId,omitempty"`
	RoleName    string    `json:"roleName,omitempty"`
	Email       string    `json:"email,omitempty"`
	Username    string    `json:"username,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Permission struct {
	ID        string    `json:"id"`
	MemberID  string    `json:"memberId"`
	Resource  Resource  `json:"resource"`
	Action    Action    `json:"action"`
	CreatedAt time.Time `json:"createdAt"`
}

func (p Permission) Entry() PermissionEntry {
	return PermissionEntry{Resource: p.Resource, Action: p.Action}
}

func EntriesOf(perms []*Permission) []PermissionEntry {
	out := make([]PermissionEntry, 0, len(perms))
	for _, p := range perms {
		if p != nil {
			out = append(out, p.Entry())
		}
	}
	return out
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
