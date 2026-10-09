package shared

import (
	"errors"
	"testing"
)

func TestExpectVersion(t *testing.T) {
	v := func(n int64) *int64 { return &n }
	cases := []struct {
		name     string
		current  int64
		expected *int64
		want     error
	}{
		{"same version", 3, v(3), nil},
		{"missing version", 3, nil, ErrVersionRequired},
		{"version below one", 0, v(0), ErrVersionRequired},
		{"negative version", 3, v(-1), ErrVersionRequired},
		{"stale version", 4, v(3), ErrVersionConflict},
		{"version from the future", 3, v(4), ErrVersionConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ExpectVersion(tc.current, tc.expected)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRequireVersion(t *testing.T) {
	for _, v := range []int64{0, -1} {
		if !errors.Is(RequireVersion(v), ErrVersionRequired) {
			t.Fatalf("version %d must be refused", v)
		}
	}
	if err := RequireVersion(1); err != nil {
		t.Fatalf("version 1 is the first one: %v", err)
	}
}
