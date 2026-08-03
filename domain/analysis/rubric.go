package analysis

import (
	"fmt"
	"math"
	"strings"
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

// ---- Classification fields (single source of truth: values + criteria) ----
//
// Descriptions are OBJECTIVE-RELATIVE: they refer to "the conversation's
// objective" instead of hardcoding sales semantics, so the same rubric works
// for any niche (sales, scheduling/dental, support, collections, …). The
// objective itself is supplied to the model through the agent instructions and
// campaign context in the prompt. Both the conversation_analysis tool schema
// and the analysis prompts render from here, the criteria are defined once.

type ClassificationOption struct {
	Value       string
	Description string
}

type ClassificationField struct {
	Key     string
	Title   string
	Intro   string
	Options []ClassificationOption
}

// Values returns the allowed enum values (for the tool schema's Enum field).
func (f ClassificationField) Values() []string {
	out := make([]string, len(f.Options))
	for i, o := range f.Options {
		out[i] = o.Value
	}
	return out
}

// Description renders the field's criteria for a tool parameter description.
func (f ClassificationField) Description() string {
	var b strings.Builder
	b.WriteString(f.Intro)
	for _, o := range f.Options {
		fmt.Fprintf(&b, "\n- \"%s\": %s", o.Value, o.Description)
	}
	return b.String()
}

func ClassificationFields() []ClassificationField {
	return []ClassificationField{
		{
			Key:   "interest",
			Title: "Interesse",
			Intro: "Nível de interesse do cliente NO OBJETIVO da conversa (baseado em AÇÕES concretas, não em mera educação):",
			Options: []ClassificationOption{
				{string(InterestInterested), "demonstra interesse claro COM AÇÕES relevantes ao objetivo, faz perguntas, pede detalhes/proposta/agendamento, fornece informações voluntariamente, confirma necessidade/fit, ou aceita um próximo passo coerente com o objetivo"},
				{string(InterestNotInterested), "recusa explicitamente, ignora repetidamente, pede para parar, ou demonstra desinteresse inequívoco no objetivo"},
				{string(InterestUndecided), "não se posicionou, respostas vagas, monossilábicas, apenas educado sem compromisso, ou exploratório sem intenção clara"},
			},
		},
		{
			Key:   "disposition",
			Title: "Disposição",
			Intro: "Resultado ATUAL da interação em relação ao OBJETIVO da campanha (seja rigoroso):",
			Options: []ClassificationOption{
				{string(DispositionSale), "o EVENTO DE CONVERSÃO do objetivo foi CONCLUÍDO e confirmado (ex.: venda fechada/paga, agendamento ou consulta confirmada, caso resolvido como ganho). Intenção (\"quero\", \"pode marcar\") NÃO basta, exige confirmação final"},
				{string(DispositionFillingInfo), "cliente fornecendo ATIVAMENTE os dados necessários para concluir a conversão (cadastro, pagamento, data/horário de agendamento, documentos). Já decidiu e está no processo"},
				{string(DispositionCallback), "cliente solicitou retorno em um momento específico (data/hora mencionada)"},
				{string(DispositionDeclined), "recusa EXPLÍCITA e definitiva do objetivo"},
				{string(DispositionPending), "TODOS os demais casos em andamento, fazendo perguntas, negociando, ou apenas aceitou receber mais informações, sem decisão final"},
				{string(DispositionNoAnswer), "apenas para chamadas de voz: não atendida, ou o usuário não falou nada"},
				{string(DispositionVoicemail), "apenas para chamadas de voz: caiu na caixa postal"},
			},
		},
		{
			Key:   "sentiment",
			Title: "Sentimento",
			Intro: "Tom emocional predominante do cliente:",
			Options: []ClassificationOption{
				{string(SentimentPositive), "engajado, amigável, fazendo perguntas com interesse, demonstrando entusiasmo"},
				{string(SentimentNeutral), "respostas factuais, sem emoção clara, tom profissional, OU o usuário não falou"},
				{string(SentimentNegative), "frustrado, impaciente, reclamando, ríspido, ameaçando cancelar, irritado"},
			},
		},
		{
			Key:   "qualification",
			Title: "Qualificação",
			Intro: "Probabilidade de o cliente ALCANÇAR O OBJETIVO (baseado em evidência, fit e próximo passo):",
			Options: []ClassificationOption{
				{string(QualificationHotLead), "1 sinal decisivo (pede o próximo passo concreto do objetivo, proposta/preço/condições/agendamento; quer avançar/fechar/marcar; fornece dados concretos para viabilizar; confirma necessidade atual + aceite imediato) OU 2+ sinais fortes (fit claro com o objetivo da campanha, contexto real do caso, respostas substantivas, aceite de continuidade, perguntas relevantes)"},
				{string(QualificationWarmLead), "há potencial/interesse real, mas ainda sem urgência, compromisso claro ou prova suficiente de avanço"},
				{string(QualificationColdLead), "baixo potencial no estado atual, receptividade passiva, monossilábico, sem fit/necessidade clara, ou apenas educação sem avanço"},
			},
		},
		{
			Key:   "next_action",
			Title: "Próxima Ação",
			Intro: "Próxima ação recomendada para avançar ao objetivo:",
			Options: []ClassificationOption{
				{string(NextActionContinue), "conversa progredindo bem, continuar (coletando informações, negociando, construindo rapport)"},
				{string(NextActionScheduleCallback), "cliente pediu ou precisa de retorno em outro momento"},
				{string(NextActionSendWhatsApp), "acompanhamento futuro via WhatsApp (enviar material, proposta, lembrete)"},
				{string(NextActionClose), "APENAS se o objetivo foi concluído OU o cliente recusou definitivamente"},
				{string(NextActionEscalate), "requer intervenção humana especializada (reclamação grave, dúvida técnica complexa, cliente insatisfeito)"},
			},
		},
	}
}

// ClassificationRubricPrompt renders the classification criteria for the
// analysis prompts (the same content the tool schema exposes per field).
func ClassificationRubricPrompt() string {
	var b strings.Builder
	for i, f := range ClassificationFields() {
		fmt.Fprintf(&b, "%d. %s, %s\n", i+1, f.Key, f.Intro)
		for _, o := range f.Options {
			fmt.Fprintf(&b, "   - \"%s\": %s\n", o.Value, o.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---- Attendance-quality scoring ----
//
// The 0-100 score is NOT emitted by the model. Asking an LLM for an arbitrary
// 0-100 number is poorly calibrated (the same call scores differently run to
// run). Instead the model rates a few weighted dimensions on a coarse ordinal
// scale (which LLMs do far more consistently) and the numeric score is computed
// here, deterministically.

// QualityLevel is the ordinal rating the model assigns to each dimension.
type QualityLevel string

const (
	QualityLevelNone   QualityLevel = "none"
	QualityLevelLow    QualityLevel = "low"
	QualityLevelMedium QualityLevel = "medium"
	QualityLevelHigh   QualityLevel = "high"
)

func (l QualityLevel) Valid() bool {
	switch l {
	case QualityLevelNone, QualityLevelLow, QualityLevelMedium, QualityLevelHigh:
		return true
	}
	return false
}

// fraction maps an ordinal level onto [0,1].
func (l QualityLevel) fraction() float64 {
	switch l {
	case QualityLevelHigh:
		return 1.0
	case QualityLevelMedium:
		return 0.66
	case QualityLevelLow:
		return 0.33
	default: // none or invalid
		return 0.0
	}
}

func QualityLevelValues() []string {
	return []string{
		string(QualityLevelNone), string(QualityLevelLow),
		string(QualityLevelMedium), string(QualityLevelHigh),
	}
}

// Dimension keys, referenced by the tool schema, the parser and the assessment.
const (
	QualityKeyGoalProgress       = "goal_progress"
	QualityKeyCustomerEngagement = "customer_engagement"
	QualityKeyAgentConduct       = "agent_conduct"
	QualityKeyProfessionalism    = "professionalism"
)

// QualityDimension is one weighted axis of attendance quality. The weights are
// the ONLY definition of how the 0-100 score is composed.
type QualityDimension struct {
	Key         string
	Weight      float64
	Label       string
	Description string
}

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
	var total float64
	for _, d := range QualityDimensions() {
		total += d.Weight * a.levelFor(d.Key).fraction()
	}
	return int(math.Round(total * 100))
}

// QualityRubricPrompt renders the attendance-quality scoring instructions for
// the analysis prompts, so every prompt scores on the same weighted dimensions
// (instead of divergent inline copies).
func QualityRubricPrompt() string {
	var b strings.Builder
	b.WriteString("QUALIDADE DO ATENDIMENTO, avalie cada dimensão abaixo com um nível ordinal e registre-o na ferramenta conversation_analysis. A nota final de 0 a 100 é CALCULADA AUTOMATICAMENTE a partir desses níveis e dos pesos, NÃO informe um número diretamente.\n\n")
	b.WriteString("Níveis: \"none\" (ausente) · \"low\" (fraco) · \"medium\" (razoável) · \"high\" (forte/excelente)\n\n")
	for _, d := range QualityDimensions() {
		fmt.Fprintf(&b, "- %s (%s, peso %.0f%%): %s\n", d.Key, d.Label, d.Weight*100, d.Description)
	}
	b.WriteString("\nDiretrizes de calibração:\n")
	b.WriteString("- A avaliação é do ATENDENTE, não do cliente: um cliente difícil bem conduzido pode ter agent_conduct \"high\".\n")
	b.WriteString("- Conversa curta e monossilábica, sem perguntas do cliente → customer_engagement e goal_progress no máximo \"low\".\n")
	b.WriteString("- Atendente que não faz perguntas estratégicas ou responde de forma genérica (copy-paste) → agent_conduct no máximo \"low\".\n")
	b.WriteString("- Se a ligação caiu/foi transferida por motivo técnico (não por recusa do cliente), avalie apenas o trecho ocorrido, sem penalizar o atendente por isso.\n")
	return b.String()
}
