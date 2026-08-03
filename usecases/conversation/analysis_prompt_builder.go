package conversation_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/analysis"
	"vozko/domain/conversation"
	"vozko/domain/lead"
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
	var transcript strings.Builder
	for _, msg := range input.History {
		if msg == nil {
			continue
		}

		switch msg.MessageType {
		case conversation.MessageTypeToolCall,
			conversation.MessageTypeToolResult,
			conversation.MessageTypeSystem:
			continue
		}
		if msg.MessageType.IsCallEvent() {
			continue
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		role := "Agent"
		if msg.From != "" && lead.NormalizeNumber(msg.From) == lead.NormalizeNumber(input.UserPhoneNumber) {
			role = "User"
		}
		transcript.WriteString(fmt.Sprintf("%s: %s\n", role, text))
	}

	agentInstructionsSection := ""
	if input.AgentInstructions != "" {
		instructions := input.AgentInstructions

		if len(instructions) > 4000 {
			instructions = instructions[:4000] + "..."
		}
		agentInstructionsSection = fmt.Sprintf("\nAgent Instructions (what the agent is supposed to do):\n%s\n", instructions)
	}

	if input.AnalysisType == AnalysisTypeCompleted {
		return buildCompletedCallPrompt(input.CampaignName, input.UserPhoneNumber, agentInstructionsSection, transcript.String())
	}
	return buildOngoingConversationPrompt(input.CampaignName, input.MessageCount, agentInstructionsSection, transcript.String())
}

func BuildTranscript(history []*conversation.Message, userPhoneNumber string) string {
	var transcript strings.Builder
	for _, msg := range history {
		if msg == nil {
			continue
		}
		switch msg.MessageType {
		case conversation.MessageTypeToolCall,
			conversation.MessageTypeToolResult,
			conversation.MessageTypeSystem:
			continue
		}
		if msg.MessageType.IsCallEvent() {
			continue
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		role := "Agent"
		if msg.From != "" && lead.NormalizeNumber(msg.From) == lead.NormalizeNumber(userPhoneNumber) {
			role = "User"
		}
		transcript.WriteString(fmt.Sprintf("%s: %s\n", role, text))
	}
	return transcript.String()
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
%s`, campaignName, messageCount, agentInstructions, analysis.ClassificationRubricPrompt(), analysis.QualityRubricPrompt(), transcript)
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
%s`, campaignName, userPhoneNumber, agentInstructions, analysis.ClassificationRubricPrompt(), analysis.QualityRubricPrompt(), transcript)
}

type AutoTagPromptInput struct {
	CampaignName   string
	MessageCount   int
	Transcript     string
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

	return fmt.Sprintf(`Você é um classificador de leads altamente experiente. Sua ÚNICA tarefa é ler a transcrição COMPLETA abaixo e determinar em qual tag o lead deve estar AGORA.

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

PASSO 1: Leia a transcrição INTEIRA do início ao fim.
PASSO 2: Identifique o ESTADO MAIS RECENTE da negociação, o que aconteceu nas ÚLTIMAS mensagens é o mais importante.
PASSO 3: Compare a descrição de CADA tag disponível com o estado atual da conversa.
PASSO 4: Escolha a tag cuja descrição MELHOR descreve a situação ATUAL do lead.

IMPORTANTE:
- Foque no que aconteceu mais recentemente na conversa, não apenas nas primeiras mensagens.
- Se a tag atual já descreve corretamente o estado do lead, MANTENHA ela (não chame a ferramenta ou passe a mesma tag).
- Se a tag atual NÃO corresponde mais ao estado real da conversa, MUDE para a tag correta.
- Leia as descrições das tags com atenção, a classificação deve bater com a descrição.

═══════════════════════════════════════════════════
TRANSCRIÇÃO COMPLETA DA CONVERSA
═══════════════════════════════════════════════════
%s`,
		input.CampaignName,
		input.MessageCount,
		currentTagSection,
		tagList.String(),
		input.Transcript,
	)
}
