package comment_analysis

import "testing"

func TestParseAuthorSortKey(t *testing.T) {
	cases := map[string]struct {
		in   string
		want AuthorSortKey
		ok   bool
	}{
		"exact":           {"reputation", SortAuthorReputation, true},
		"camel from a UI": {"lastSeen", SortAuthorLastSeen, true},
		"upper":           {"NEGATIVE", SortAuthorNegative, true},
		"padded":          {"  severity  ", SortAuthorSeverity, true},
		// Refused, not defaulted. A client asking for an ordering we do not
		// have has a bug; answering with a different one hides it behind a page
		// of plausible results.
		"unknown": {"karma", "", false},
		"empty":   {"", "", false},
		// Not a column name the client may guess at: the repository owns the
		// key→SQL mapping, and leaking a column would freeze the schema.
		"column name": {"severity_high_count", "", false},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := ParseAuthorSortKey(c.in)
			if ok != c.ok || got != c.want {
				t.Fatalf("ParseAuthorSortKey(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

// Every advertised key must parse. A key offered by AllAuthorSortKeys but
// rejected by the parser would render a sort control that errors on click.
func TestEveryAdvertisedAuthorSortKeyParses(t *testing.T) {
	for _, key := range AllAuthorSortKeys() {
		if !key.Valid() {
			t.Errorf("%q is offered but does not parse", key)
		}
	}
}

// The default has to be one of the real keys, or an unsorted request asks the
// repository for an ordering it cannot map.
func TestDefaultAuthorSortIsAValidKey(t *testing.T) {
	if !DefaultAuthorSort.Key.Valid() {
		t.Fatalf("default sort key %q is not valid", DefaultAuthorSort.Key)
	}
	// Ascending on reputation means the most hostile first: a moderation table
	// opens on the people who need attention.
	if !DefaultAuthorSort.Ascending {
		t.Error("the default should surface the worst reputations first")
	}
}
