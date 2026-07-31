package instagram

import "testing"

func comment(text string) *Comment {
	return &Comment{
		IGCommentID:  "c-1",
		IGMediaID:    "m-1",
		FromUsername: "maria",
		Text:         text,
	}
}

func promoRule() *CommentRule {
	r := &CommentRule{
		Name:            "Promo",
		Enabled:         true,
		Match:           MatchContains,
		Keywords:        []string{"promoção", "quero"},
		Actions:         []CommentRuleAction{ActionPublicReply},
		PublicReplyText: "oi {{username}}!",
	}
	r.Normalize()
	return r
}

func TestCommentRuleMatching(t *testing.T) {
	rule := promoRule()

	cases := []struct {
		name string
		text string
		want bool
	}{
		{"exact keyword", "promoção", true},
		{"inside a sentence", "tem promoção ainda?", true},
		{"second keyword", "eu QUERO esse", true},
		// A Brazilian audience types the accented and unaccented spellings
		// interchangeably; a rule that only matched one would miss most comments.
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

func TestCommentRuleExactMatch(t *testing.T) {
	rule := &CommentRule{
		Name: "Exact", Enabled: true, Match: MatchExact,
		Keywords: []string{"EU QUERO"},
		Actions:  []CommentRuleAction{ActionPrivateReply}, PrivateReplyText: "oi",
	}
	rule.Normalize()

	if !rule.Matches(comment("eu quero")) {
		t.Error("exact match should ignore case")
	}
	if rule.Matches(comment("eu quero muito")) {
		t.Error("exact match must not fire on a superset")
	}
}

func TestCommentRuleMatchAny(t *testing.T) {
	rule := &CommentRule{
		Name: "Moderate all", Enabled: true, Match: MatchAny,
		Actions: []CommentRuleAction{ActionHide},
	}
	rule.Normalize()

	if !rule.Matches(comment("anything at all")) {
		t.Error("MatchAny should fire on any comment")
	}
	// Even MatchAny must not react to our own comment.
	ours := comment("our own reply")
	ours.IsOurs = true
	if rule.Matches(ours) {
		t.Error("MatchAny must still skip our own comments")
	}
}

// Our public replies arrive back as webhooks. Without this guard a reply rule
// would answer its own answer, forever.
func TestCommentRuleNeverMatchesOurOwnComment(t *testing.T) {
	rule := promoRule()
	own := comment("promoção")
	own.IsOurs = true
	if rule.Matches(own) {
		t.Error("a rule must never react to our own comment")
	}
}

// Re-acting on a hidden comment would fight an operator who moderated it.
func TestCommentRuleSkipsHiddenComments(t *testing.T) {
	rule := promoRule()
	hidden := comment("promoção")
	hidden.Hidden = true
	if rule.Matches(hidden) {
		t.Error("a hidden comment must not be re-processed")
	}
}

func TestCommentRuleDisabledNeverMatches(t *testing.T) {
	rule := promoRule()
	rule.Enabled = false
	if rule.Matches(comment("promoção")) {
		t.Error("a disabled rule must not fire")
	}
}

func TestCommentRuleMediaScope(t *testing.T) {
	rule := promoRule()
	rule.IGMediaID = "m-1"

	if !rule.Matches(comment("promoção")) {
		t.Error("a post-scoped rule must fire on its own post")
	}

	other := comment("promoção")
	other.IGMediaID = "m-2"
	if rule.Matches(other) {
		t.Error("a post-scoped rule must not fire on another post")
	}

	// An account-wide rule (empty media id) fires on every post.
	rule.IGMediaID = ""
	if !rule.Matches(other) {
		t.Error("an account-wide rule must fire on any post")
	}
}

func TestCommentRuleNormalize(t *testing.T) {
	rule := &CommentRule{
		Name:     "  Promo  ",
		Keywords: []string{" promoção ", "PROMOCAO", "", "quero"},
		Actions:  []CommentRuleAction{ActionHide, ActionHide, ActionPublicReply},
	}
	rule.Normalize()

	// "promoção" and "PROMOCAO" fold to the same keyword; keeping both would
	// double-scan every comment for no benefit.
	if len(rule.Keywords) != 2 {
		t.Errorf("expected duplicates and blanks removed, got %v", rule.Keywords)
	}
	if len(rule.Actions) != 2 {
		t.Errorf("expected duplicate actions removed, got %v", rule.Actions)
	}
	if rule.Name != "Promo" {
		t.Errorf("name not trimmed: %q", rule.Name)
	}
	if rule.Match != MatchContains {
		t.Errorf("match should default to contains, got %q", rule.Match)
	}
}

func TestCommentRuleValidate(t *testing.T) {
	valid := promoRule()
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete rule should validate: %v", err)
	}

	cases := map[string]func(*CommentRule){
		"no name":            func(r *CommentRule) { r.Name = "" },
		"no actions":         func(r *CommentRule) { r.Actions = nil },
		"keywords required":  func(r *CommentRule) { r.Keywords = nil },
		"reply text missing": func(r *CommentRule) { r.PublicReplyText = "" },
		"unknown action":     func(r *CommentRule) { r.Actions = []CommentRuleAction{"launch_missiles"} },
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

	// MatchAny needs no keywords: it fires on everything by design.
	any := &CommentRule{Name: "All", Enabled: true, Match: MatchAny, Actions: []CommentRuleAction{ActionHide}}
	any.Normalize()
	if err := any.Validate(); err != nil {
		t.Errorf("MatchAny should not require keywords: %v", err)
	}
}

func TestRenderText(t *testing.T) {
	c := comment("tem promoção?")
	got := RenderText("Oi {{username}}, sobre \"{{comment}}\" — te chamei no direct!", c)
	want := `Oi maria, sobre "tem promoção?" — te chamei no direct!`
	if got != want {
		t.Errorf("RenderText = %q, want %q", got, want)
	}

	// A template with no variables passes through untouched.
	if got := RenderText("obrigado!", c); got != "obrigado!" {
		t.Errorf("plain template altered: %q", got)
	}
	// Nil comment must not panic.
	if got := RenderText("hi {{username}}", nil); got != "hi {{username}}" {
		t.Errorf("nil comment should return the template: %q", got)
	}
}
