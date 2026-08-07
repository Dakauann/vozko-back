package unofficial_whatsapp

import "errors"

// Department scoping for connected numbers.
//
// A workspace can dedicate a number to one department — Comercial, Cobrança —
// and from then on it belongs to that team: only its members see it in the
// numbers list, only they may send from it, and only they enter the round-robin
// for messages that arrive on it.
//
// Most of that is already true without this file, and deliberately so. The
// department a conversation belongs to is resolved from its INSTANCE
// (see ConversationRepository.DepartmentIDForEntry), and the channel-neutral
// machinery reads it: the inbox union filters on it, the round-robin restricts
// eligible users to that department, and the websocket authorizer refuses to
// open a conversation outside it. Those paths are shared with every other
// channel and needed no change.
//
// What this file adds is the half that has no channel-neutral home: the NUMBER
// itself. A conversation is department-scoped by the machinery above, but the
// instance list, the instance detail and cold outbound are this channel's own
// endpoints, and nothing was checking them — so an operator outside the
// department could still see the number and start a conversation from it.
//
// The rule is stated ONCE here because five call sites need the same answer and
// a rule re-derived per handler is a rule that will differ in one of them.

// ErrInstanceOutsideDepartment means the caller is not in the department this
// number belongs to.
//
// Deliberately indistinguishable from "no such number" at the HTTP boundary:
// whether a number exists in a department you are not in is itself information,
// and an operator has no legitimate use for it.
var ErrInstanceOutsideDepartment = errors.New("this number belongs to another department")

// DepartmentScope is the set of departments a caller may act within.
//
// Produced by the platform's existing authorizer, so this channel does not
// re-derive membership, roles or permissions — it only applies the answer.
type DepartmentScope struct {
	// DepartmentIDs are the departments the caller belongs to. Consulted only
	// when Restrict is true.
	DepartmentIDs []string
	// Restrict is false for workspace owners and admins, who see every number.
	// It is true for an ordinary member, whose view is limited to their own
	// departments.
	Restrict bool
}

// Unrestricted is the scope of a caller who sees everything. Named rather than
// spelled as a zero value at each call site, so "no restriction" is never
// confused with "restricted to nothing" — the two are opposite and the zero
// value of a bool would quietly pick one.
func Unrestricted() DepartmentScope { return DepartmentScope{} }

// Allows reports whether this caller may see and use a number.
//
// Three cases, and the third is the one worth stating out loud:
//
//   - Not restricted: everything. Owners and admins.
//   - Restricted, number in one of their departments: allowed.
//   - Restricted, number with NO department: REFUSED.
//
// That last one is fail-closed and it matches the conversation machinery
// exactly. The inbox's SQL compares the department column with ANY(...), which a
// NULL never satisfies, so a restricted operator already cannot see the
// conversations of an unassigned number. Showing them the number itself — one
// they can neither read nor answer from — would be a listing that only raises
// questions. A workspace that wants a number visible to everyone gives it no
// departments at all, and then nobody is restricted.
func (s DepartmentScope) Allows(instanceDepartmentID *string) bool {
	if !s.Restrict {
		return true
	}
	if instanceDepartmentID == nil || *instanceDepartmentID == "" {
		return false
	}
	for _, id := range s.DepartmentIDs {
		if id == *instanceDepartmentID {
			return true
		}
	}
	return false
}

// AllowsInstance is Allows for a loaded instance, so callers do not reach into
// the struct to apply a rule this package owns.
func (s DepartmentScope) AllowsInstance(instance *Instance) bool {
	if instance == nil {
		return false
	}
	return s.Allows(instance.DepartmentID)
}

// BlocksEverything reports whether this scope can match no number at all.
//
// A restricted caller who belongs to no department is in exactly that position.
// Worth naming because the repository can skip the query entirely, and because
// the alternative — an IN () with an empty list — is a SQL error in some
// dialects and a silent match-all in others.
func (s DepartmentScope) BlocksEverything() bool {
	return s.Restrict && len(s.DepartmentIDs) == 0
}
