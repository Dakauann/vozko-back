package audience

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
		"unknown":         {"karma", "", false},
		"empty":           {"", "", false},
		"column name":     {"severity_high_count", "", false},
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

func TestEveryAdvertisedAuthorSortKeyParses(t *testing.T) {
	for _, key := range AllAuthorSortKeys() {
		if !key.Valid() {
			t.Errorf("%q is offered but does not parse", key)
		}
	}
}

func TestDefaultAuthorSortIsAValidKey(t *testing.T) {
	if !DefaultAuthorSort.Key.Valid() {
		t.Fatalf("default sort key %q is not valid", DefaultAuthorSort.Key)
	}
	if !DefaultAuthorSort.Ascending {
		t.Error("the default should surface the worst reputations first")
	}
}
