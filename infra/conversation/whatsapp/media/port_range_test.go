package media

import "testing"

func TestPortRangeOverlaps(t *testing.T) {
	cases := []struct {
		name         string
		aStart, aEnd int
		bStart, bEnd int
		want         bool
	}{
		{"disjoint below", 51000, 58000, 40000, 50000, false},
		{"disjoint above", 30000, 39999, 40000, 50000, false},
		{"adjacent no touch", 50001, 60000, 40000, 50000, false},
		{"touch at boundary", 50000, 60000, 40000, 50000, true},
		{"fully contained", 42000, 43000, 40000, 50000, true},
		{"contains", 40000, 50000, 42000, 43000, true},
		{"partial overlap", 45000, 55000, 40000, 50000, true},
		{"identical", 40000, 50000, 40000, 50000, true},
		{"reversed args still detected", 50000, 40000, 45000, 46000, true},
		{"single port inside", 45000, 45000, 40000, 50000, true},
		{"single port outside", 60000, 60000, 40000, 50000, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PortRangeOverlaps(c.aStart, c.aEnd, c.bStart, c.bEnd); got != c.want {
				t.Errorf("PortRangeOverlaps(%d-%d, %d-%d) = %v, want %v",
					c.aStart, c.aEnd, c.bStart, c.bEnd, got, c.want)
			}
		})
	}
}

func TestSIPAndWhatsAppRangesDisjoint(t *testing.T) {
	const sipStart, sipEnd = 40000, 50000

	if PortRangeOverlaps(51000, 58000, sipStart, sipEnd) {
		t.Errorf("recommended WhatsApp range 51000-58000 must NOT overlap SIP RTP %d-%d", sipStart, sipEnd)
	}

	if !PortRangeOverlaps(32768, 60999, sipStart, sipEnd) {
		t.Errorf("OS ephemeral 32768-60999 is expected to overlap SIP RTP %d-%d (the unsafe default)", sipStart, sipEnd)
	}
}

func TestMuxPortDisjointFromSIP(t *testing.T) {
	const sipStart, sipEnd = 40000, 50000
	if PortRangeOverlaps(3479, 3479, sipStart, sipEnd) {
		t.Errorf("default mux port 3479 must NOT be inside SIP RTP %d-%d", sipStart, sipEnd)
	}
	if !PortRangeOverlaps(45000, 45000, sipStart, sipEnd) {
		t.Errorf("a mux port inside SIP RTP %d-%d must be flagged", sipStart, sipEnd)
	}
}
