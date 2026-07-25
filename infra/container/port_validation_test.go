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
			name: "safe layout (fixed defaults)",
			in:   portLayoutInputs{rtpStart: 16384, rtpEnd: 32767, sipStart: 15060, sipCount: 100, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "",
		},
		{
			name: "safe homolog layout (RTP 10000-20000, SIP 25060-30059)",
			in:   portLayoutInputs{rtpStart: 10000, rtpEnd: 20000, sipStart: 25060, sipCount: 5000, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "",
		},
		{
			name: "media disabled -> no checks",
			in:   portLayoutInputs{rtpStart: 0, rtpEnd: 0, ephLo: eLo, ephHi: eHi},
			want: "",
		},
		{
			name: "RTP inside the ephemeral range (the old 40000-50000 on Linux)",
			in:   portLayoutInputs{rtpStart: 40000, rtpEnd: 50000, sipStart: 25060, sipCount: 5000, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "overlaps the OS ephemeral range",
		},
		{
			name: "odd RTP start",
			in:   portLayoutInputs{rtpStart: 16385, rtpEnd: 32767, sipStart: 15060, sipCount: 100, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "odd",
		},
		{
			name: "SIP trunk range inside RTP",
			in:   portLayoutInputs{rtpStart: 10000, rtpEnd: 30000, sipStart: 15060, sipCount: 100, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "SIP trunk range",
		},
		{
			name: "branch SIP listener inside RTP",
			in:   portLayoutInputs{rtpStart: 5000, rtpEnd: 6000, sipStart: 25060, sipCount: 100, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
			want: "branch SIP listener",
		},
		{
			name: "mux inside RTP",
			in:   portLayoutInputs{rtpStart: 3000, rtpEnd: 4000, sipStart: 25060, sipCount: 100, branchSIP: 5070, mux: 3092, ephLo: eLo, ephHi: eHi},
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
