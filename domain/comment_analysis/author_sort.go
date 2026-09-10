package comment_analysis

import "strings"

// Ranking the authors table (§1).
//
// The filters for "who commented badly" already existed; the ORDER did not, so
// the repository returned whatever the table gave it and "ranked" meant nothing.
// These are the client-facing keys, and the repository owns the mapping from key
// to SQL — the same division lead.SortKey uses, and the reason no layer above
// infra ever names a column.

// AuthorSortKey is a stable, client-facing ordering key for the authors table.
type AuthorSortKey string

const (
	// SortAuthorReputation is the default: the signed ledger, so the two ends
	// of the list are the two questions actually being asked — who is most
	// hostile, and who is most supportive.
	SortAuthorReputation AuthorSortKey = "reputation"
	SortAuthorComments   AuthorSortKey = "comments"
	SortAuthorNegative   AuthorSortKey = "negative"
	SortAuthorPositive   AuthorSortKey = "positive"
	SortAuthorSeverity   AuthorSortKey = "severity"
	SortAuthorLastSeen   AuthorSortKey = "lastSeen"
	SortAuthorFirstSeen  AuthorSortKey = "firstSeen"
)

// DefaultAuthorSort is what an unsorted request gets: the worst reputations
// first. A moderation table opens on the people who need attention, not on
// whoever happens to be first by id.
var DefaultAuthorSort = Sort{Key: SortAuthorReputation, Ascending: true}

// Sort is one ordering instruction.
//
// A bool rather than a direction string: there are exactly two directions, and
// a string invites "descending", "DESC" and "desc" to mean three things.
type Sort struct {
	Key       AuthorSortKey
	Ascending bool
}

// AllAuthorSortKeys lists every valid key, in the order a UI should offer them.
func AllAuthorSortKeys() []AuthorSortKey {
	return []AuthorSortKey{
		SortAuthorReputation,
		SortAuthorComments,
		SortAuthorNegative,
		SortAuthorPositive,
		SortAuthorSeverity,
		SortAuthorLastSeen,
		SortAuthorFirstSeen,
	}
}

// ParseAuthorSortKey resolves a case-insensitive client value to a known key.
//
// Unknown values are REFUSED rather than silently defaulted: a client asking
// for an ordering we do not have has a bug, and answering it with a different
// ordering hides that bug behind a plausible-looking page of results.
func ParseAuthorSortKey(value string) (AuthorSortKey, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", false
	}
	for _, key := range AllAuthorSortKeys() {
		if strings.ToLower(string(key)) == normalized {
			return key, true
		}
	}
	return "", false
}

func (k AuthorSortKey) Valid() bool {
	_, ok := ParseAuthorSortKey(string(k))
	return ok
}
