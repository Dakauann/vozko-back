package analysis

import (
	"strings"

	"vozko/domain/shared"
)

// This file is the SINGLE SOURCE OF TRUTH for the analysis rubric: the set of
// valid classification values and the attendance-quality scoring model.
//
// Previously the taxonomy and the quality weights were restated in three
// places (the domain enums, the conversation_analysis tool schema, and two
// separate prompt strings) and had already diverged, e.g. one prompt scored
// quality on 35/25/25/15 weights while the tool schema and the other prompt
// used 40/30/20/10. Both the tool schema and the prompt builders now derive
// their values and scoring from the functions here, so they cannot drift.
//
// The generic machinery (field/option types, the renderers, ordinal levels and
// the weighted score) lives in domain/shared/rubric.go so a second rubric can
// build on it without a second copy. The aliases below keep every existing
// call site of this package compiling unchanged.

type (
	ClassificationField  = shared.ClassificationField
	ClassificationOption = shared.ClassificationOption
	QualityLevel         = shared.QualityLevel
	QualityDimension     = shared.QualityDimension
)

const (
	QualityLevelNone   = shared.QualityLevelNone
	QualityLevelLow    = shared.QualityLevelLow
	QualityLevelMedium = shared.QualityLevelMedium
	QualityLevelHigh   = shared.QualityLevelHigh
)

func QualityLevelValues() []string { return shared.QualityLevelValues() }

// ---- Classification fields (single source of truth: values + criteria) ----
//
// Descriptions are OBJECTIVE-RELATIVE: they refer to "the conversation's
// objective" instead of hardcoding sales semantics, so the same rubric works
// for any niche (sales, scheduling/dental, support, collections, …). The
// objective itself is supplied to the model through the agent instructions and
// campaign context in the prompt. Both the conversation_analysis tool schema
// and the analysis prompts render from here, the criteria are defined once.

func ClassificationFields() []ClassificationField {
	return []ClassificationField{
		{
			Key:   "interest",
			Title: "Interesse",
			Intro: "Nível de interesse do cliente NO OBJETIVO da conversa (baseado em AÇÕES concretas, não em mera educação):",
			Options: []ClassificationOption{
				{Value: string(InterestInterested), Description: "demonstra interesse claro COM AÇÕES relevantes ao objetivo, faz perguntas, pede detalhes/proposta/agendamento, fornece informações voluntariamente, confirma necessidade/fit, ou aceita um próximo passo coerente com o objetivo"},
				{Value: string(InterestNotInterested), Description: "recusa explicitamente, ignora repetidamente, pede para parar, ou demonstra desinteresse inequívoco no objetivo"},
				{Value: string(InterestUndecided), Description: "não se posicionou, respostas vagas, monossilábicas, apenas educado sem compromisso, ou exploratório sem intenção clara"},
			},
		},
		{
			Key:   "disposition",
			Title: "Disposição",
			Intro: "Resultado ATUAL da interação em relação ao OBJETIVO da campanha (seja rigoroso):",
			Options: []ClassificationOption{
				{Value: string(DispositionSale), Description: "o EVENTO DE CONVERSÃO do objetivo foi CONCLUÍDO e confirmado (ex.: venda fechada/paga, agendamento ou consulta confirmada, caso resolvido como ganho). Intenção (\"quero\", \"pode marcar\") NÃO basta, exige confirmação final"},
				{Value: string(DispositionFillingInfo), Description: "cliente fornecendo ATIVAMENTE os dados necessários para concluir a conversão (cadastro, pagamento, data/horário de agendamento, documentos). Já decidiu e está no processo"},
				{Value: string(DispositionCallback), Description: "cliente solicitou retorno em um momento específico (data/hora mencionada)"},
				{Value: string(DispositionDeclined), Description: "recusa EXPLÍCITA e definitiva do objetivo"},
				{Value: string(DispositionPending), Description: "TODOS os demais casos em andamento, fazendo perguntas, negociando, ou apenas aceitou receber mais informações, sem decisão final"},
				{Value: string(DispositionNoAnswer), Description: "apenas para chamadas de voz: não atendida, ou o usuário não falou nada"},
				{Value: string(DispositionVoicemail), Description: "apenas para chamadas de voz: caiu na caixa postal"},
			},
		},
		{
			Key:   "sentiment",
			Title: "Sentimento",
			Intro: "Tom emocional predominante do cliente:",
			Options: []ClassificationOption{
				{Value: string(SentimentPositive), Description: "engajado, amigável, fazendo perguntas com interesse, demonstrando entusiasmo"},
				{Value: string(SentimentNeutral), Description: "respostas factuais, sem emoção clara, tom profissional, OU o usuário não falou"},
				{Value: string(SentimentNegative), Description: "frustrado, impaciente, reclamando, ríspido, ameaçando cancelar, irritado"},
			},
		},
		{
			Key:   "qualification",
			Title: "Qualificação",
			Intro: "Probabilidade de o cliente ALCANÇAR O OBJETIVO (baseado em evidência, fit e próximo passo):",
			Options: []ClassificationOption{
				{Value: string(QualificationHotLead), Description: "1 sinal decisivo (pede o próximo passo concreto do objetivo, proposta/preço/condições/agendamento; quer avançar/fechar/marcar; fornece dados concretos para viabilizar; confirma necessidade atual + aceite imediato) OU 2+ sinais fortes (fit claro com o objetivo da campanha, contexto real do caso, respostas substantivas, aceite de continuidade, perguntas relevantes)"},
				{Value: string(QualificationWarmLead), Description: "há potencial/interesse real, mas ainda sem urgência, compromisso claro ou prova suficiente de avanço"},
				{Value: string(QualificationColdLead), Description: "baixo potencial no estado atual, receptividade passiva, monossilábico, sem fit/necessidade clara, ou apenas educação sem avanço"},
			},
		},
		{
			Key:   "next_action",
			Title: "Próxima Ação",
			Intro: "Próxima ação recomendada para avançar ao objetivo:",
			Options: []ClassificationOption{
				{Value: string(NextActionContinue), Description: "conversa progredindo bem, continuar (coletando informações, negociando, construindo rapport)"},
				{Value: string(NextActionScheduleCallback), Description: "cliente pediu ou precisa de retorno em outro momento"},
				{Value: string(NextActionSendWhatsApp), Description: "acompanhamento futuro via WhatsApp (enviar material, proposta, lembrete)"},
				{Value: string(NextActionClose), Description: "APENAS se o objetivo foi concluído OU o cliente recusou definitivamente"},
				{Value: string(NextActionEscalate), Description: "requer intervenção humana especializada (reclamação grave, dúvida técnica complexa, cliente insatisfeito)"},
			},
		},
	}
}

// ClassificationRubricPrompt renders the classification criteria for the
// analysis prompts (the same content the tool schema exposes per field).
func ClassificationRubricPrompt() string {
	return shared.RenderClassificationRubric(ClassificationFields())
}

// ---- Attendance-quality scoring ----
//
// The 0-100 score is NOT emitted by the model; see shared.WeightedScore for
// why. The model rates the four dimensions below on a coarse ordinal scale and
// the number is computed deterministically from these weights.

// Dimension keys, referenced by the tool schema, the parser and the assessment.
const (
	QualityKeyGoalProgress       = "goal_progress"
	QualityKeyCustomerEngagement = "customer_engagement"
	QualityKeyAgentConduct       = "agent_conduct"
	QualityKeyProfessionalism    = "professionalism"
)

func QualityDimensions() []QualityDimension {
	return []QualityDimension{
		{
			Key: QualityKeyGoalProgress, Weight: 0.40,
			Label:       "Resultado / avanço ao objetivo",
			Description: "A conversa avançou em direção ao objetivo (venda, qualificação, agendamento, suporte)? Houve progresso real e concreto?",
		},
		{
			Key: QualityKeyCustomerEngagement, Weight: 0.30,
			Label:       "Engajamento do cliente",
			Description: "O cliente interagiu de forma substantiva, fez perguntas, deu respostas detalhadas, demonstrou interesse real (não apenas educação)?",
		},
		{
			Key: QualityKeyAgentConduct, Weight: 0.20,
			Label:       "Condução do atendente",
			Description: "O atendente conduziu bem, fez perguntas de qualificação estratégicas, explorou oportunidades, contornou objeções e foi específico (não genérico)?",
		},
		{
			Key: QualityKeyProfessionalism, Weight: 0.10,
			Label:       "Profissionalismo e clareza",
			Description: "Tom adequado ao contexto, mensagens claras e corretas, sem erros graves de comunicação?",
		},
	}
}

// QualityAssessment holds the model's per-dimension ordinal ratings.
type QualityAssessment struct {
	GoalProgress       QualityLevel
	CustomerEngagement QualityLevel
	AgentConduct       QualityLevel
	Professionalism    QualityLevel
}

// NewQualityAssessment builds an assessment from a map keyed by the dimension
// keys, so callers (the tool) never hardcode the field mapping.
func NewQualityAssessment(levels map[string]QualityLevel) QualityAssessment {
	return QualityAssessment{
		GoalProgress:       levels[QualityKeyGoalProgress],
		CustomerEngagement: levels[QualityKeyCustomerEngagement],
		AgentConduct:       levels[QualityKeyAgentConduct],
		Professionalism:    levels[QualityKeyProfessionalism],
	}
}

func (a QualityAssessment) levelFor(key string) QualityLevel {
	switch key {
	case QualityKeyGoalProgress:
		return a.GoalProgress
	case QualityKeyCustomerEngagement:
		return a.CustomerEngagement
	case QualityKeyAgentConduct:
		return a.AgentConduct
	case QualityKeyProfessionalism:
		return a.Professionalism
	}
	return QualityLevelNone
}

// Score computes the 0-100 attendance quality from the per-dimension ordinal
// ratings and the dimension weights. Result is always within [0,100].
func (a QualityAssessment) Score() int {
	return shared.WeightedScore(QualityDimensions(), a.levelFor)
}

// QualityRubricPrompt renders the attendance-quality scoring instructions for
// the analysis prompts, so every prompt scores on the same weighted dimensions
// (instead of divergent inline copies).
func QualityRubricPrompt() string {
	var b strings.Builder
	b.WriteString("QUALIDADE DO ATENDIMENTO, avalie cada dimensão abaixo com um nível ordinal e registre-o na ferramenta conversation_analysis. A nota final de 0 a 100 é CALCULADA AUTOMATICAMENTE a partir desses níveis e dos pesos, NÃO informe um número diretamente.\n\n")
	b.WriteString("Níveis: \"none\" (ausente) · \"low\" (fraco) · \"medium\" (razoável) · \"high\" (forte/excelente)\n\n")
	b.WriteString(shared.RenderQualityDimensions(QualityDimensions()))
	b.WriteString("\nDiretrizes de calibração:\n")
	b.WriteString("- A avaliação é do ATENDENTE, não do cliente: um cliente difícil bem conduzido pode ter agent_conduct \"high\".\n")
	b.WriteString("- Conversa curta e monossilábica, sem perguntas do cliente → customer_engagement e goal_progress no máximo \"low\".\n")
	b.WriteString("- Atendente que não faz perguntas estratégicas ou responde de forma genérica (copy-paste) → agent_conduct no máximo \"low\".\n")
	b.WriteString("- Se a ligação caiu/foi transferida por motivo técnico (não por recusa do cliente), avalie apenas o trecho ocorrido, sem penalizar o atendente por isso.\n")
	return b.String()
}
