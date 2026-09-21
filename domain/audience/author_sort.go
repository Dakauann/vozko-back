package audience

import "strings"

type AuthorSortKey string

const (
	SortAuthorReputation AuthorSortKey = "reputation"
	SortAuthorComments   AuthorSortKey = "comments"
	SortAuthorNegative   AuthorSortKey = "negative"
	SortAuthorPositive   AuthorSortKey = "positive"
	SortAuthorSeverity   AuthorSortKey = "severity"
	SortAuthorLastSeen   AuthorSortKey = "lastSeen"
	SortAuthorFirstSeen  AuthorSortKey = "firstSeen"
)

var DefaultAuthorSort = Sort{Key: SortAuthorReputation, Ascending: true}

type Sort struct {
	Key       AuthorSortKey
	Ascending bool
}

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
