package conversation_usecase

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
)

func TestBuildAutoMemoryPromptRendersWithoutFormatErrors(t *testing.T) {
	prompt := BuildAutoMemoryPrompt(AutoMemoryPromptInput{
		ContainerName:   "Campanha X",
		ContactLabel:    "+55 11 99999-0000",
		MessageCount:    12,
		CurrentMemories: "\n# Memórias sobre este lead\n- [abc12345 · 2026-08-01] Prefere boleto\n",
		History:         []*conversation.Message{customerSays("quero pagar no boleto"), teamSays("claro")},
	})

	if strings.Contains(prompt, "%!") {
		t.Fatalf("prompt has fmt errors: %s", prompt)
	}
	for _, want := range []string{
		"manage_lead_memory",
		"Campanha X",
		"Prefere boleto",
		"quero pagar no boleto",
		"NÃO salve trivialidades",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestBuildAutoMemoryPromptAnnouncesEmptyMemory(t *testing.T) {
	prompt := BuildAutoMemoryPrompt(AutoMemoryPromptInput{
		ContainerName: "Campanha X",
		ContactLabel:  "+55 11 99999-0000",
		MessageCount:  3,
		History:       []*conversation.Message{customerSays("oi")},
	})
	if !strings.Contains(prompt, "nenhuma memória salva") {
		t.Fatalf("prompt does not announce the empty memory state:\n%s", prompt)
	}
}
