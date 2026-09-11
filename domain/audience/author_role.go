package audience

import (
	"strings"

	"vozko/domain/shared"
)

// Who is this person (§5).
//
// The ask was "identificação do usuário (pela bio, ou comentários dos posts)".
// There is no bio: Instagram's comment edge returns a username and nothing
// else, and scraping a public profile to fill the gap is a platform-terms
// problem rather than an engineering one. So this reads the only evidence we
// actually hold, which is also the better evidence: a corpus of the person's
// own words across every post they have commented on.
//
// This file is mostly restraint, and deliberately so. Everything else the
// engine derives is a statement about a COMMENT; this is a claim about a real
// member of the public, shown to a customer who may act on it. Three rules
// follow, and they are the reason the type exists at all rather than a string
// on AuthorStats:
//
//  1. The label set is CLOSED. A free-text job title cannot be filtered,
//     counted or checked, and a model asked for one invents a new one daily.
//  2. Nothing is shown below MinCommentsForRole comments AND medium
//     confidence. Below either bar the honest answer is "unknown".
//  3. Nothing keys off it. No routing, no scoring, no automatic action reads
//     this field; it is context for a human, and a wrong chip must cost only
//     a raised eyebrow.

// AuthorRole is the closed taxonomy.
type AuthorRole string

const (
	// RoleUnknown is a real answer and the default one. Most commenters are
	// members of the public with no public role, and saying so is correct.
	RoleUnknown       AuthorRole = "unknown"
	RolePolitician    AuthorRole = "politician"
	RoleJournalist    AuthorRole = "journalist"
	RolePublicServant AuthorRole = "public_servant"
	RoleBusinessOwner AuthorRole = "business_owner"
	RoleProfessional  AuthorRole = "professional"
	RoleActivist      AuthorRole = "activist"
)

// AllAuthorRoles lists the taxonomy, in the order a UI should offer it.
func AllAuthorRoles() []AuthorRole {
	return []AuthorRole{
		RoleUnknown, RolePolitician, RoleJournalist, RolePublicServant,
		RoleBusinessOwner, RoleProfessional, RoleActivist,
	}
}

func (r AuthorRole) Valid() bool {
	switch r {
	case RoleUnknown, RolePolitician, RoleJournalist, RolePublicServant,
		RoleBusinessOwner, RoleProfessional, RoleActivist:
		return true
	}
	return false
}

// ParseAuthorRole resolves a case-insensitive value, refusing anything outside
// the taxonomy. A model that returns "influencer" gets nothing, not a new
// category invented at read time.
func ParseAuthorRole(value string) (AuthorRole, bool) {
	normalized := AuthorRole(strings.ToLower(strings.TrimSpace(value)))
	if !normalized.Valid() {
		return "", false
	}
	return normalized, true
}

const (
	// MinCommentsForRole is how much of somebody's writing we insist on before
	// naming their role at all. Three comments is a mood; a dozen is a voice.
	MinCommentsForRole = 12
	// RoleRecomputeGrowth is how much a corpus must grow before the pass runs
	// again, as a multiple of what the last inference read. Without it every
	// new comment would re-bill a person's entire history.
	RoleRecomputeGrowth = 2
)

// AuthorRoleInference is one inference and everything needed to judge it.
type AuthorRoleInference struct {
	Role AuthorRole `json:"role"`
	// Confidence is the model's own, on the shared ordinal rubric. Ordinal
	// rather than a percentage for the reason the rubric header gives: a model
	// asked for 0-100 returns a number nobody can defend.
	Confidence shared.QualityLevel `json:"confidence"`
	// BasedOnComments is the corpus size the inference read. Shown to the
	// customer, because "politician, from 3 comments" and "politician, from 90"
	// are different claims and the UI must not present them as one.
	BasedOnComments int `json:"basedOnComments"`
	// Rationale is one short sentence in the model's words, so an operator can
	// see WHY and disagree. Never used for anything but display.
	Rationale string `json:"rationale,omitempty"`
}

// Normalize keeps only values this version of the taxonomy recognises. A row
// written by an older build, or by a bad write, reads as unknown rather than as
// a label nothing can interpret.
func (i *AuthorRoleInference) Normalize() {
	if role, ok := ParseAuthorRole(string(i.Role)); ok {
		i.Role = role
	} else {
		i.Role = RoleUnknown
	}
	if !i.Confidence.Valid() {
		i.Confidence = shared.QualityLevelNone
	}
	if i.BasedOnComments < 0 {
		i.BasedOnComments = 0
	}
	i.Rationale = strings.TrimSpace(i.Rationale)
	if len(i.Rationale) > MaxRoleRationale {
		i.Rationale, _ = TruncateRunes(i.Rationale, MaxRoleRationale)
	}
}

// MaxRoleRationale bounds the one free-text field. It is display-only, but it
// is still model output about a real person and does not need to be long.
const MaxRoleRationale = 200

// Displayable is the gate the UI must ask before showing a chip.
//
// Both bars have to clear: enough of the person's words, and enough confidence
// in what they say. "unknown" is never displayed as a chip, because "we do not
// know" is the resting state, not a finding.
func (i AuthorRoleInference) Displayable() bool {
	if i.Role == RoleUnknown || !i.Role.Valid() {
		return false
	}
	if i.BasedOnComments < MinCommentsForRole {
		return false
	}
	return i.Confidence == shared.QualityLevelMedium || i.Confidence == shared.QualityLevelHigh
}

// ShouldInferRole decides whether the author pass is worth its tokens.
//
// It runs when there is nothing yet and the corpus is big enough to judge, or
// when the corpus has grown by RoleRecomputeGrowth since the last look. Anything
// else re-reads words we have already paid to read.
func ShouldInferRole(previous AuthorRoleInference, comments int) bool {
	if comments < MinCommentsForRole {
		return false
	}
	if previous.BasedOnComments == 0 {
		return true
	}
	return comments >= previous.BasedOnComments*RoleRecomputeGrowth
}
