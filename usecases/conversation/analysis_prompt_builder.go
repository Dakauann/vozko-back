package conversation_usecase

import (
	"fmt"
	"strings"

	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/stage"
)

type AnalysisType string

const (
	AnalysisTypeOngoing   AnalysisType = "ongoing"
	AnalysisTypeCompleted AnalysisType = "completed"
)

type AnalysisPromptInput struct {
	AnalysisType      AnalysisType
	CampaignName      string
	UserPhoneNumber   string
	MessageCount      int
	AgentInstructions string
	History           []*conversation.Message
}

func BuildAnalysisPrompt(input AnalysisPromptInput) string {
	transcript := BuildTranscript(input.History)

	agentInstructionsSection := ""
	if input.AgentInstructions != "" {
		instructions := input.AgentInstructions

		if len(instructions) > 4000 {
			instructions = instructions[:4000] + "..."
		}
		agentInstructionsSection = fmt.Sprintf("\nAgent Instructions (what the agent is supposed to do):\n%s\n", instructions)
	}

	if input.AnalysisType == AnalysisTypeCompleted {
		return buildCompletedCallPrompt(input.CampaignName, input.UserPhoneNumber, agentInstructionsSection, transcript)
	}
	return buildOngoingConversationPrompt(input.CampaignName, input.MessageCount, agentInstructionsSection, transcript)
}

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

func BuildRecencyTranscript(history []*conversation.Message) string {
	lines := transcriptLines(history)
	split := max(len(lines)-recentMessageCount, 0)
	var transcript strings.Builder
	if split > 0 {
		transcript.WriteString(earlierHistoryHeading + "\n")
		transcript.WriteString(strings.Join(lines[:split], ""))
		transcript.WriteString("\n")
	}
	transcript.WriteString(recentMessagesHeading + "\n")
	transcript.WriteString(strings.Join(lines[split:], ""))
	return transcript.String()
}

func transcriptRole(msg *conversation.Message) string {
	if msg.FromCustomer() {
		return "User"
	}
	return "Agent"
}

func buildOngoingConversationPrompt(campaignName string, messageCount int, agentInstructions, transcript string) string {
	return fmt.Sprintf(`Você é um supervisor sênior de atendimento e analista de qualidade. Avalie de forma rigorosa e imparcial o atendimento EM ANDAMENTO, diagnosticando a performance do atendente e o progresso real da conversa.

CONTEXTO
- Campanha: %s
- Total de mensagens trocadas: %d
%s

OBJETIVO
O OBJETIVO desta conversa é definido pelas instruções do agente acima e pelo contexto da campanha, pode ser venda, agendamento, suporte, qualificação, cobrança, ou qualquer outro. Se não houver instruções, deduza o objetivo pelo fluxo. Avalie TODOS os critérios SEMPRE em relação a esse objetivo; NÃO pressuponha que seja venda. Não infle resultados positivos sem evidências concretas na transcrição.

CRITÉRIOS DE CLASSIFICAÇÃO (use a ferramenta conversation_analysis)
%s
%s
- summary: Resumo executivo profissional em PORTUGUÊS (2-4 frases): estado atual em relação ao objetivo, o que o atendente fez bem e o que pode melhorar, e a perspectiva de avanço.

REGRAS DE OURO
1. Nunca confunda educação/cordialidade com interesse real no objetivo.
2. Nunca classifique "sale" sem o evento de conversão CONCLUÍDO e confirmado.
3. Avalie a conversa COMO UM TODO, não apenas a última mensagem.
4. Quando não houver instruções do agente, avalie pela condução geral e profissionalismo.
5. Para "hot_lead", procure evidências de avanço, fit e próximo passo, não dependa exclusivamente de pergunta sobre preço.

Transcrição:
%s`, campaignName, messageCount, agentInstructions, ca.ConversationRubricPrompt(), ca.ConversationQualityRubricPrompt(), transcript)
}

func buildCompletedCallPrompt(campaignName, userPhoneNumber, agentInstructions, transcript string) string {
	return fmt.Sprintf(`Você é um analista profissional de conversas. Analise esta CHAMADA DE VOZ CONCLUÍDA e registre uma análise estruturada, de forma rigorosa e realista, não infle resultados positivos sem evidências concretas.

CONTEXTO
- Campanha: %s
- Telefone: %s
%s

OBJETIVO
O OBJETIVO da ligação é definido pelas instruções do agente acima e pelo contexto da campanha (venda, agendamento, suporte, qualificação, cobrança, etc.). Avalie TUDO em relação a esse objetivo; não pressuponha que seja venda.

CASOS ESPECIAIS (voz)
- Se o usuário NÃO FALOU NADA (apenas o agente falou): interest "undecided", disposition "no_answer", sentiment "neutral", qualification "cold_lead", next_action "schedule_callback" ou "send_whatsapp".
- Ligação curta com respostas monossilábicas ("pode", "ok", "sim") sem perguntas e sem engajamento: interest "undecided", disposition "pending", qualification "cold_lead".
- Se a ligação caiu ou foi transferida por motivo técnico (não por recusa do usuário), avalie apenas o trecho que ocorreu, sem penalizar o atendente.

CRITÉRIOS DE CLASSIFICAÇÃO (use a ferramenta conversation_analysis)
%s
%s
- summary: Breve resumo profissional do resultado da chamada em PORTUGUÊS, em relação ao objetivo. Se o usuário não falou nada, mencione isso.

Transcrição:
%s`, campaignName, userPhoneNumber, agentInstructions, ca.ConversationRubricPrompt(), ca.ConversationQualityRubricPrompt(), transcript)
}

const autoMemoryRules = `- Salve apenas FATOS duráveis e declarativos sobre o lead (preferências, orçamento, datas importantes, combinados, objeções, contexto pessoal relevante).
- NÃO salve trivialidades, dados já visíveis (nome, telefone), instruções, nem trechos da conversa.
- Antes de salvar, confira o bloco "Memórias sobre este lead": se o fato já existe, use action='update' com o memory_id em vez de criar outro.
- Se um fato salvo deixou de valer ou o lead pediu para esquecer, use action='forget' com o memory_id.
- NÃO salve o estado do atendimento ou da negociação (quer continuar, desistiu, voltou atrás, quer finalizar, agendou e cancelou): isso é a etapa do funil, não memória. Memória é só fato durável sobre a pessoa ou o combinado vigente.
- Use action='forget' só com um memory_id que esteja na lista de memórias acima; sem memórias salvas, nunca use 'forget'.
- Se a conversa não trouxe nenhum fato durável novo ou alterado, NÃO chame a ferramenta manage_lead_memory.
- ` + latestPositionRule

type AutoMemoryPromptInput struct {
	ContainerName   string
	ContactLabel    string
	MessageCount    int
	CurrentMemories string
	History         []*conversation.Message
}

func BuildAutoMemoryPrompt(input AutoMemoryPromptInput) string {
	memories := input.CurrentMemories
	if strings.TrimSpace(memories) == "" {
		memories = "\n(nenhuma memória salva sobre este lead até agora)\n"
	}
	return fmt.Sprintf(`Você é responsável por manter a memória de longo prazo sobre um lead em um CRM. Sua ÚNICA tarefa é ler a transcrição abaixo e, usando a ferramenta manage_lead_memory, registrar fatos duráveis novos, corrigir os que mudaram e apagar os que deixaram de valer.

═══════════════════════════════════════════════════
CONTEXTO
═══════════════════════════════════════════════════
- Campanha/canal: %s
- Contato: %s
- Total de mensagens na conversa: %d
%s
═══════════════════════════════════════════════════
REGRAS
═══════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════
TRANSCRIÇÃO DA CONVERSA
═══════════════════════════════════════════════════
%s`,
		input.ContainerName,
		input.ContactLabel,
		input.MessageCount,
		memories,
		autoMemoryRules,
		BuildRecencyTranscript(input.History),
	)
}

type AutoTagPromptInput struct {
	CampaignName   string
	MessageCount   int
	History        []*conversation.Message
	CurrentTagName string
	Tags           []*stage.Stage
}

func BuildAutoTagPrompt(input AutoTagPromptInput) string {

	var tagList strings.Builder
	for _, t := range input.Tags {
		marker := "  "
		if t.Name == input.CurrentTagName {
			marker = "→ "
		}
		desc := t.Description
		if desc == "" {
			desc = "(sem descrição)"
		}
		tagList.WriteString(fmt.Sprintf("%s• \"%s\", %s\n", marker, t.Name, desc))
	}

	currentTagSection := "nenhuma (lead ainda não classificado)"
	if input.CurrentTagName != "" {
		currentTagSection = fmt.Sprintf("\"%s\"", input.CurrentTagName)
	}

	return fmt.Sprintf(`Você é um classificador de leads altamente experiente. Sua ÚNICA tarefa é ler a transcrição abaixo e determinar em qual tag o lead deve estar AGORA.

═══════════════════════════════════════════════════
CONTEXTO
═══════════════════════════════════════════════════
- Campanha: %s
- Total de mensagens na conversa: %d
- Tag ATUAL do lead: %s

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
═══════════════════════════════════════════════════
%s`,
		input.CampaignName,
		input.MessageCount,
		currentTagSection,
		tagList.String(),
		latestPositionRule,
		BuildRecencyTranscript(input.History),
	)
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
)

type AutoDealPromptInput struct {
	ContainerName string
	ContactLabel  string
	MessageCount  int
	CurrentDeals  string
	History       []*conversation.Message
}

func BuildAutoDealPrompt(input AutoDealPromptInput) string {
	return fmt.Sprintf(`Você mantém as oportunidades de venda de uma conversa em um CRM. Sua ÚNICA tarefa é ler a transcrição abaixo e, usando a ferramenta auto_manage_opportunity, registrar o que mudou nas oportunidades desta conversa.

═══════════════════════════════════════════════════
CONTEXTO
═══════════════════════════════════════════════════
- Campanha/canal: %s
- Contato: %s
- Total de mensagens na conversa: %d

%s

═══════════════════════════════════════════════════
REGRAS
═══════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════
TRANSCRIÇÃO DA CONVERSA
═══════════════════════════════════════════════════
%s`,
		input.ContainerName,
		input.ContactLabel,
		input.MessageCount,
		input.CurrentDeals,
		autoDealRules,
		BuildRecencyTranscript(input.History),
	)
}
