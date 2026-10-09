package calllist

import (
	"testing"

	"vozko/domain/workspace"
)

func TestTheCallListCapabilitiesAreInTheWorkspaceCatalog(t *testing.T) {
	for _, key := range []workspace.CapabilityKey{CapabilityView, CapabilityWork, CapabilityManage} {
		if _, err := workspace.CapabilitiesRequire([]workspace.CapabilityKey{key}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
}
