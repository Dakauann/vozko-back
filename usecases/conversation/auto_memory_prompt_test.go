package conversation_usecase

import (
	"strings"
	"testing"
)

func TestBuildAutoMemoryPromptRendersWithoutFormatErrors(t *testing.T) {
	prompt := BuildAutoMemoryPrompt(AutoMemoryPromptInput{
		ContainerName:   "Campanha X",
		ContactLabel:    "+55 11 99999-0000",
		MessageCount:    12,
		CurrentMemories: "\n# Memórias sobre este lead\n- [abc12345 · 2026-08-01] Prefere boleto\n",
		Transcript:      "User: quero pagar no boleto\nAgent: claro\n",
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
		Transcript:    "User: oi\n",
	})
	// An empty block must be said out loud: a model told nothing about the
	// current memories would have no basis for choosing update over remember.
	if !strings.Contains(prompt, "nenhuma memória salva") {
		t.Fatalf("prompt does not announce the empty memory state:\n%s", prompt)
	}
}

func TestBuildAutoMemorySection(t *testing.T) {
	section := BuildAutoMemorySection("\n# Memórias sobre este lead\n- [abc12345 · 2026-08-01] Prefere boleto\n")
	if strings.Contains(section, "%!") {
		t.Fatalf("section has fmt errors: %s", section)
	}
	if !strings.Contains(section, "manage_lead_memory") || !strings.Contains(section, "Prefere boleto") {
		t.Fatalf("section missing tool guidance or the injected block:\n%s", section)
	}

	empty := BuildAutoMemorySection("")
	if !strings.Contains(empty, "nenhuma memória salva") {
		t.Fatalf("empty-memory section does not announce the empty state:\n%s", empty)
	}
}
