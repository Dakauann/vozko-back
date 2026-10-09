package conversation_usecase

import (
	"fmt"
	"slices"
	"strings"

	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/stage"
)

const (
	recentMessageCount    = 8
	earlierHistoryHeading = "HISTÓRICO ANTERIOR (só contexto)"
	recentMessagesHeading = "MENSAGENS MAIS RECENTES (definem o estado atual)"
	latestPositionRule    = "A conversa pode mudar de rumo: o cliente pode desistir e voltar, ou pedir algo e cancelar depois. Quando as mensagens se contradizem, vale a posição MAIS RECENTE; o histórico anterior serve só de contexto."
)

func transcriptLines(history []*conversation.Message) []string {
	lines := make([]string, 0, len(history))
	for _, msg := range history {
		if !msg.Transcribable() {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s\n", transcriptRole(msg), strings.TrimSpace(msg.Text)))
	}
	return lines
}

func BuildTranscript(history []*conversation.Message) string {
	return strings.Join(transcriptLines(history), "")
}

type recencyTranscript struct {
	earlier string
	recent  string
}

func splitByRecency(history []*conversation.Message) recencyTranscript {
	lines := transcriptLines(history)
	split := max(len(lines)-recentMessageCount, 0)
	var earlier string
	if split > 0 {
		earlier = earlierHistoryHeading + "\n" + strings.Join(lines[:split], "")
	}
	return recencyTranscript{earlier: earlier, recent: recentMessagesHeading + "\n" + strings.Join(lines[split:], "")}
}

const (
	analysisHistoryWindow = 100
	autoTaskNoteTail      = 1
	autoStageFeature      = "auto_stage"
	autoMemoryFeature     = "auto_memory"
	autoDealsFeature      = "auto_deals"
)

type AutoTaskPrompt struct {
	System   string
	Messages []ai.Message
	Scope    string
}

func (p AutoTaskPrompt) ApplyTo(input ai.GenerateInput) ai.GenerateInput {
	input.SystemPrompt = p.System
	input.Messages = p.Messages
	input.VolatileTail = autoTaskNoteTail
	input.SessionID = p.Scope
	input.BillingReference = p.Scope
	return input
}

type autoTask struct {
	feature     string
	entryID     string
	system      string
	history     []*conversation.Message
	facts       []string
	instruction string
}

func (t autoTask) prompt() AutoTaskPrompt {
	window := t.history[ai.HistoryWindowStart(len(t.history), analysisHistoryWindow):]
	transcript := splitByRecency(window)
	var messages []ai.Message
	if transcript.earlier != "" {
		messages = append(messages, ai.Message{Role: ai.RoleUser, Content: transcript.earlier})
	}
	note := ai.ContextNote(slices.Concat(t.facts, []string{transcript.recent, t.instruction})...)
	return AutoTaskPrompt{
		System:   t.system,
		Messages: append(messages, note),
		Scope:    t.feature + ":" + t.entryID,
	}
}

func messageCountLine(count int) string {
	return fmt.Sprintf("- Total de mensagens na conversa: %d", count)
}

func transcriptRole(msg *conversation.Message) string {
	if msg.FromCustomer() {
		return "User"
	}
	return "Agent"
}

const (
	autoMemoryRelativesRule = "\n- Data de nascimento e parentes não são memória. Parentes são leads ligados na aba Família do cadastro, registrados pela equipe."
	autoMemoryProfileRule   = "\n- Quando o lead disser a data de nascimento, chame update_lead_profile em vez de salvar memória: ela só preenche a data quando o cadastro está vazio e avisa a equipe quando o cadastro já tinha outra data. Bairro, endereço e outros dados de cadastro ficam para a equipe."
	autoMemoryNoProfileRule = "\n- Não salve a data de nascimento: ela pertence ao cadastro do lead, que a equipe atualiza."
)

func autoMemoryProfileTask(profileTool bool) string {
	if profileTool {
		return " A data de nascimento que o lead informar vai para o cadastro com a ferramenta update_lead_profile."
	}
	return ""
}

func autoMemoryRulesFor(profileTool bool) string {
	if profileTool {
		return autoMemoryRules + autoMemoryRelativesRule + autoMemoryProfileRule
	}
	return autoMemoryRules + autoMemoryRelativesRule + autoMemoryNoProfileRule
}

const autoMemoryRules = `- Salve apenas FATOS duráveis e declarativos sobre o lead (preferências, orçamento, combinados, objeções, contexto pessoal relevante).
- NÃO salve trivialidades, dados já visíveis (nome, telefone), instruções, nem trechos da conversa.
- Antes de salvar, confira o bloco "Memórias sobre este lead": se o fato já existe, use action='update' com o memory_id em vez de criar outro.
- Se um fato salvo deixou de valer ou o lead pediu para esquecer, use action='forget' com o memory_id.
- NÃO salve o estado do atendimento ou da negociação (quer continuar, desistiu, voltou atrás, quer finalizar, agendou e cancelou): isso é a etapa do funil, não memória. Memória é só fato durável sobre a pessoa ou o combinado vigente.
- Use action='forget' só com um memory_id que esteja no bloco "Memórias sobre este lead"; sem memórias salvas, nunca use 'forget'.
- Se a conversa não trouxe nenhum fato durável novo ou alterado, NÃO chame a ferramenta manage_lead_memory.
- ` + latestPositionRule

type AutoMemoryPromptInput struct {
	EntryID         string
	ContainerName   string
	ContactLabel    string
	MessageCount    int
	CurrentMemories string
	History         []*conversation.Message
	ProfileTool     bool
}

func BuildAutoMemoryPrompt(input AutoMemoryPromptInput) AutoTaskPrompt {
	memories := input.CurrentMemories
	if strings.TrimSpace(memories) == "" {
		memories = "(nenhuma memória salva sobre este lead até agora)"
	}
	system := fmt.Sprintf(`Você é responsável por manter a memória de longo prazo sobre um lead em um CRM. Sua ÚNICA tarefa é ler a transcrição abaixo e, usando a ferramenta manage_lead_memory, registrar fatos duráveis novos, corrigir os que mudaram e apagar os que deixaram de valer.%s

═══════════════════════════════════════════════════
CONTEXTO
═══════════════════════════════════════════════════
- Campanha/canal: %s
- Contato: %s

═══════════════════════════════════════════════════
REGRAS
═══════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════
TRANSCRIÇÃO DA CONVERSA
═══════════════════════════════════════════════════`,
		autoMemoryProfileTask(input.ProfileTool),
		input.ContainerName,
		input.ContactLabel,
		autoMemoryRulesFor(input.ProfileTool),
	)
	instruction := autoMemoryInstruction
	if input.ProfileTool {
		instruction = autoMemoryProfileInstruction
	}
	return autoTask{
		feature:     autoMemoryFeature,
		entryID:     input.EntryID,
		system:      system,
		history:     input.History,
		facts:       []string{messageCountLine(input.MessageCount), memories},
		instruction: instruction,
	}.prompt()
}

type AutoTagPromptInput struct {
	EntryID        string
	CampaignName   string
	MessageCount   int
	History        []*conversation.Message
	CurrentTagName string
	Tags           []*stage.Stage
}

func BuildAutoTagPrompt(input AutoTagPromptInput) AutoTaskPrompt {
	var tagList strings.Builder
	for _, t := range input.Tags {
		desc := t.Description
		if desc == "" {
			desc = "(sem descrição)"
		}
		tagList.WriteString(fmt.Sprintf("  • \"%s\", %s\n", t.Name, desc))
	}

	currentTagSection := "nenhuma (lead ainda não classificado)"
	if input.CurrentTagName != "" {
		currentTagSection = fmt.Sprintf("\"%s\"", input.CurrentTagName)
	}

	system := fmt.Sprintf(`Você é um classificador de leads altamente experiente. Sua ÚNICA tarefa é ler a transcrição abaixo e determinar em qual tag o lead deve estar AGORA.

═══════════════════════════════════════════════════
CONTEXTO
═══════════════════════════════════════════════════
- Campanha: %s

═══════════════════════════════════════════════════
TAGS DISPONÍVEIS (com descrições)
═══════════════════════════════════════════════════
%s
═══════════════════════════════════════════════════
COMO CLASSIFICAR
═══════════════════════════════════════════════════

PASSO 1: Leia o histórico anterior só para entender o contexto.
PASSO 2: Identifique o ESTADO ATUAL pelas mensagens mais recentes.
PASSO 3: Compare a descrição de CADA tag disponível com o estado atual da conversa.
PASSO 4: Escolha a tag cuja descrição MELHOR descreve a situação ATUAL do lead.

IMPORTANTE:
- %s
- Se a tag atual já descreve corretamente o estado do lead, MANTENHA ela (não chame a ferramenta ou passe a mesma tag).
- Se a tag atual NÃO corresponde mais ao estado real da conversa, MUDE para a tag correta.
- Leia as descrições das tags com atenção, a classificação deve bater com a descrição.

═══════════════════════════════════════════════════
TRANSCRIÇÃO DA CONVERSA
═══════════════════════════════════════════════════`,
		input.CampaignName,
		tagList.String(),
		latestPositionRule,
	)
	return autoTask{
		feature:     autoStageFeature,
		entryID:     input.EntryID,
		system:      system,
		history:     input.History,
		facts:       []string{messageCountLine(input.MessageCount) + "\n- Tag ATUAL do lead: " + currentTagSection},
		instruction: autoTagInstruction,
	}.prompt()
}

const autoDealRules = `- Mantenha as oportunidades desta conversa no funil usando a ferramenta auto_manage_opportunity.
- create: quando o cliente demonstra intenção real de compra e ainda não há oportunidade aberta para esse interesse. create_new: só para um contrato diferente dos que já existem.
- update_value: quando um valor é combinado ou alterado. move: quando a negociação avança para outra etapa aberta.
- win: quando o cliente CONFIRMA a compra, sempre com o valor fechado em "value".
- lose: só quando desistir é a posição MAIS RECENTE do cliente, com o motivo em "lost_reason". Se ele desistiu e depois voltou atrás, a oportunidade continua aberta.
- Quando houver mais de uma oportunidade aberta, informe em "opportunity_id" o id exato da lista abaixo.
- NUNCA invente valores nem oportunidades. Se nada mudou nas oportunidades, não chame a ferramenta.
- ` + latestPositionRule

const (
	autoTagInstruction    = "Siga os passos do sistema: descubra a posição MAIS RECENTE do cliente nas mensagens mais recentes e chame manage_entry_stage com a etapa que a descreve. Se a etapa atual já está correta, passe a mesma etapa."
	autoMemoryInstruction = "Siga as instruções do sistema: gerencie a memória do lead com a ferramenta manage_lead_memory. Se não houver fatos duráveis novos ou alterados, não chame nenhuma ferramenta."
	autoDealInstruction   = "Siga as instruções do sistema: mantenha as oportunidades da conversa com a ferramenta auto_manage_opportunity, pela posição MAIS RECENTE do cliente. Se nada mudou nas oportunidades, não chame nenhuma ferramenta."

	autoMemoryProfileInstruction = "Siga as instruções do sistema: gerencie a memória do lead com a ferramenta manage_lead_memory e leve a data de nascimento ao cadastro com update_lead_profile. Se não houver nada novo ou alterado, não chame nenhuma ferramenta."
)

type AutoDealPromptInput struct {
	EntryID       string
	ContainerName string
	ContactLabel  string
	MessageCount  int
	CurrentDeals  string
	History       []*conversation.Message
}

func BuildAutoDealPrompt(input AutoDealPromptInput) AutoTaskPrompt {
	system := fmt.Sprintf(`Você mantém as oportunidades de venda de uma conversa em um CRM. Sua ÚNICA tarefa é ler a transcrição abaixo e, usando a ferramenta auto_manage_opportunity, registrar o que mudou nas oportunidades desta conversa.

═══════════════════════════════════════════════════
CONTEXTO
═══════════════════════════════════════════════════
- Campanha/canal: %s
- Contato: %s

═══════════════════════════════════════════════════
REGRAS
═══════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════
TRANSCRIÇÃO DA CONVERSA
═══════════════════════════════════════════════════`,
		input.ContainerName,
		input.ContactLabel,
		autoDealRules,
	)
	return autoTask{
		feature:     autoDealsFeature,
		entryID:     input.EntryID,
		system:      system,
		history:     input.History,
		facts:       []string{messageCountLine(input.MessageCount), input.CurrentDeals},
		instruction: autoDealInstruction,
	}.prompt()
}
