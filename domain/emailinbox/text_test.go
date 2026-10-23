package emailinbox

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateText(t *testing.T) {
	long := strings.Repeat("á", MaxTranscriptTextBytes)
	cases := []struct {
		name     string
		in       string
		max      int
		wantCut  bool
		wantSame bool
	}{
		{"short stays", "olá", 10, false, true},
		{"exact stays", "abcd", 4, false, true},
		{"long cut on rune boundary", long, MaxTranscriptTextBytes, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cut := TruncateText(tc.in, tc.max)
			if cut != tc.wantCut {
				t.Fatalf("cut = %v, want %v", cut, tc.wantCut)
			}
			if tc.wantSame && got != tc.in {
				t.Fatalf("got %q, want unchanged", got)
			}
			if len(got) > tc.max {
				t.Fatalf("len = %d over %d", len(got), tc.max)
			}
			if !utf8.ValidString(got) {
				t.Fatal("cut inside a rune")
			}
		})
	}
}

func TestReplyHTMLEscapesAndKeepsParagraphs(t *testing.T) {
	got := ReplyHTML("Olá <b>Ana</b> & cia\nlinha dois\n\nnovo parágrafo")
	for _, want := range []string{"&lt;b&gt;Ana&lt;/b&gt; &amp; cia<br>linha dois", "<p", "novo parágrafo</p>"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReplyHTML missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<b>") || strings.Contains(got, "<img") {
		t.Errorf("ReplyHTML leaked markup or tracking: %q", got)
	}
}

func TestReferenceChain(t *testing.T) {
	many := make([]string, 0, MaxReferences+5)
	for i := 0; i < MaxReferences+5; i++ {
		many = append(many, "id"+string(rune('a'+i))+"@x")
	}
	cases := []struct {
		name  string
		prior []string
		add   []string
		want  []string
	}{
		{"start", nil, []string{"<a@x>"}, []string{"a@x"}},
		{"append and dedupe", []string{"a@x", "b@x"}, []string{"b@x", "<c@x>"}, []string{"a@x", "b@x", "c@x"}},
		{"blank and spaced ids dropped", []string{"a@x"}, []string{"", "bad id"}, []string{"a@x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReferenceChain(tc.prior, tc.add...)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("ReferenceChain = %v, want %v", got, tc.want)
			}
		})
	}
	bounded := ReferenceChain(many)
	if len(bounded) != MaxReferences || bounded[0] != many[0] || bounded[len(bounded)-1] != many[len(many)-1] {
		t.Errorf("bounded chain keeps the root and the newest: %v", bounded)
	}
}

func TestNormalizeMessageID(t *testing.T) {
	cases := map[string]string{
		"<abc@x.com>":    "abc@x.com",
		"  <abc@x.com> ": "abc@x.com",
		"abc@x.com":      "abc@x.com",
		"":               "",
		"<a b@x>":        "",
	}
	for in, want := range cases {
		if got := NormalizeMessageID(in); got != want {
			t.Errorf("NormalizeMessageID(%q) = %q, want %q", in, got, want)
		}
	}
}
