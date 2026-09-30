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

func TestRealMessageSQL_ExcludesImportedHistory(t *testing.T) {
	got := realMessageSQL("cm")

	if !strings.Contains(got, "COALESCE(cm.metadata->>'backfill', '') <> 'true'") {
		t.Fatalf("a message imported from the phone's history happened before Vozko; it must not count as attendance: %s", got)
	}
}

func TestOwnerResponseTimeStartsFromARealMessage(t *testing.T) {
	got := ownerResponseLateralsSQL()

	if !strings.Contains(got, liveMessageSQL("m")) {
		t.Fatalf("response time must start at the first real customer message, not days-old imported history: %s", got)
	}
}
