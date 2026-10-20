package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestContactRef_CoversEveryKnownEntryType(t *testing.T) {
	for _, entryType := range shared.KnownEntryTypes() {
		if _, ok := contactRefs[entryType]; !ok {
			t.Errorf("%s is a known entry type but declares no contact slot; its conversations would render with no contact", entryType)
		}
	}
}

func TestContactRef_NoRegistryMissesASlot(t *testing.T) {
	if missing := entryTypesWithoutContactRef(); len(missing) != 0 {
		t.Fatalf("entry types without a contact slot: %v", missing)
	}
}

func TestContactRef_AnUnknownEntryTypeIsRefusedLoudly(t *testing.T) {
	for _, entryType := range []shared.EntryType{"carrier-pigeon", ""} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%q: an unknown entry type must never project NULL as its contact", entryType)
				}
				if !strings.Contains(r.(string), "contact slot") {
					t.Errorf("%q: panic = %v, want it to name the missing contact slot", entryType, r)
				}
			}()
			contactRefUUID(entryType)
		}()
	}
}
