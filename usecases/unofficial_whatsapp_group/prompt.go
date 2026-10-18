package unofficial_whatsapp_group_usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"vozko/domain/ai"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

var errUnreadableAnswer = errors.New("the model answer could not be read")

const analysisSystemPrompt = `Você é um analista de inteligência especializado em monitoramento de grupos de WhatsApp. Você analisa conversas fornecidas como transcrição e extrai inteligência estruturada.

A estrutura exata da resposta (campos, tipos e valores permitidos) é garantida pelo esquema JSON do canal de saída. Foque em produzir conteúdo de altíssima qualidade obedecendo às regras abaixo.

Regras de conteúdo:
- "summary": 2 a 4 frases descrevendo a conversa.
- "sentiment": sentimento geral da conversa (positive, negative, neutral ou mixed).
- "key_themes": principais assuntos discutidos.
- "relevant_mentions": pessoas, organizações, marcas ou assuntos citados, com sentimento e frequência (inteiro maior ou igual a 0).
- "participant_sentiments": inclua somente quem enviou pelo menos uma mensagem na transcrição abaixo, uma entrada por autor. A tabela de nomes serve apenas para escrever nomes e JIDs corretamente; nunca inclua quem não escreveu. Para cada participante:
    - "messages": quantas mensagens dele aparecem na transcrição (inteiro maior ou igual a 1).
    - "summary": uma frase sobre a postura dele, com base no que ele disse.
    - "key_quote": citação curta, de preferência literal, que mostre o sentimento atribuído. Deixe vazio só quando nenhuma fala servir.
  As falas de "Nossa equipe" são enviadas pelo próprio número de quem monitora o grupo: use como contexto e nunca inclua em participant_sentiments.
- "insights": observações úteis e acionáveis para quem acompanha o grupo.
- "suggested_actions": para cada situação relevante, uma estratégia concreta de resposta. De 1 a 5 itens quando houver situações que pedem ação; lista vazia quando não houver.
- "alert_level":
    - high: conteúdo urgente ou sensível (incitação, desinformação coordenada, ameaças, risco jurídico).
    - medium: tendências relevantes que merecem atenção.
    - low: conversa rotineira.
- "custom_fields": preencha conforme as instruções de campos acompanhados na mensagem do usuário; use null quando não for possível determinar o valor pelas mensagens.

Escreva todos os textos em português do Brasil, com foco especial no tema indicado pelo usuário. Seja conciso e factual.`

type promptAuthor struct {
	name string
	jid  string
}

func buildAnalysisMessage(a *uwg.Analysis, groupSubject string, lines []uwg.TranscriptLine, names map[string]string, loc *time.Location) string {
	authors := map[string]promptAuthor{}
	var transcript strings.Builder
	for _, line := range lines {
		author := authorOf(line, names)
		authors[author.jid+"|"+author.name] = author
		fmt.Fprintf(&transcript, "[%s] %s: %s\n", line.At.In(loc).Format("2006-01-02 15:04"), author.name, line.Text)
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "Análise: %s\nGrupo: %s\nFoco/tema: %s\n\n", a.Name, groupSubject, a.Prompt)
	msg.WriteString(trackedFieldsInstruction(a.TrackedFields))
	msg.WriteString(rosterTable(authors))
	msg.WriteString("Mensagens do grupo (da mais antiga para a mais recente):\n")
	msg.WriteString(transcript.String())
	return msg.String()
}

func authorOf(line uwg.TranscriptLine, names map[string]string) promptAuthor {
	if line.Outbound {
		return promptAuthor{name: uwg.OurTeamAuthor}
	}
	handle := strings.TrimSpace(line.Author)
	jid := handle
	if phone := uw.NormalizePhone(handle); phone != "" && strings.HasPrefix(handle, "+") {
		jid = phone + "@" + uw.DomainUser
	}
	name := names[uwg.PersonKey(uw.PhoneFromJID(jid), jid)]
	if name == "" {
		name = handle
	}
	return promptAuthor{name: name, jid: jid}
}

func rosterTable(authors map[string]promptAuthor) string {
	if len(authors) == 0 {
		return ""
	}
	keys := make([]string, 0, len(authors))
	for k := range authors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("Tabela de nomes (use apenas para escrever nomes e JIDs corretamente; inclua em participant_sentiments só quem escreveu na transcrição):\n")
	for _, k := range keys {
		author := authors[k]
		if author.jid != "" {
			fmt.Fprintf(&b, "  - %s (jid: %s)\n", author.name, author.jid)
			continue
		}
		fmt.Fprintf(&b, "  - %s\n", author.name)
	}
	b.WriteString("\n")
	return b.String()
}

func trackedFieldsInstruction(fields []uwg.TrackedField) string {
	if len(fields) == 0 {
		return "Campos acompanhados: nenhum. Retorne \"custom_fields\": {}.\n\n"
	}
	var b strings.Builder
	b.WriteString("Campos acompanhados (preencha o objeto \"custom_fields\" com estes valores):\n")
	for _, f := range fields {
		switch f.Type {
		case uwg.FieldPercentage:
			fmt.Fprintf(&b, "  - \"%s\" (porcentagem): número de 0 a 100, por exemplo 72.5\n", f.Key)
		case uwg.FieldNumber:
			fmt.Fprintf(&b, "  - \"%s\" (número): inteiro ou decimal, por exemplo 42\n", f.Key)
		case uwg.FieldBoolean:
			fmt.Fprintf(&b, "  - \"%s\" (sim ou não): true ou false\n", f.Key)
		default:
			fmt.Fprintf(&b, "  - \"%s\" (texto): texto livre curto\n", f.Key)
		}
	}
	b.WriteString("Quando não for possível determinar um valor pelas mensagens, use null.\n\n")
	return b.String()
}

type wireMention struct {
	Entity    string `json:"entity"`
	Sentiment string `json:"sentiment"`
	Frequency int    `json:"frequency"`
	Context   string `json:"context"`
}

type wireParticipant struct {
	Name      string `json:"name"`
	JID       string `json:"jid"`
	Sentiment string `json:"sentiment"`
	Messages  int    `json:"messages"`
	Summary   string `json:"summary"`
	KeyQuote  string `json:"key_quote"`
}

type wireAction struct {
	Situation string `json:"situation"`
	Strategy  string `json:"strategy"`
}

type wireResult struct {
	Summary          string            `json:"summary"`
	Sentiment        string            `json:"sentiment"`
	KeyThemes        []string          `json:"key_themes"`
	Mentions         []wireMention     `json:"relevant_mentions"`
	Participants     []wireParticipant `json:"participant_sentiments"`
	Insights         []string          `json:"insights"`
	SuggestedActions []wireAction      `json:"suggested_actions"`
	AlertLevel       string            `json:"alert_level"`
	CustomFields     map[string]any    `json:"custom_fields"`
}

func parseAnalysis(content string, fields []uwg.TrackedField) (*uwg.Result, error) {
	body := ai.UnfenceJSON(content)
	if body == "" {
		return nil, errUnreadableAnswer
	}
	var wire wireResult
	if err := json.Unmarshal([]byte(body), &wire); err != nil {
		return nil, fmt.Errorf("%w: %v", errUnreadableAnswer, err)
	}
	result := &uwg.Result{
		Summary: wire.Summary, Sentiment: uwg.RunSentiment(wire.Sentiment), KeyThemes: wire.KeyThemes,
		Insights: wire.Insights, AlertLevel: uwg.AlertLevel(wire.AlertLevel), CustomFields: wire.CustomFields,
	}
	for _, m := range wire.Mentions {
		result.Mentions = append(result.Mentions, uwg.Mention{Entity: m.Entity, Sentiment: shared.Sentiment(m.Sentiment), Frequency: m.Frequency, Context: m.Context})
	}
	for _, p := range wire.Participants {
		result.Participants = append(result.Participants, uwg.ParticipantReading{
			Name: p.Name, JID: p.JID, Sentiment: uwg.RunSentiment(p.Sentiment), Messages: p.Messages, Summary: p.Summary, KeyQuote: p.KeyQuote,
		})
	}
	for _, s := range wire.SuggestedActions {
		result.SuggestedActions = append(result.SuggestedActions, uwg.SuggestedAction{Situation: s.Situation, Strategy: s.Strategy})
	}
	result.Normalize(fields)
	return result, nil
}
