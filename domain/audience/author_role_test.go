package audience

import (
	"testing"

	"vozko/domain/shared"
)

func TestAuthorRoleIsAClosedSet(t *testing.T) {
	for _, r := range AllAuthorRoles() {
		if !r.Valid() {
			t.Fatalf("%q is listed but not valid", r)
		}
	}
	for _, bad := range []AuthorRole{"", "influencer", "Politician", "médico"} {
		if bad.Valid() {
			t.Fatalf("%q must not be a valid role", bad)
		}
	}
	if !RoleUnknown.Valid() {
		t.Fatal("unknown is a real answer and must be valid")
	}
}

func TestParseAuthorRoleRefusesWhatItDoesNotKnow(t *testing.T) {
	if got, ok := ParseAuthorRole(" POLITICIAN "); !ok || got != RolePolitician {
		t.Fatalf("got %q, %v", got, ok)
	}
	if _, ok := ParseAuthorRole("influencer"); ok {
		t.Fatal("an unknown label must be refused, not coerced")
	}
}

func TestAuthorRoleInferenceHoldsBackWhenTheEvidenceIsThin(t *testing.T) {
	cases := map[string]struct {
		role       AuthorRole
		confidence shared.QualityLevel
		comments   int
		wantShown  bool
	}{
		"clear signal, enough comments": {RolePolitician, shared.QualityLevelHigh, 12, true},
		"medium confidence is enough":   {RoleJournalist, shared.QualityLevelMedium, 12, true},
		"low confidence is not":         {RolePolitician, shared.QualityLevelLow, 40, false},
		"no confidence is not":          {RolePolitician, shared.QualityLevelNone, 40, false},
		"too few comments":              {RolePolitician, shared.QualityLevelHigh, MinCommentsForRole - 1, false},
		"unknown is never shown":        {RoleUnknown, shared.QualityLevelHigh, 40, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			inf := AuthorRoleInference{Role: c.role, Confidence: c.confidence, BasedOnComments: c.comments}
			if got := inf.Displayable(); got != c.wantShown {
				t.Fatalf("Displayable = %v, want %v", got, c.wantShown)
			}
		})
	}
}

func TestAuthorRoleInferenceNormalizes(t *testing.T) {
	inf := AuthorRoleInference{Role: "influencer", Confidence: "very", BasedOnComments: 50}
	inf.Normalize()
	if inf.Role != RoleUnknown {
		t.Fatalf("role = %q, want unknown", inf.Role)
	}
	if inf.Confidence != shared.QualityLevelNone {
		t.Fatalf("confidence = %q, want none", inf.Confidence)
	}
	if inf.Displayable() {
		t.Fatal("a normalised-away inference must not be shown")
	}
}

func TestShouldInferRole(t *testing.T) {
	t.Run("never inferred, enough comments", func(t *testing.T) {
		if !ShouldInferRole(AuthorRoleInference{}, MinCommentsForRole) {
			t.Fatal("an author over the threshold with no inference must be inferred")
		}
	})
	t.Run("never inferred, too few comments", func(t *testing.T) {
		if ShouldInferRole(AuthorRoleInference{}, MinCommentsForRole-1) {
			t.Fatal("too few words to judge anybody by")
		}
	})
	t.Run("inferred, corpus barely grew", func(t *testing.T) {
		prev := AuthorRoleInference{Role: RolePolitician, Confidence: shared.QualityLevelHigh, BasedOnComments: 20}
		if ShouldInferRole(prev, 21) {
			t.Fatal("one more comment must not re-bill the whole corpus")
		}
	})
	t.Run("inferred, corpus grew materially", func(t *testing.T) {
		prev := AuthorRoleInference{Role: RolePolitician, Confidence: shared.QualityLevelHigh, BasedOnComments: 20}
		if !ShouldInferRole(prev, 41) {
			t.Fatal("a corpus that doubled deserves a fresh look")
		}
	})
	t.Run("previously unknown, corpus grew", func(t *testing.T) {
		prev := AuthorRoleInference{Role: RoleUnknown, Confidence: shared.QualityLevelNone, BasedOnComments: 12}
		if !ShouldInferRole(prev, 25) {
			t.Fatal("an unknown answer is worth revisiting once there is more to read")
		}
	})
}
