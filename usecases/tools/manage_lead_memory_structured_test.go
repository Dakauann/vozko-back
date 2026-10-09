package tools_usecase

import (
	"strings"
	"testing"
)

func TestTheMemoryToolSendsBirthdaysAndRelativesToTheRecordNotToMemory(t *testing.T) {
	description := newMemTool(&memToolFakes{}).Definition().Description
	for _, want := range []string{UpdateLeadProfileToolName, "data de nascimento", "parentes", "Família"} {
		if !strings.Contains(description, want) {
			t.Errorf("the memory tool description misses %q:\n%s", want, description)
		}
	}
	for _, stale := range []string{"data importante", "nome de familiar"} {
		if strings.Contains(description, stale) {
			t.Errorf("the memory tool still asks to remember %q as free text", stale)
		}
	}
}
