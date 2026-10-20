package channel

import (
	"strings"
	"testing"
)

type latinOnlyRule struct{}

func (latinOnlyRule) Instruction() string { return "only latin letters" }

func (latinOnlyRule) Check(body string) (TextViolation, bool) {
	if strings.ContainsRune(body, '中') {
		return TextViolation{Code: "charset", Message: "has a han rune"}, true
	}
	return TextViolation{}, false
}

func TestTextViolations(t *testing.T) {
	cases := []struct {
		name  string
		caps  Capabilities
		body  string
		codes []string
	}{
		{"no limits", Capabilities{}, strings.Repeat("a", 10000), nil},
		{"runes within", Capabilities{MaxTextRunes: 5}, "olá!!", nil},
		{"runes over", Capabilities{MaxTextRunes: 4}, "olá!!", []string{TextViolationMaxRunes}},
		{"bytes within", Capabilities{MaxTextBytes: 6}, "olá!!", nil},
		{"bytes over counts the accent twice", Capabilities{MaxTextBytes: 5}, "olá!!", []string{TextViolationMaxBytes}},
		{"both over", Capabilities{MaxTextRunes: 1, MaxTextBytes: 1}, "ab", []string{TextViolationMaxRunes, TextViolationMaxBytes}},
		{"provider rule", Capabilities{ProviderTextRules: []TextRule{latinOnlyRule{}}}, "中", []string{"charset"}},
		{"provider rule and length", Capabilities{MaxTextRunes: 1, ProviderTextRules: []TextRule{latinOnlyRule{}}}, "a中", []string{TextViolationMaxRunes, "charset"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.caps.TextViolations(tc.body)
			if len(got) != len(tc.codes) {
				t.Fatalf("violations = %+v, want codes %v", got, tc.codes)
			}
			for i, code := range tc.codes {
				if got[i].Code != code {
					t.Errorf("violation %d = %q, want %q", i, got[i].Code, code)
				}
				if strings.TrimSpace(got[i].Message) == "" {
					t.Errorf("violation %d carries no message for the model", i)
				}
			}
		})
	}
}

func TestTextTooLongAgreesWithTheLengthRules(t *testing.T) {
	caps := []Capabilities{{}, {MaxTextRunes: 3}, {MaxTextBytes: 3}, {MaxTextRunes: 3, MaxTextBytes: 4}}
	bodies := []string{"", "abc", "abcd", "ááá", "á", "😀"}
	for _, c := range caps {
		for _, body := range bodies {
			if got, want := c.TextTooLong(body), len(c.TextViolations(body)) > 0; got != want {
				t.Errorf("caps %+v body %q: TextTooLong = %v, violations say %v", c, body, got, want)
			}
		}
	}
}

func TestTextInstructions(t *testing.T) {
	if got := (Capabilities{}).TextInstructions(); len(got) != 0 {
		t.Fatalf("a channel without limits states none, got %v", got)
	}
	got := Capabilities{MaxTextRunes: 350, MaxTextBytes: 1000, ProviderTextRules: []TextRule{latinOnlyRule{}}}.TextInstructions()
	if len(got) != 3 {
		t.Fatalf("instructions = %v, want one per rule", got)
	}
	if !strings.Contains(got[0], "350") || !strings.Contains(got[1], "1000") || got[2] != "only latin letters" {
		t.Errorf("instructions = %v", got)
	}
}

func TestReservingLeavesRoomForAPrefix(t *testing.T) {
	cases := []struct {
		name      string
		caps      Capabilities
		prefix    string
		wantOK    bool
		wantRunes int
		wantBytes int
	}{
		{"no prefix", Capabilities{MaxTextRunes: 10}, "", true, 10, 0},
		{"unlimited stays unlimited", Capabilities{}, "aviso\n\n", true, 0, 0},
		{"runes reserved", Capabilities{MaxTextRunes: 10}, "olá\n\n", true, 5, 0},
		{"bytes reserved", Capabilities{MaxTextBytes: 10}, "olá\n\n", true, 0, 4},
		{"prefix fills the limit", Capabilities{MaxTextRunes: 5}, "olá\n\n", false, 0, 0},
		{"prefix over the limit", Capabilities{MaxTextBytes: 3}, "olá\n\n", false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.caps.Reserving(tc.prefix)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.MaxTextRunes != tc.wantRunes || got.MaxTextBytes != tc.wantBytes {
				t.Errorf("room = %d runes %d bytes, want %d and %d", got.MaxTextRunes, got.MaxTextBytes, tc.wantRunes, tc.wantBytes)
			}
		})
	}
}

func TestReservingKeepsTheProviderRules(t *testing.T) {
	caps := Capabilities{MaxTextRunes: 10, ProviderTextRules: []TextRule{latinOnlyRule{}}}
	room, ok := caps.Reserving("ab")
	if !ok || len(room.ProviderTextRules) != 1 {
		t.Fatalf("room = %+v ok = %v", room, ok)
	}
}

func TestWithOutboundBudget(t *testing.T) {
	caps := Capabilities{MaxTextRunes: 350}
	got := caps.WithOutboundBudget(1)
	if got.OutboundBudget != 1 || got.MaxTextRunes != 350 {
		t.Fatalf("caps = %+v", got)
	}
	if caps.OutboundBudget != 0 {
		t.Fatal("WithOutboundBudget must not change the receiver")
	}
}
