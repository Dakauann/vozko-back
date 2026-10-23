package shared

import (
	"errors"
	"testing"
)

func TestParseYesNo(t *testing.T) {
	cases := []struct {
		raw     string
		want    bool
		refused bool
	}{
		{raw: "Sim", want: true},
		{raw: " não ", want: false},
		{raw: "NAO", want: false},
		{raw: "x", want: true},
		{raw: "true", want: true},
		{raw: "0", want: false},
		{raw: "talvez", refused: true},
		{raw: "", refused: true},
	}
	for _, c := range cases {
		got, err := ParseYesNo(c.raw)
		if c.refused {
			if !errors.Is(err, ErrYesNoInvalid) {
				t.Fatalf("ParseYesNo(%q) err = %v, want ErrYesNoInvalid", c.raw, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("ParseYesNo(%q) = %v, %v; want %v", c.raw, got, err, c.want)
		}
	}
}
