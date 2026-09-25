package copilot_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/readiness"
)

var capabilityLabels = map[readiness.Capability]string{
	readiness.OfficialWhatsApp:   "WhatsApp oficial",
	readiness.ApprovedTemplates:  "Modelos aprovados",
	readiness.UnofficialWhatsApp: "WhatsApp não oficial",
	readiness.Instagram:          "Instagram",
	readiness.Telegram:           "Telegram",
	readiness.KnowledgeBases:     "Bases de conhecimento",
}

var blockerLabels = map[readiness.Blocker]string{
	readiness.BlockerNoPermission:   "o usuário não tem permissão para adicionar",
	readiness.BlockerAtLimit:        "no limite do plano",
	readiness.BlockerNoSubscription: "precisa de assinatura ativa",
	readiness.BlockerNeedsOfficial:  "é preciso conectar um número do WhatsApp oficial antes",
}

func workspacePrompt(snap *readiness.Snapshot) string {
	if snap == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n# Estado do workspace (lido agora, antes desta resposta)\n")
	subscription := "ativa"
	if !snap.SubscriptionActive {
		subscription = "inativa (números, modelos e campanhas pagos não funcionam)"
	}
	fmt.Fprintf(&b, "- Assinatura: %s\n- Saldo: US$ %.2f\n", subscription, float64(snap.BalanceMicros)/1_000_000)
	for _, s := range snap.Capabilities {
		b.WriteString("- " + capabilityLine(s) + "\n")
	}
	b.WriteString("Use este estado antes de propor qualquer ação. Se o pedido depende de algo que falta, está no limite ou " +
		"o usuário não tem permissão, não proponha a ação: explique o motivo em uma frase e chame offer_action com o cartão " +
		"certo (conectar um número, recarregar o saldo, regularizar a assinatura). Nunca prometa o que o estado não permite.")
	return b.String()
}

func capabilityLine(s readiness.Status) string {
	label := capabilityLabels[s.Capability]
	if label == "" {
		label = string(s.Capability)
	}
	if s.Blocker == readiness.BlockerUnavailable {
		return label + ": estado indisponível agora"
	}
	line := fmt.Sprintf("%s: %d", label, s.Count)
	if s.Usage != nil {
		line += fmt.Sprintf(" (%d de %d do plano)", s.Usage.Used, s.Usage.Total)
	}
	switch {
	case s.CanAdd:
		return line + "; pode adicionar"
	case blockerLabels[s.Blocker] != "":
		return line + "; " + blockerLabels[s.Blocker]
	}
	return line
}
