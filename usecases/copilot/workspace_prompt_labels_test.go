package copilot_usecase

import (
	"testing"

	"vozko/domain/copilot"
)

func TestEveryOfferedCapabilityHasAPromptLabel(t *testing.T) {
	for _, kind := range copilot.ActionKinds() {
		capability, ok := kind.Capability()
		if !ok {
			continue
		}
		if capabilityLabels[capability] == "" {
			t.Errorf("%s offers %s but the workspace prompt has no label for it", kind, capability)
		}
	}
}
