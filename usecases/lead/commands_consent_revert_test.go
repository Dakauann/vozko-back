package lead_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
)

func TestTheSingleLeadConsentRouteCannotRevertAWithdrawal(t *testing.T) {
	f := newCommandsFixture(t, consentPermissions(), storedLead())
	for _, source := range []lead.ConsentSource{lead.ConsentManual, ""} {
		_, err := f.cmds.RecordConsent(context.Background(), operator(), "l-1", ConsentInput{
			Kind: lead.ConsentRevertWithdrawal, Source: source, Evidence: lead.ConsentEvidence{Type: lead.EvidenceNote, Excerpt: "erro"},
		})
		if err == nil || !(errors.Is(err, lead.ErrConsentNotAllowed) || errors.Is(err, lead.ErrConsentSourceForbidden)) {
			t.Fatalf("source %q: got %v, want a refusal", source, err)
		}
	}
	if len(f.store.saves) != 0 {
		t.Fatal("a refused revert writes nothing")
	}
}
