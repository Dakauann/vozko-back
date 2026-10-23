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
	ResourceGoogleSheets                = registerResource("google_sheets")
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
	ResourceMercadoLivreAccounts        = registerResource("mercadolivre_accounts")
	ResourceTikTokAccounts              = registerResource("tiktok_accounts")
	ResourceWebchatWidgets              = registerResource("webchat_widgets")
	ResourceEmailInboxes                = registerResource("email_inboxes")
	ResourceUnofficialWhatsAppInstances = registerResource("unofficial_whatsapp_instances")
	ResourceUnofficialWhatsAppCampaigns = registerResource("unofficial_whatsapp_campaigns")
	ResourceUnofficialWhatsAppGroups    = registerResource("unofficial_whatsapp_groups")
	ResourceSIPTrunks                   = registerResource("sip_trunks")
	ResourceCallQueues                  = registerResource("call_queues")
	ResourceCallHistory                 = registerResource("call_history")
	ResourceCallLists                   = registerResource("call_lists")
	ResourceAds                         = registerResource("ads")
	ResourceLeadCompanies               = registerResource("lead_companies")
	ResourceCustomerPortfolios          = registerResource("customer_portfolios")
	ResourceWhatsAppForms               = registerResource("whatsapp_forms")
	ResourceWhatsAppFormResponses       = registerResource("whatsapp_form_responses")
	ResourceTeamChat                    = registerResource("team_chat")
	ResourceEmailMarketing              = registerResource("email_marketing")
	ResourceCarrierMessaging            = registerResource("carrier_messaging")
	ResourceCarrierInboxes              = registerResource("carrier_inboxes")
	ResourceAPIKeys                     = registerResource("api_keys")
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
	ActionDelegate    = registerAction("delegate")

	ActionReadSensitive = registerAction("read_sensitive")
	ActionReadAddresses = registerAction("read_addresses")
	ActionAnonymize     = registerAction("anonymize")
	ActionConfigure     = registerAction("configure")
	ActionExport        = registerAction("export")
	ActionBulkUpdate    = registerAction("bulk_update")
	ActionManage        = registerAction("manage")
	ActionConsent       = registerAction("consent")
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
	ResourceTeamChat: {
		{ActionName: ActionCreate, Description: "Criar grupos no chat da equipe; conversar com os colegas não precisa de permissão"},
	},
	ResourceEmailMarketing: {
		{ActionName: ActionRead, Description: "Ver as campanhas de e-mail, os modelos, os remetentes, os custos e os resultados"},
		{ActionName: ActionCreate, Description: "Criar campanhas e modelos de e-mail", Requires: []PermissionEntry{
			{Resource: ResourceEmailMarketing, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar campanhas e modelos, enviar testes, importar consentimentos e validar listas (a validação consome saldo)", Risks: []RiskKind{RiskSpendsBalance}, Requires: []PermissionEntry{
			{Resource: ResourceEmailMarketing, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Excluir rascunhos de campanhas e modelos de e-mail", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceEmailMarketing, Action: ActionRead},
		}},
		{ActionName: ActionStart, Description: "Disparar e retomar campanhas de e-mail e iniciar conversas por e-mail com leads (consome saldo)", Risks: []RiskKind{RiskSpendsBalance, RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceEmailMarketing, Action: ActionRead},
		}},
		{ActionName: ActionStop, Description: "Pausar e parar campanhas de e-mail", Requires: []PermissionEntry{
			{Resource: ResourceEmailMarketing, Action: ActionRead},
		}},
		{ActionName: ActionConfigure, Description: "Conectar domínios, criar remetentes e gerenciar a lista de supressão", Risks: []RiskKind{RiskConnectsAccounts}, Requires: []PermissionEntry{
			{Resource: ResourceEmailMarketing, Action: ActionRead},
		}},
	},
	ResourceCarrierMessaging: {
		{ActionName: ActionRead, Description: "Ver as campanhas de RCS e SMS, os remetentes, as regras de envio e os resultados"},
		{ActionName: ActionCreate, Description: "Criar campanhas de RCS e SMS", Requires: []PermissionEntry{
			{Resource: ResourceCarrierMessaging, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar campanhas de RCS e SMS e enviar testes", Requires: []PermissionEntry{
			{Resource: ResourceCarrierMessaging, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Excluir rascunhos de campanhas de RCS e SMS", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceCarrierMessaging, Action: ActionRead},
		}},
		{ActionName: ActionStart, Description: "Disparar, agendar e retomar campanhas de RCS e SMS (consome saldo)", Risks: []RiskKind{RiskSpendsBalance, RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceCarrierMessaging, Action: ActionRead},
		}},
		{ActionName: ActionStop, Description: "Pausar e parar campanhas de RCS e SMS", Requires: []PermissionEntry{
			{Resource: ResourceCarrierMessaging, Action: ActionRead},
		}},
		{ActionName: ActionConfigure, Description: "Cadastrar e enviar remetentes RCS para aprovação, criar remetentes de SMS, mudar as regras de envio e a lista de descadastro", Risks: []RiskKind{RiskConnectsAccounts}, Requires: []PermissionEntry{
			{Resource: ResourceCarrierMessaging, Action: ActionRead},
		}},
	},
	ResourceCarrierInboxes: {
		{ActionName: ActionRead, Description: "Visualizar os remetentes de RCS e SMS que recebem as respostas no atendimento e a automação de cada um"},
		{ActionName: ActionUpdate, Description: "Escolher o agente ou o fluxo que responde as conversas de cada remetente de RCS e SMS", Risks: []RiskKind{RiskChangesAutomation}},
	},
	ResourceAPIKeys: {
		{ActionName: ActionRead, Description: "Ver as chaves de API do workspace, com o prefixo, as permissões e o último uso"},
		{ActionName: ActionCreate, Description: "Criar chaves de API que enviam e-mails e leem os envios em nome do workspace (os envios consomem saldo)", Risks: []RiskKind{RiskSpendsBalance, RiskContactsCustomers, RiskManagesAccess}, Requires: []PermissionEntry{
			{Resource: ResourceAPIKeys, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Revogar chaves de API; a chave para de funcionar na hora", Risks: []RiskKind{RiskManagesAccess}, Requires: []PermissionEntry{
			{Resource: ResourceAPIKeys, Action: ActionRead},
		}},
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
	ResourceWhatsAppForms: {
		{ActionName: ActionRead, Description: "Ver os formulários do WhatsApp e onde estão publicados"},
		{ActionName: ActionCreate, Description: "Criar formulários do WhatsApp", Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppForms, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Editar o rascunho dos formulários", Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppForms, Action: ActionRead},
		}},
		{ActionName: ActionConfigure, Description: "Publicar e descontinuar formulários na Meta", Risks: []RiskKind{RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppForms, Action: ActionRead},
		}},
		{ActionName: ActionSend, Description: "Enviar formulários nas conversas do WhatsApp oficial", Risks: []RiskKind{RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppForms, Action: ActionRead},
			{Resource: ResourceConversations, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Arquivar formulários; as respostas continuam guardadas", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppForms, Action: ActionRead},
		}},
	},
	ResourceWhatsAppFormResponses: {
		{ActionName: ActionRead, Description: "Ver as respostas dos formulários das conversas que você alcança", Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppForms, Action: ActionRead},
		}},
		{ActionName: ActionReadSensitive, Description: "Ver as respostas marcadas como sensíveis, fotos e documentos", Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppFormResponses, Action: ActionRead},
		}},
		{ActionName: ActionExport, Description: "Exportar as respostas em CSV ou XLSX", Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppFormResponses, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Excluir respostas e os arquivos enviados nelas", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceWhatsAppFormResponses, Action: ActionRead},
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
		{ActionName: ActionDelegate, Description: "Delegar conversas a um agente de IA ou a um fluxo, mesmo em canais sem automação ativa", Risks: []RiskKind{RiskSpendsBalance, RiskChangesAutomation}, Requires: []PermissionEntry{
			{Resource: ResourceConversations, Action: ActionRead},
		}},
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
		{ActionName: ActionAssign, Description: "Definir o responsável por um lead", Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
		}},
		{ActionName: ActionReadSensitive, Description: "Ver os campos personalizados marcados como dados sensíveis, como a classificação política", Risks: []RiskKind{RiskSensitiveData}, Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
		}},
		{ActionName: ActionReadAddresses, Description: "Ver os endereços completos e a posição dos leads no mapa; sem isso aparecem só o bairro e a cidade", Risks: []RiskKind{RiskSensitiveData}, Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
		}},
		{ActionName: ActionAnonymize, Description: "Anonimizar leads, apagando de forma definitiva os dados pessoais de cada um", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
		}},
		{ActionName: ActionConfigure, Description: "Criar, editar e remover os campos personalizados dos leads", Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
		}},
		{ActionName: ActionExport, Description: "Exportar leads selecionados para uma planilha", Risks: []RiskKind{RiskSensitiveData}, Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
		}},
		{ActionName: ActionBulkUpdate, Description: "Aplicar uma mesma alteração a muitos leads de uma vez, como classificar, atribuir ou bloquear uma seleção", Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
			{Resource: ResourceLeads, Action: ActionUpdate},
		}},
		{ActionName: ActionConsent, Description: "Registrar o consentimento do lead para receber mensagens, com a prova de como foi dado; retirar o consentimento só exige editar leads", Requires: []PermissionEntry{
			{Resource: ResourceLeads, Action: ActionRead},
			{Resource: ResourceLeads, Action: ActionUpdate},
		}},
		{ActionName: ActionDelete, Description: "Excluir leads", Risks: []RiskKind{RiskDeletesData}},
	},
	ResourceCallRecordings: {
		{ActionName: ActionRead, Description: "Ouvir e baixar gravações de chamadas", Risks: []RiskKind{RiskSensitiveData}},
	},
	ResourceCallHistory: {
		{ActionName: ActionRead, Description: "Ver o histórico das chamadas de que participou: resultado, duração, transferências e custo"},
		{ActionName: ActionViewOthers, Description: "Ver as chamadas de todos os membros do workspace", Risks: []RiskKind{RiskSensitiveData}, Requires: []PermissionEntry{
			{Resource: ResourceCallHistory, Action: ActionRead},
		}},
	},
	ResourceLeadCompanies: {
		{ActionName: ActionRead, Description: "Ver as empresas dos leads, a imagem de cada uma e quantos leads fazem parte dela"},
		{ActionName: ActionCreate, Description: "Cadastrar empresas para agrupar leads", Requires: []PermissionEntry{
			{Resource: ResourceLeadCompanies, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Renomear empresas, trocar a imagem e ligar ou desligar a entrada de novos leads", Requires: []PermissionEntry{
			{Resource: ResourceLeadCompanies, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Excluir empresas; os leads delas continuam cadastrados, sem empresa", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceLeadCompanies, Action: ActionRead},
		}},
	},
	ResourceCustomerPortfolios: {
		{ActionName: ActionRead, Description: "Ver a própria carteira de clientes"},
		{ActionName: ActionUse, Description: "Colocar clientes na própria carteira e tirá-los dela; as conversas novas desses clientes chegam direto para você, mesmo off-line", Requires: []PermissionEntry{
			{Resource: ResourceCustomerPortfolios, Action: ActionRead},
			{Resource: ResourceConversations, Action: ActionRead},
		}},
		{ActionName: ActionManage, Description: "Ver a carteira de qualquer membro, colocar clientes na carteira de outra pessoa, mover e tirar clientes", Requires: []PermissionEntry{
			{Resource: ResourceCustomerPortfolios, Action: ActionUse},
			{Resource: ResourceMembers, Action: ActionRead},
		}},
	},
	ResourceCallLists: {
		{ActionName: ActionRead, Description: "Ver as listas de ligação de que participa e trabalhar nelas: próximo contato, resultado e nota"},
		{ActionName: ActionManage, Description: "Criar listas de ligação a partir de leads, escolher quem liga, pausar, arquivar e excluir listas e ver todas elas", Risks: []RiskKind{RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceCallLists, Action: ActionRead},
		}},
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
	ResourceAds: {
		{ActionName: ActionCreate, Description: "Conectar contas de anúncios da Meta, criar e duplicar anúncios, públicos, formulários, regras, testes A/B e pixels; anúncios gastam o orçamento da conta de anúncios", Risks: []RiskKind{RiskConnectsAccounts, RiskSpendsBalance}},
		{ActionName: ActionRead, Description: "Ver campanhas, conjuntos, anúncios, resultados, públicos, formulários com os dados dos leads, regras e conversões", Risks: []RiskKind{RiskSensitiveData}},
		{ActionName: ActionUpdate, Description: "Editar campanhas, conjuntos e anúncios, orçamentos, limite de gasto da conta, regras e o envio de conversões para a Meta, e arquivar"},
		{ActionName: ActionStart, Description: "Ligar campanhas, conjuntos e anúncios, o que volta a gastar o orçamento da conta de anúncios"},
		{ActionName: ActionStop, Description: "Desligar campanhas, conjuntos e anúncios"},
		{ActionName: ActionDelete, Description: "Excluir campanhas, conjuntos, anúncios, públicos e regras, e desconectar contas de anúncios", Risks: []RiskKind{RiskConnectsAccounts, RiskDeletesData}},
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
		{ActionName: ActionCreate, Description: "Conectar linhas telefônicas (SIP) com as credenciais da operadora", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar as linhas telefônicas, se estão conectadas e as ligações em andamento"},
		{ActionName: ActionUpdate, Description: "Editar as linhas telefônicas e suas credenciais", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionDelete, Description: "Remover linhas telefônicas, encerrando as ligações em andamento", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionCall, Description: "Fazer e atender ligações pelas linhas telefônicas no discador", Risks: []RiskKind{RiskContactsCustomers}, Requires: []PermissionEntry{
			{Resource: ResourceSIPTrunks, Action: ActionRead},
			{Resource: ResourceCallSession, Action: ActionUse},
		}},
	},
	ResourceCallQueues: {
		{ActionName: ActionCreate, Description: "Criar filas de atendimento telefônico"},
		{ActionName: ActionRead, Description: "Visualizar filas de atendimento telefônico, a música de espera e a espera de cada fila"},
		{ActionName: ActionUpdate, Description: "Editar filas de atendimento telefônico, quem atende cada uma e a música de espera do workspace", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionDelete, Description: "Remover filas de atendimento telefônico; fluxos de voz que enviam ligações a elas deixam de transferir", Risks: []RiskKind{RiskDeletesData, RiskChangesAutomation}},
	},
	ResourceMercadoLivreAccounts: {
		{ActionName: ActionCreate, Description: "Conectar contas de vendedor do Mercado Livre", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar as contas do Mercado Livre, suas permissões e o estado da conexão"},
		{ActionName: ActionUpdate, Description: "Editar as configurações e a automação das contas do Mercado Livre e verificar a conexão", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionDelete, Description: "Desconectar contas do Mercado Livre", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceTikTokAccounts: {
		{ActionName: ActionCreate, Description: "Conectar contas do TikTok", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar as contas do TikTok, os vídeos, os comentários e o estado da conexão"},
		{ActionName: ActionUpdate, Description: "Editar a automação das contas do TikTok e responder, ocultar e curtir comentários", Risks: []RiskKind{RiskContactsCustomers}},
		{ActionName: ActionDelete, Description: "Desconectar contas do TikTok", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceTelegramAccounts: {
		{ActionName: ActionCreate, Description: "Conectar bots do Telegram", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar bots e links de atribuição do Telegram"},
		{ActionName: ActionUpdate, Description: "Editar configurações e links do Telegram"},
		{ActionName: ActionDelete, Description: "Desconectar bots do Telegram", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceWebchatWidgets: {
		{ActionName: ActionCreate, Description: "Criar WebChats", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Visualizar os WebChats, os sites permitidos e o código de instalação"},
		{ActionName: ActionUpdate, Description: "Editar WebChats: sites permitidos, formulário inicial, aparência, verificação de identidade e automação", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionDelete, Description: "Remover WebChats; o chat some dos sites onde foi instalado", Risks: []RiskKind{RiskConnectsAccounts}},
	},
	ResourceEmailInboxes: {
		{ActionName: ActionRead, Description: "Visualizar as caixas de e-mail que recebem as respostas dos clientes no atendimento e as suas configurações"},
		{ActionName: ActionUpdate, Description: "Editar o departamento, o funil e o agente ou fluxo que atende as respostas de cada caixa de e-mail", Risks: []RiskKind{RiskChangesAutomation}},
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
	ResourceUnofficialWhatsAppGroups: {
		{ActionName: ActionRead, Description: "Visualizar os grupos de WhatsApp dos números conectados por QR Code, os membros, as métricas, as mensagens e as análises"},
		{ActionName: ActionCreate, Description: "Entrar em grupos por link de convite e criar grupos com os números conectados por QR Code", Requires: []PermissionEntry{
			{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead},
		}},
		{ActionName: ActionUpdate, Description: "Acompanhar grupos, buscar os grupos dos números e alterar categoria, nome, descrição, foto e configurações", Requires: []PermissionEntry{
			{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead},
		}},
		{ActionName: ActionConfigure, Description: "Criar, editar e pausar as análises de IA dos grupos; cada análise consome saldo", Risks: []RiskKind{RiskSpendsBalance}, Requires: []PermissionEntry{
			{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead},
		}},
		{ActionName: ActionDelete, Description: "Sair de grupos com os números e excluir análises com o histórico", Risks: []RiskKind{RiskDeletesData}, Requires: []PermissionEntry{
			{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead},
		}},
		{ActionName: ActionExport, Description: "Exportar os membros dos grupos, com os telefones, para uma planilha", Risks: []RiskKind{RiskSensitiveData}, Requires: []PermissionEntry{
			{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead},
		}},
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
	ResourceGoogleSheets: {
		{ActionName: ActionCreate, Description: "Conectar o Google Planilhas e criar planilhas no Google Drive do workspace", Risks: []RiskKind{RiskConnectsAccounts}},
		{ActionName: ActionRead, Description: "Ver as planilhas, as colunas e a saúde das gravações"},
		{ActionName: ActionUpdate, Description: "Editar as colunas, adicionar linhas, reenviar linhas retidas e reparar o cabeçalho", Risks: []RiskKind{RiskChangesAutomation}},
		{ActionName: ActionDelete, Description: "Remover planilhas e desconectar o Google Planilhas; agentes e fluxos que gravam nelas deixam de gravar", Risks: []RiskKind{RiskDeletesData, RiskChangesAutomation}},
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
