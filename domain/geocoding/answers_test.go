package geocoding

import (
	"testing"
	"time"

	"vozko/domain/geo"
)

func TestAnswerOfRemembersWhatTheProviderSaidAboutTheTextNeverAFailure(t *testing.T) {
	key := AnswerKey{WorkspaceID: "ws-1", Fingerprint: "f-1"}
	tests := []struct {
		name    string
		outcome geo.Outcome
		kept    bool
		kind    AnswerKind
	}{
		{"a located answer", geo.Located(providerFix(geo.PrecisionAddress)), true, AnswerLocated},
		{"an ambiguous answer", geo.Ambiguous(), true, AnswerAmbiguous},
		{"no result", geo.NotFound(), true, AnswerNotFound},
		{"a refused query text", geo.Unavailable(geo.ReasonQueryRefused, 0), true, AnswerRefused},
		{"a refused account", geo.Unavailable(geo.ReasonKeyRejected, 0), false, ""},
		{"a provider that is down", geo.Unavailable(geo.ReasonProviderDown, 0), false, ""},
		{"a rate limit", geo.Unavailable(geo.ReasonRateLimited, time.Minute), false, ""},
		{"an unreadable answer", geo.Unavailable(geo.ReasonInvalidAnswer, 0), false, ""},
		{"a paused provider", geo.Unavailable(geo.ReasonProviderPaused, time.Hour), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answer, kept := AnswerOf(ProviderOpenCage, key, tt.outcome, at)
			if kept != tt.kept {
				t.Fatalf("AnswerOf() kept = %v, want %v", kept, tt.kept)
			}
			if !kept {
				return
			}
			if answer.Key != key || answer.Kind != tt.kind || answer.Provider != ProviderOpenCage || !answer.ResolvedAt.Equal(at) || !answer.Valid() {
				t.Fatalf("AnswerOf() = %+v, want a valid %q answer for %+v", answer, tt.kind, key)
			}
			replayed := answer.Outcome()
			if replayed.Kind != tt.outcome.Kind || replayed.Reason != tt.outcome.Reason {
				t.Fatalf("Outcome() = %+v, want the provider's %+v", replayed, tt.outcome)
			}
		})
	}
}

func TestALocatedAnswerReplaysTheProvidersPosition(t *testing.T) {
	fix := providerFix(geo.PrecisionStreet)
	answer, _ := AnswerOf(ProviderOpenCage, AnswerKey{WorkspaceID: "ws-1", Fingerprint: "f-1"}, geo.Located(fix), at.Add(time.Hour))
	got := answer.Outcome()
	if got.Kind != geo.OutcomeLocated || !geo.SameFix(&got.Fix, &fix) || got.Fix.Source != geo.SourceProvider || got.Fix.Provider != string(ProviderOpenCage) {
		t.Fatalf("Outcome() = %+v, want the provider's street fix", got)
	}
}

func TestAnswerValidity(t *testing.T) {
	located, _ := AnswerOf(ProviderOpenCage, AnswerKey{WorkspaceID: "ws-1", Fingerprint: "f-1"}, geo.Located(providerFix(geo.PrecisionAddress)), at)
	tests := []struct {
		name  string
		edit  func(a *Answer)
		valid bool
	}{
		{"as remembered", func(*Answer) {}, true},
		{"no workspace", func(a *Answer) { a.Key.WorkspaceID = " " }, false},
		{"no fingerprint", func(a *Answer) { a.Key.Fingerprint = "" }, false},
		{"an unknown kind", func(a *Answer) { a.Kind = "maybe" }, false},
		{"located without a position", func(a *Answer) { a.Fix = nil }, false},
		{"located outside the valid range", func(a *Answer) { a.Fix = &geo.Fix{Point: geo.Point{Lat: 200}, Precision: geo.PrecisionAddress} }, false},
		{"no result carrying a position", func(a *Answer) { a.Kind = AnswerNotFound }, false},
		{"no result without a position", func(a *Answer) { a.Kind, a.Fix = AnswerNotFound, nil }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := located
			fix := *located.Fix
			a.Fix = &fix
			tt.edit(&a)
			if got := a.Valid(); got != tt.valid {
				t.Fatalf("Valid() = %v, want %v for %+v", got, tt.valid, a)
			}
		})
	}
}

func TestAnswerKeysOfDeduplicatesClaimsOfTheSameText(t *testing.T) {
	keys := AnswerKeysOf([]AnswerKey{
		{WorkspaceID: "ws-1", Fingerprint: "f-1"},
		{WorkspaceID: "ws-1", Fingerprint: "f-1"},
		{WorkspaceID: "ws-2", Fingerprint: "f-1"},
		{WorkspaceID: "ws-1", Fingerprint: " "},
	})
	if len(keys) != 2 || keys[0] != (AnswerKey{WorkspaceID: "ws-1", Fingerprint: "f-1"}) || keys[1] != (AnswerKey{WorkspaceID: "ws-2", Fingerprint: "f-1"}) {
		t.Fatalf("AnswerKeysOf() = %+v, want one key per workspace and text, never across workspaces", keys)
	}
}
