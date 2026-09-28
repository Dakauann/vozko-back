package commentautomation

import (
	"errors"
	"testing"

	"vozko/domain/shared"
)

func comment(text string) Subject {
	return Subject{ContainerID: "m-1", AuthorName: "maria", Text: text}
}

func promoRule() *Rule {
	r := &Rule{
		Source:          shared.EntryTypeInstagram,
		Name:            "Promo",
		Enabled:         true,
		Match:           MatchContains,
		Keywords:        []string{"promoção", "quero"},
		Actions:         []Action{ActionPublicReply},
		PublicReplyText: "oi {{username}}!",
	}
	r.Normalize()
	return r
}

func TestRuleMatching(t *testing.T) {
	rule := promoRule()
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"exact keyword", "promoção", true},
		{"inside a sentence", "tem promoção ainda?", true},
		{"second keyword", "eu QUERO esse", true},
		{"unaccented", "tem promocao?", true},
		{"uppercase unaccented", "PROMOCAO!!", true},
		{"no keyword", "que lindo", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rule.Matches(comment(tc.text)); got != tc.want {
				t.Errorf("Matches(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestExactMatch(t *testing.T) {
	rule := &Rule{Name: "Exact", Enabled: true, Match: MatchExact, Keywords: []string{"EU QUERO"},
		Actions: []Action{ActionPrivateReply}, PrivateReplyText: "oi"}
	rule.Normalize()
	if !rule.Matches(comment("eu quero")) {
		t.Error("exact match should ignore case")
	}
	if rule.Matches(comment("eu quero muito")) {
		t.Error("exact match must not fire on a superset")
	}
}

func TestMatchAnySkipsOurOwnAndHiddenComments(t *testing.T) {
	rule := &Rule{Name: "All", Enabled: true, Match: MatchAny, Actions: []Action{ActionHide}}
	rule.Normalize()
	if !rule.Matches(comment("anything at all")) {
		t.Error("MatchAny should fire on any comment")
	}
	ours := comment("our own reply")
	ours.IsOurs = true
	hidden := comment("x")
	hidden.Hidden = true
	if rule.Matches(ours) || rule.Matches(hidden) {
		t.Error("our own and hidden comments are never processed")
	}
}

func TestDisabledRuleNeverMatches(t *testing.T) {
	rule := promoRule()
	rule.Enabled = false
	if rule.Matches(comment("promoção")) {
		t.Error("a disabled rule must not fire")
	}
}

func TestContainerScope(t *testing.T) {
	rule := promoRule()
	rule.ContainerID = "m-1"
	other := comment("promoção")
	other.ContainerID = "m-2"
	if !rule.Matches(comment("promoção")) || rule.Matches(other) {
		t.Error("a post-scoped rule fires only on its own post")
	}
	rule.ContainerID = ""
	if !rule.Matches(other) {
		t.Error("an account-wide rule fires on any post")
	}
}

func TestNormalize(t *testing.T) {
	rule := &Rule{Name: "  Promo  ", Keywords: []string{" promoção ", "PROMOCAO", "", "quero"},
		Actions: []Action{ActionHide, ActionHide, ActionPublicReply}}
	rule.Normalize()
	if len(rule.Keywords) != 2 || len(rule.Actions) != 2 || rule.Name != "Promo" || rule.Match != MatchContains {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestValidateAgainstTheSourcesActions(t *testing.T) {
	if err := promoRule().Validate(); err != nil {
		t.Fatalf("a complete rule should validate: %v", err)
	}
	cases := map[string]func(*Rule){
		"no name":            func(r *Rule) { r.Name = "" },
		"no actions":         func(r *Rule) { r.Actions = nil },
		"keywords required":  func(r *Rule) { r.Keywords = nil },
		"reply text missing": func(r *Rule) { r.PublicReplyText = "" },
		"unknown action":     func(r *Rule) { r.Actions = []Action{"launch_missiles"} },
		"unknown source":     func(r *Rule) { r.Source = "telegram" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := promoRule()
			mutate(r)
			if err := r.Validate(); err == nil {
				t.Error("expected validation to fail")
			}
		})
	}
}

func TestDeleteAndLikeAreFacebookOnly(t *testing.T) {
	for _, action := range []Action{ActionDelete, ActionLike} {
		ig := &Rule{Source: shared.EntryTypeInstagram, Name: "x", Match: MatchAny, Actions: []Action{action}}
		if err := ig.Validate(); !errors.Is(err, ErrActionUnsupported) {
			t.Errorf("instagram %s: %v", action, err)
		}
		fb := &Rule{Source: shared.EntryTypeFacebook, Name: "x", Match: MatchAny, Actions: []Action{action}}
		if err := fb.Validate(); err != nil {
			t.Errorf("facebook %s: %v", action, err)
		}
	}
}

func TestRenderText(t *testing.T) {
	c := comment("tem promoção?")
	got := RenderText("Oi {{username}}, sobre \"{{comment}}\", te chamei no direct!", c)
	if got != `Oi maria, sobre "tem promoção?", te chamei no direct!` {
		t.Errorf("RenderText = %q", got)
	}
	if got := RenderText("obrigado!", c); got != "obrigado!" {
		t.Errorf("plain template altered: %q", got)
	}
}
