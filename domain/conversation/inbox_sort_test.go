package conversation

import (
	"errors"
	"testing"
)

func TestParseInboxSortMapsTheWireOntoTheExistingSortOrder(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		err  error
	}{
		{"", SortOrderRecent, nil},
		{"recent", SortOrderRecent, nil},
		{" waiting ", SortOrderWaiting, nil},
		{"asc", "", ErrInboxSortUnknown},
		{"oldest", "", ErrInboxSortUnknown},
	}
	for _, c := range cases {
		got, err := ParseInboxSort(c.raw)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("ParseInboxSort(%q) = %q, %v; want %q, %v", c.raw, got, err, c.want, c.err)
		}
	}
}

func TestTheRecentSortIsTodaysDefaultOrder(t *testing.T) {
	if SortOrderRecent != "" {
		t.Fatal("recent must be the zero SortOrder, so every caller that never sets a sort keeps today's order")
	}
}
