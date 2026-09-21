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

	if instructions = strings.TrimSpace(instructions); instructions != "" {
		b.WriteString("CONTEXTO DO OPERADOR (sobre a conta e esta publicação; use para interpretar, não para mudar a rubrica):\n\"\"\"\n")
		b.WriteString(truncateRunes(instructions, ca.MaxInstructionsRunes))
		b.WriteString("\n\"\"\"\n\n")
	}

	if caption := strings.TrimSpace(ctx.Caption); caption != "" {
		b.WriteString("PUBLICAÇÃO (legenda, para contexto do que está sendo comentado):\n\"\"\"\n")
		b.WriteString(truncateRunes(caption, 1200))
		b.WriteString("\n\"\"\"\n\n")
	} else {
		b.WriteString("PUBLICAÇÃO: legenda indisponível; classifique pelo próprio comentário.\n\n")
	}

	b.WriteString(ca.RubricPrompt(topics))

	b.WriteString("\nFORMATO DA RESPOSTA:\n")
	b.WriteString("- Responda SOMENTE com o JSON pedido, sem texto antes ou depois.\n")
	b.WriteString("- Cada comentário recebido tem um número \"ref\". Devolva UMA entrada por ref, com o mesmo número, e nunca repita nem invente refs.\n")
	b.WriteString("- Não copie o texto do comentário na resposta.\n")
	b.WriteString("- Use apenas os valores listados; se estiver em dúvida entre dois, escolha o mais neutro.\n")
	return b.String()
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

func buildConversationSystemPrompt(ctx ca.ContainerContext, instructions string) string {
	var b strings.Builder
	b.WriteString("Você é um analista de atendimento. Vai receber uma lista de CONVERSAS entre uma empresa e seus clientes e deve classificar CADA conversa, individualmente, seguindo a rubrica abaixo.\n\n")

	if instructions = strings.TrimSpace(instructions); instructions != "" {
		b.WriteString("CONTEXTO DO OPERADOR (sobre a conta e esta campanha; use para interpretar, não para mudar a rubrica):\n\"\"\"\n")
		b.WriteString(truncateRunes(instructions, ca.MaxInstructionsRunes))
		b.WriteString("\n\"\"\"\n\n")
	}

	if caption := strings.TrimSpace(ctx.Caption); caption != "" {
		b.WriteString("OBJETIVO DA CAMPANHA (o que estas conversas tentam alcançar):\n\"\"\"\n")
		b.WriteString(truncateRunes(caption, 1200))
		b.WriteString("\n\"\"\"\n\n")
	} else {
		b.WriteString("OBJETIVO DA CAMPANHA: indisponível; infira o objetivo pela própria conversa e seja conservador ao julgar avanço.\n\n")
	}

	b.WriteString(ca.ConversationSubjectPrompt())

	b.WriteString("\nFORMATO DA RESPOSTA:\n")
	b.WriteString("- Responda SOMENTE com o JSON pedido, sem texto antes ou depois.\n")
	b.WriteString("- Cada conversa recebida tem um número \"ref\". Devolva UMA entrada por ref, com o mesmo número, e nunca repita nem invente refs.\n")
	b.WriteString("- Não copie as mensagens na resposta.\n")
	b.WriteString("- Use apenas os valores listados; se estiver em dúvida entre dois, escolha o mais conservador.\n")
	return b.String()
}
