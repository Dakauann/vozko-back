package container

import (
	"strings"
	"testing"
)

func TestCheckPortLayout(t *testing.T) {
	// Linux ephemeral floor for these cases.
	const eLo, eHi = 32768, 60999

	cases := []struct {
		name string
		in   portLayoutInputs
		want string // substring the first violation must contain; "" = must be safe
	}{
		{
			name: "safe layout (fixed default mux)",
			in:   portLayoutInputs{mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "",
		},
		{
			name: "media disabled -> no checks",
			in:   portLayoutInputs{mux: 0, ephLo: eLo, ephHi: eHi},
			want: "",
		},
		{
			name: "mux inside the OS ephemeral range",
			in:   portLayoutInputs{mux: 40000, ephLo: eLo, ephHi: eHi},
			want: "WhatsApp media mux",
		},
		{
			name: "mux exactly at the ephemeral floor",
			in:   portLayoutInputs{mux: eLo, ephLo: eLo, ephHi: eHi},
			want: "WhatsApp media mux",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkPortLayout(c.in)
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected a safe layout, got violations: %v", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected a violation containing %q, got none", c.want)
			}
			joined := strings.Join(got, " | ")
			if !strings.Contains(joined, c.want) {
				t.Fatalf("violations %q do not mention %q", joined, c.want)
			}
		})
	}
}

func TestOSEphemeralRange_Sane(t *testing.T) {
	lo, hi := osEphemeralRange()
	if lo <= 0 || hi < lo || hi > 65535 {
		t.Fatalf("osEphemeralRange returned an implausible range %d-%d", lo, hi)
	}
}

func TestParseEphemeralRange(t *testing.T) {
	cases := []struct {
		in     string
		wantLo int
		wantHi int
		wantOK bool
	}{
		{"32768\t60999\n", 32768, 60999, true}, // real Linux /proc content
		{"  10000 20000  ", 10000, 20000, true},
		{"49152 65535", 49152, 65535, true},
		{"32768", 0, 0, false},       // one field
		{"", 0, 0, false},            // empty
		{"abc def", 0, 0, false},     // non-numeric
		{"60999 32768", 0, 0, false}, // hi < lo
		{"0 1000", 0, 0, false},      // lo <= 0
	}
	for _, c := range cases {
		lo, hi, ok := parseEphemeralRange(c.in)
		if ok != c.wantOK || (ok && (lo != c.wantLo || hi != c.wantHi)) {
			t.Fatalf("parseEphemeralRange(%q) = (%d,%d,%v), want (%d,%d,%v)", c.in, lo, hi, ok, c.wantLo, c.wantHi, c.wantOK)
		}
	}
}
