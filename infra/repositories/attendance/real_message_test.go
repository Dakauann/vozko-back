package attendance_repository

import (
	"strings"
	"testing"
)

func TestRealMessageSQL_ExcludesOnlyTheLeadImportSeed(t *testing.T) {
	got := realMessageSQL("cm")

	for _, want := range []string{
		"cm.message_type = 'system'",
		"cm.metadata->>'seed'",
		"'lead_import'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("predicate missing %q: %s", want, got)
		}
	}
}

func TestRealMessageSQL_CoalescesNullMetadata(t *testing.T) {
	got := realMessageSQL("cm")

	if !strings.Contains(got, "COALESCE(cm.metadata->>'seed', '')") {
		t.Fatalf(
			"metadata must be COALESCEd: a system row with NULL metadata makes NOT(true AND NULL) = NULL, "+
				"which silently drops the message from every JOIN and WHERE; got: %s",
			got,
		)
	}
}

func TestRealMessageSQL_HonoursTheAlias(t *testing.T) {
	got := realMessageSQL("m2")

	if strings.Contains(got, "cm.") {
		t.Fatalf("predicate leaked the cm alias: %s", got)
	}
	if !strings.Contains(got, "m2.message_type") || !strings.Contains(got, "m2.metadata") {
		t.Fatalf("predicate did not apply the alias: %s", got)
	}
}
