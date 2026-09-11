package audience_usecase

import (
	"encoding/json"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
)

// The batch prompt. The rubric itself is rendered by the domain
// (ca.RubricPrompt), so the criteria are worded in exactly one place; this
// file only frames the task, adds the post's context and lays out the
// items. Refs, never text, come back (plan §2.3).

// BuildSystemPrompt builds the comment prompt. Kept for the call sites and
// tests that only ever mean comments; BuildSystemPromptFor is the kind-aware
// entry point the engine uses.
func BuildSystemPrompt(topics ca.TopicSet, ctx ca.ContainerContext, instructions string) string {
	return BuildSystemPromptFor(ca.SubjectKindComment, topics, ctx, instructions)
}

// BuildSystemPromptFor frames the task for the subject kind. The rubric itself
// is rendered by the domain, so the criteria are worded in exactly one place.
func BuildSystemPromptFor(kind ca.SubjectKind, topics ca.TopicSet, ctx ca.ContainerContext, instructions string) string {
	if kind == ca.SubjectKindConversation {
		return buildConversationSystemPrompt(ctx, instructions)
	}
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

// buildConversationSystemPrompt frames a batch of conversations.
//
// The shape mirrors the comment prompt deliberately: operator context first,
// then what the subjects are FOR, then the rubric. The middle part is what
// differs in kind rather than in wording. A comment is judged against a post; a
// conversation is judged against an OBJECTIVE, and every criterion in the
// conversation rubric is written relative to it. Without the campaign's
// objective the model is left inferring what "progress" means, which is how a
// scheduling conversation ends up scored as a failed sale.
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
