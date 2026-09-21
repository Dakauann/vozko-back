package audience

import (
	"strings"

	"vozko/domain/shared"
)

type AuthorRole string

const (
	RoleUnknown       AuthorRole = "unknown"
	RolePolitician    AuthorRole = "politician"
	RoleJournalist    AuthorRole = "journalist"
	RolePublicServant AuthorRole = "public_servant"
	RoleBusinessOwner AuthorRole = "business_owner"
	RoleProfessional  AuthorRole = "professional"
	RoleActivist      AuthorRole = "activist"
)

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

func ParseAuthorRole(value string) (AuthorRole, bool) {
	normalized := AuthorRole(strings.ToLower(strings.TrimSpace(value)))
	if !normalized.Valid() {
		return "", false
	}
	return normalized, true
}

const (
	MinCommentsForRole  = 12
	RoleRecomputeGrowth = 2
)

type AuthorRoleInference struct {
	Role            AuthorRole          `json:"role"`
	Confidence      shared.QualityLevel `json:"confidence"`
	BasedOnComments int                 `json:"basedOnComments"`
	Rationale       string              `json:"rationale,omitempty"`
}

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

const MaxRoleRationale = 200

func (i AuthorRoleInference) Displayable() bool {
	if i.Role == RoleUnknown || !i.Role.Valid() {
		return false
	}
	if i.BasedOnComments < MinCommentsForRole {
		return false
	}
	return i.Confidence == shared.QualityLevelMedium || i.Confidence == shared.QualityLevelHigh
}

func ShouldInferRole(previous AuthorRoleInference, comments int) bool {
	if comments < MinCommentsForRole {
		return false
	}
	if previous.BasedOnComments == 0 {
		return true
	}
	return comments >= previous.BasedOnComments*RoleRecomputeGrowth
}
