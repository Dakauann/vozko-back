package comment_analysis_usecase

import (
	"encoding/json"
	"fmt"
	"strings"

	ca "vozko/domain/comment_analysis"
)

// The batch prompt. The rubric itself is rendered by the domain
// (ca.RubricPrompt), so the criteria are worded in exactly one place; this
// file only frames the task, adds the post's context and lays out the
// items. Refs, never text, come back (plan §2.3).

// BuildSystemPrompt is the part of the prompt that is the same for every
// batch of one container, which is what makes it cacheable at the provider
// and what the budgeter estimates once per container.
func BuildSystemPrompt(topics ca.TopicSet, ctx ca.ContainerContext, instructions string) string {
	var b strings.Builder
	b.WriteString("Você é um analista de audiência. Vai receber uma lista de comentários públicos feitos em UMA publicação de rede social e deve classificar CADA comentário, individualmente, seguindo a rubrica abaixo.\n\n")

	// Operator context: what the account is, what this post is about, what
	// to watch for. Quoted so it cannot read as an instruction to change the
	// rubric; the rubric below is the only authority on labels.
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

// batchItem is one comment as the model sees it.
type batchItem struct {
	Ref  int    `json:"ref"`
	Text string `json:"text"`
}

// BuildUserMessage lays the batch out as a JSON array so quoting, newlines
// and emoji inside comments cannot be read as prompt structure.
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
