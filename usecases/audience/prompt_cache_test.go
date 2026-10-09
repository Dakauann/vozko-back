package audience_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/ai"
	ca "vozko/domain/audience"
)

func openingOf(text string) string {
	opening, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return opening
}

func assertBefore(t *testing.T, prompt, first, then string) {
	t.Helper()
	i, j := strings.Index(prompt, first), strings.Index(prompt, then)
	if i < 0 || j < 0 {
		t.Fatalf("prompt lacks %q or %q", openingOf(first), openingOf(then))
	}
	if i > j {
		t.Fatalf("%q must come before %q", openingOf(first), openingOf(then))
	}
}

func TestTheCommentRubricComesBeforeThePerPostContext(t *testing.T) {
	topics := ca.DefaultTopicsFor(ca.VerticalGov)
	first := BuildSystemPrompt(topics, ca.ContainerContext{Caption: "Asfalto novo na Rua A"}, "Conta da prefeitura.")
	second := BuildSystemPrompt(topics, ca.ContainerContext{Caption: "Vacinação no posto central"}, "Conta da prefeitura.")

	assertBefore(t, first, ca.RubricPrompt(topics), "CONTEXTO DO OPERADOR")
	assertBefore(t, first, "FORMATO DA RESPOSTA", "CONTEXTO DO OPERADOR")
	assertBefore(t, first, "CONTEXTO DO OPERADOR", "PUBLICAÇÃO (legenda")

	shared, _, _ := strings.Cut(first, "CONTEXTO DO OPERADOR")
	if !strings.HasPrefix(second, shared) || !strings.Contains(shared, "Use apenas os valores listados") {
		t.Fatal("two posts of one account must share the rubric and the answer format as their prefix")
	}
}

func TestACommentPromptWithoutCaptionStillEndsWithThePostNotice(t *testing.T) {
	prompt := BuildSystemPrompt(ca.DefaultTopicsFor(ca.VerticalGov), ca.ContainerContext{}, "")
	assertBefore(t, prompt, "FORMATO DA RESPOSTA", "PUBLICAÇÃO: legenda indisponível")
}

func TestTheConversationRubricComesBeforeTheCampaignContext(t *testing.T) {
	campaign := ca.ContainerContext{Caption: "Agendar avaliação odontológica gratuita"}

	classify := BuildSystemPromptFor(ca.SubjectKindConversation, nil, campaign, "Clínica do centro.")
	assertBefore(t, classify, ca.ConversationSubjectPrompt(), "CONTEXTO DO OPERADOR")
	assertBefore(t, classify, "escolha o mais conservador", "CONTEXTO DO OPERADOR")
	assertBefore(t, classify, "CONTEXTO DO OPERADOR", "OBJETIVO DA CAMPANHA")

	summary := buildConversationSummaryPrompt(campaign, "Clínica do centro.")
	assertBefore(t, summary, ca.ConversationSummaryPrompt(), "CONTEXTO DO OPERADOR")
	assertBefore(t, summary, "FORMATO DA RESPOSTA", "OBJETIVO DA CAMPANHA")
}

func TestEverySingleCallFeatureIsScopedForCachingAndBilling(t *testing.T) {
	comments := make([]string, ca.MinCommentsForRole)
	for i := range comments {
		comments[i] = "comentário " + itoa(i)
	}
	calls := map[string]func(ai.Service) error{
		"audience_classify:ws-1": func(svc ai.Service) error {
			_, err := NewClassifier(svc, "m").Classify(context.Background(), sampleRequest(2))
			return err
		},
		"audience_author_role:ws-1": func(svc ai.Service) error {
			_, err := NewRoleInferrer(svc, "m").InferRole(context.Background(), ca.RoleInferRequest{WorkspaceID: "ws-1", Comments: comments})
			return err
		},
		"audience_alert_briefing:ws-1": func(svc ai.Service) error {
			_, err := NewAlertBriefer(svc, "m").Brief(context.Background(), ca.AlertBriefRequest{WorkspaceID: "ws-1", Comment: "que absurdo"})
			return err
		},
		"audience_reply_draft:ws-1": func(svc ai.Service) error {
			_, err := NewReplyDrafter(svc, "m").Draft(context.Background(), ca.ReplyDraftRequest{WorkspaceID: "ws-1", Comment: "quando abre?"})
			return err
		},
	}
	for want, call := range calls {
		svc := &fakeAI{}
		if err := call(svc); err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if len(svc.Inputs) != 1 {
			t.Fatalf("%s: calls = %d", want, len(svc.Inputs))
		}
		if in := svc.Inputs[0]; in.SessionID != want || in.BillingReference != want {
			t.Errorf("session %q, billing %q, want %q", in.SessionID, in.BillingReference, want)
		}
	}
}
