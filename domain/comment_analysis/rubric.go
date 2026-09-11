package comment_analysis

import (
	"fmt"
	"sort"
	"strings"

	"vozko/domain/shared"
)

// This file is the SINGLE SOURCE OF TRUTH for the comment rubric, built on
// the machinery in domain/shared/rubric.go. The JSON response schema, the
// prompt fragment and the persisted enums all render from the tables here,
// so they cannot diverge. That is the guarantee the conversation rubric
// already makes, kept for the second rubric by construction rather than by
// care.
//
// Criteria are pt-BR, matching the existing rubric: it is the model's working
// language here. Stored values are English slugs; the UI translates them.

// JSON keys the model writes. Shared between the schema (BatchResponseSchema)
// and the decoder (BatchResult) so a rename cannot orphan one side.
const (
	SchemaKeyResults = "results"

	FieldRef       = "ref"
	FieldSentiment = "sentiment"
	FieldStance    = "stance"
	FieldIntent    = "intent"
	FieldTopicKey  = "topic_key"
	FieldIsSpam    = "is_spam"
	FieldLanguage  = "language"

	SeverityKeyToxicity       = "toxicity"
	SeverityKeyPersonalAttack = "personal_attack"
	SeverityKeyLegalRisk      = "legal_risk"

	// Conversation-subject keys (see conversation.go for the taxonomy). They
	// live in this block with the comment keys because it is the one place the
	// wire format is named: the schema and the decoder both read from here, so
	// a rename cannot orphan one side. FieldSentiment above is deliberately
	// shared, it means the same thing for both subjects.
	FieldInterest        = "interest"
	FieldProductInterest = "product_interest"
	FieldDisposition     = "disposition"
	FieldQualification   = "qualification"
	FieldNextAction      = "next_action"
	FieldSummary         = "summary"
)

// ClassificationFields is the taxonomy the model classifies each comment on.
// The topic field is not here: its enum is the container's TopicSet and is
// rendered per request (see BatchResponseSchema / RubricPrompt).
func ClassificationFields() []shared.ClassificationField {
	return []shared.ClassificationField{
		{
			Key:   FieldSentiment,
			Title: "Sentimento",
			Intro: "Tom emocional predominante do comentário:",
			Options: []shared.ClassificationOption{
				{Value: string(shared.SentimentPositive), Description: "elogio, entusiasmo, agradecimento, apoio explícito, humor amigável"},
				{Value: string(shared.SentimentNeutral), Description: "pergunta factual, informação, menção sem carga emocional, emoji ambíguo"},
				{Value: string(shared.SentimentNegative), Description: "reclamação, frustração, ironia hostil, ofensa, ameaça"},
			},
		},
		{
			Key:   FieldStance,
			Title: "Posicionamento",
			Intro: "Posição do autor em relação ao ASSUNTO DA PUBLICAÇÃO (pessoa, marca, produto ou pauta):",
			Options: []shared.ClassificationOption{
				{Value: string(StanceSupporter), Description: "apoia, defende, elogia ou se identifica com o assunto"},
				{Value: string(StanceNeutral), Description: "não se posiciona: pergunta, informa, comenta algo lateral, ou o comentário é só emoji/marcação"},
				{Value: string(StanceCritic), Description: "discorda ou critica o ASSUNTO (a pauta, o produto, a decisão) com argumento ou queixa, sem atacar a pessoa"},
				{Value: string(StanceHostile), Description: "ataca a PESSOA ou a marca em si: xingamento, deboche pessoal, acusação sem base, ameaça, desejo de mal"},
			},
		},
		{
			Key:   FieldIntent,
			Title: "Intenção",
			Intro: "O que o autor quer com o comentário (escolha a intenção DOMINANTE):",
			Options: []shared.ClassificationOption{
				{Value: string(IntentPraise), Description: "elogiar, agradecer, parabenizar, sem pedir nada"},
				{Value: string(IntentQuestion), Description: "obter uma informação (preço, horário, onde, como, quando)"},
				{Value: string(IntentComplaint), Description: "reclamar de um problema, serviço, produto ou decisão"},
				{Value: string(IntentSupportRequest), Description: "pedir ajuda com um caso próprio já em andamento (pedido, atendimento, protocolo)"},
				{Value: string(IntentSpam), Description: "propaganda, link suspeito, corrente, texto sem relação, bot"},
				{Value: string(IntentSalesLead), Description: "manifesta intenção de comprar ou contratar (\"quero\", \"me manda o link\", \"como faço para adquirir\")"},
				{Value: string(IntentOther), Description: "nenhuma das anteriores (marcação de amigo, piada, comentário lateral)"},
			},
		},
	}
}

// SeverityDimensions are the ordinal axes severity is composed from. The
// weights are the ONLY definition of the 0-100 severity.
func SeverityDimensions() []shared.QualityDimension {
	return []shared.QualityDimension{
		{
			Key: SeverityKeyToxicity, Weight: 0.45,
			Label:       "Toxicidade",
			Description: "Linguagem ofensiva, xingamento, vulgaridade, discurso de ódio, deboche cruel?",
		},
		{
			Key: SeverityKeyPersonalAttack, Weight: 0.35,
			Label:       "Ataque pessoal",
			Description: "Ataca a pessoa (caráter, aparência, família, honra) em vez do assunto?",
		},
		{
			Key: SeverityKeyLegalRisk, Weight: 0.20,
			Label:       "Risco jurídico",
			Description: "Ameaça, incitação à violência, calúnia/difamação (acusação de crime sem base), exposição de dados pessoais (doxxing)?",
		},
	}
}

// ---- Response schema ----

// BatchResponseSchema is the JSON Schema for one batch call, rendered from
// the rubric and the container's topic set. It is strict-mode shaped: every
// property required, nothing extra allowed, so an out-of-set label is
// refused by the provider before it reaches Validate.
//
// The model returns a batch-local ref per item, never the text (§2.3): the
// use case reconciles refs against what it sent.
func BatchResponseSchema(topics TopicSet) map[string]any {
	props := map[string]any{
		FieldRef: map[string]any{
			"type":        "integer",
			"description": "O número (ref) do comentário, exatamente como recebido.",
		},
		FieldTopicKey: map[string]any{
			"type":        "string",
			"description": "O tema da lista fornecida que melhor descreve o comentário; \"other\" se nenhum se encaixa.",
			"enum":        topics.Keys(),
		},
		FieldIsSpam: map[string]any{
			"type":        "boolean",
			"description": "true se o comentário é propaganda, link suspeito, corrente ou bot.",
		},
		FieldLanguage: map[string]any{
			"type":        "string",
			"description": "Idioma do comentário em BCP-47 (ex.: pt, es, en). Melhor esforço.",
		},
	}
	for _, f := range ClassificationFields() {
		props[f.Key] = map[string]any{
			"type":        "string",
			"description": f.Description(),
			"enum":        f.Values(),
		}
	}
	for _, d := range SeverityDimensions() {
		props[d.Key] = map[string]any{
			"type":        "string",
			"description": d.Description,
			"enum":        shared.QualityLevelValues(),
		}
	}
	// Sorted so the schema (and with it the provider's prompt-cache key) is
	// deterministic across calls.
	required := make([]string, 0, len(props))
	for key := range props {
		required = append(required, key)
	}
	sort.Strings(required)

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			SchemaKeyResults: map[string]any{
				"type":        "array",
				"description": "Uma entrada por comentário recebido, na ordem que preferir. Cada ref deve aparecer exatamente uma vez.",
				"items": map[string]any{
					"type":                 "object",
					"properties":           props,
					"required":             required,
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{SchemaKeyResults},
		"additionalProperties": false,
	}
}

// BatchResponse is the decoded shape of a batch call, mirroring
// BatchResponseSchema.
type BatchResponse struct {
	Results []BatchResult `json:"results"`
}

// BatchResult is one item's raw answer. Strings, not the typed enums, so a
// bad label decodes and is rejected by Validate with a reason rather than
// failing the whole batch at unmarshal.
type BatchResult struct {
	Ref            int    `json:"ref"`
	Sentiment      string `json:"sentiment"`
	Stance         string `json:"stance"`
	Intent         string `json:"intent"`
	TopicKey       string `json:"topic_key"`
	IsSpam         bool   `json:"is_spam"`
	Language       string `json:"language"`
	Toxicity       string `json:"toxicity"`
	PersonalAttack string `json:"personal_attack"`
	LegalRisk      string `json:"legal_risk"`

	// Conversation subjects fill these instead. One decoder for both kinds,
	// because the response schema already constrains which fields a given call
	// may return: the model is never offered both taxonomies at once.
	Interest        string `json:"interest"`
	ProductInterest string `json:"product_interest"`
	Disposition     string `json:"disposition"`
	Qualification   string `json:"qualification"`
	NextAction      string `json:"next_action"`
	Summary         string `json:"summary"`
	// The attendance-quality dimensions, rated ordinally. The 0-100 is computed
	// from them; the model is never asked for a number.
	GoalProgress       string `json:"goal_progress"`
	CustomerEngagement string `json:"customer_engagement"`
	AgentConduct       string `json:"agent_conduct"`
	Professionalism    string `json:"professionalism"`
}

// Classification converts the raw answer to the typed value. Not validated;
// call Validate with the container's topic set.
func (r BatchResult) Classification() Classification {
	return Classification{
		Sentiment:      shared.Sentiment(strings.TrimSpace(r.Sentiment)),
		Stance:         Stance(strings.TrimSpace(r.Stance)),
		Intent:         Intent(strings.TrimSpace(r.Intent)),
		TopicKey:       strings.TrimSpace(r.TopicKey),
		IsSpam:         r.IsSpam,
		Language:       strings.TrimSpace(r.Language),
		Toxicity:       shared.QualityLevel(strings.TrimSpace(r.Toxicity)),
		PersonalAttack: shared.QualityLevel(strings.TrimSpace(r.PersonalAttack)),
		LegalRisk:      shared.QualityLevel(strings.TrimSpace(r.LegalRisk)),
	}
}

// ---- Prompt ----

// RubricPrompt renders the classification criteria, the severity dimensions
// and the container's topics for the system prompt. The use case wraps it
// with the task framing and the post's caption; the rubric itself lives
// here so there is exactly one place a criterion is worded.
func RubricPrompt(topics TopicSet) string {
	var b strings.Builder
	b.WriteString("CLASSIFICAÇÃO, para cada comentário informe:\n\n")
	b.WriteString(shared.RenderClassificationRubric(ClassificationFields()))

	fmt.Fprintf(&b, "%d. %s, o tema da lista abaixo que melhor descreve o comentário (use exatamente a chave):\n", len(ClassificationFields())+1, FieldTopicKey)
	for _, t := range topics {
		if t.Description != "" {
			fmt.Fprintf(&b, "   - \"%s\" (%s): %s\n", t.Key, t.Label, t.Description)
		} else {
			fmt.Fprintf(&b, "   - \"%s\" (%s)\n", t.Key, t.Label)
		}
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%d. %s, true apenas para propaganda, link suspeito, corrente ou bot.\n\n", len(ClassificationFields())+2, FieldIsSpam)
	fmt.Fprintf(&b, "%d. %s, idioma em BCP-47 (pt, es, en…), melhor esforço.\n\n", len(ClassificationFields())+3, FieldLanguage)

	b.WriteString("GRAVIDADE, avalie cada dimensão abaixo com um nível ordinal. A gravidade final é CALCULADA AUTOMATICAMENTE a partir desses níveis e dos pesos, NÃO informe um número.\n\n")
	b.WriteString("Níveis: \"none\" (ausente) · \"low\" (leve) · \"medium\" (claro) · \"high\" (grave/explícito)\n\n")
	b.WriteString(shared.RenderQualityDimensions(SeverityDimensions()))
	b.WriteString("\nDiretrizes de calibração:\n")
	b.WriteString("- Crítica dura ao assunto sem ofensa à pessoa → stance \"critic\", personal_attack \"none\".\n")
	b.WriteString("- Xingamento ou deboche à pessoa → stance \"hostile\"; toxicity e personal_attack ao menos \"medium\".\n")
	b.WriteString("- Ameaça, incitação ou acusação de crime sem base → legal_risk \"high\", mesmo em tom calmo.\n")
	b.WriteString("- Ironia e sarcasmo contam pelo alvo, não pela forma.\n")
	b.WriteString("- Comentário só de emoji ou marcação: sentiment pelo emoji se for inequívoco, senão \"neutral\"; stance \"neutral\"; intent \"other\".\n")
	return b.String()
}
