package copilot_usecase

import (
	"strings"
	"testing"

	"vozko/domain/readiness"
)

func TestWorkspacePromptStatesWhatIsMissingAndWhy(t *testing.T) {
	text := workspacePrompt(&readiness.Snapshot{SubscriptionActive: true, BalanceMicros: 2_500_000, Capabilities: []readiness.Status{
		{Capability: readiness.OfficialWhatsApp, Count: 0, Usage: &readiness.Usage{Used: 0, Total: 1}, CanAdd: true},
		{Capability: readiness.ApprovedTemplates, Blocker: readiness.BlockerNeedsOfficial},
		{Capability: readiness.UnofficialWhatsApp, Count: 2, Usage: &readiness.Usage{Used: 2, Total: 2}, Blocker: readiness.BlockerAtLimit},
		{Capability: readiness.Instagram, Blocker: readiness.BlockerUnavailable},
	}})
	for _, want := range []string{
		"Assinatura: ativa", "Saldo: US$ 2.50",
		"WhatsApp oficial: 0 (0 de 1 do plano); pode adicionar",
		"Modelos aprovados: 0; é preciso conectar um número do WhatsApp oficial antes",
		"WhatsApp não oficial: 2 (2 de 2 do plano); no limite do plano",
		"Instagram: estado indisponível agora",
		"offer_action",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestWorkspacePromptIsEmptyWithoutASnapshot(t *testing.T) {
	if workspacePrompt(nil) != "" {
		t.Fatal("no snapshot must add nothing")
	}
}
