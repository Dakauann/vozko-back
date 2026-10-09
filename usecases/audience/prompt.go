package audience_usecase

import (
	"encoding/json"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
)

func BuildSystemPrompt(topics ca.TopicSet, ctx ca.ContainerContext, instructions string) string {
	return BuildSystemPromptFor(ca.SubjectKindComment, topics, ctx, instructions)
}

func BuildSystemPromptFor(kind ca.SubjectKind, topics ca.TopicSet, ctx ca.ContainerContext, instructions string) string {
	if kind == ca.SubjectKindConversation {
		return buildConversationSystemPrompt(ctx, instructions)
	}
	var b strings.Builder
	b.WriteString("Você é um analista de audiência. Vai receber uma lista de comentários públicos feitos em UMA publicação de rede social e deve classificar CADA comentário, individualmente, seguindo a rubrica abaixo.\n\n")

	b.WriteString(ca.RubricPrompt(topics))

	b.WriteString("\nFORMATO DA RESPOSTA:\n")
	b.WriteString("- Responda SOMENTE com o JSON pedido, sem texto antes ou depois.\n")
	b.WriteString("- Cada comentário recebido tem um número \"ref\". Devolva UMA entrada por ref, com o mesmo número, e nunca repita nem invente refs.\n")
	b.WriteString("- Não copie o texto do comentário na resposta.\n")
	b.WriteString("- Use apenas os valores listados; se estiver em dúvida entre dois, escolha o mais neutro.\n")

	writePostContext(&b, ctx, instructions)
	return b.String()
}

func writePostContext(b *strings.Builder, ctx ca.ContainerContext, instructions string) {
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		writeQuotedBlock(b, "CONTEXTO DO OPERADOR (sobre a conta e esta publicação; use para interpretar, não para mudar a rubrica):", truncateRunes(instructions, ca.MaxInstructionsRunes))
	}

	if caption := strings.TrimSpace(ctx.Caption); caption != "" {
		writeQuotedBlock(b, "PUBLICAÇÃO (legenda, para contexto do que está sendo comentado):", truncateRunes(caption, 1200))
	} else {
		b.WriteString("\nPUBLICAÇÃO: legenda indisponível; classifique pelo próprio comentário.\n")
	}
}

func writeQuotedBlock(b *strings.Builder, heading, body string) {
	b.WriteString("\n" + heading + "\n\"\"\"\n")
	b.WriteString(body)
	b.WriteString("\n\"\"\"\n")
}

type batchItem struct {
	Ref  int    `json:"ref"`
	Text string `json:"text"`
}

func BuildUserMessage(plan ca.BatchPlan) (string, error) {
	items := make([]batchItem, len(plan.Items))
	for i, it := range plan.Items {
		items[i] = batchItem{Ref: it.Ref, Text: it.Text}
	}
	body, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Comentários (%d):\n%s", len(items), body), nil
}

func truncateRunes(s string, max int) string {
	out, _ := ca.TruncateRunes(s, max)
	return out
}

func buildSystemPromptOf(req ca.ClassifyRequest) string {
	if req.SummaryOnly {
		return buildConversationSummaryPrompt(req.Context, req.Instructions)
	}
	return BuildSystemPromptFor(req.SubjectKind, req.Topics, req.Context, req.Instructions)
}

func buildConversationSummaryPrompt(ctx ca.ContainerContext, instructions string) string {
	var b strings.Builder
	b.WriteString("Você é um analista de atendimento. Vai receber uma lista de CONVERSAS entre uma empresa e seus clientes e deve resumir CADA conversa, individualmente.\n\n")
	b.WriteString(ca.ConversationSummaryPrompt())
	writeConversationFormat(&b)
	writeCampaignContext(&b, ctx, instructions)
	return b.String()
}

func buildConversationSystemPrompt(ctx ca.ContainerContext, instructions string) string {
	var b strings.Builder
	b.WriteString("Você é um analista de atendimento. Vai receber uma lista de CONVERSAS entre uma empresa e seus clientes e deve classificar CADA conversa, individualmente, seguindo a rubrica abaixo.\n\n")
	b.WriteString(ca.ConversationSubjectPrompt())
	writeConversationFormat(&b)
	b.WriteString("- Use apenas os valores listados; se estiver em dúvida entre dois, escolha o mais conservador.\n")
	writeCampaignContext(&b, ctx, instructions)
	return b.String()
}

func writeCampaignContext(b *strings.Builder, ctx ca.ContainerContext, instructions string) {
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		writeQuotedBlock(b, "CONTEXTO DO OPERADOR (sobre a conta e esta campanha; use para interpretar, não para mudar a rubrica):", truncateRunes(instructions, ca.MaxInstructionsRunes))
	}

	if caption := strings.TrimSpace(ctx.Caption); caption != "" {
		writeQuotedBlock(b, "OBJETIVO DA CAMPANHA (o que estas conversas tentam alcançar):", truncateRunes(caption, 1200))
	} else {
		b.WriteString("\nOBJETIVO DA CAMPANHA: indisponível; infira o objetivo pela própria conversa e seja conservador ao julgar avanço.\n")
	}
}

func writeConversationFormat(b *strings.Builder) {
	b.WriteString("\nFORMATO DA RESPOSTA:\n")
	b.WriteString("- Responda SOMENTE com o JSON pedido, sem texto antes ou depois.\n")
	b.WriteString("- Cada conversa recebida tem um número \"ref\". Devolva UMA entrada por ref, com o mesmo número, e nunca repita nem invente refs.\n")
	b.WriteString("- Não copie as mensagens na resposta.\n")
}
