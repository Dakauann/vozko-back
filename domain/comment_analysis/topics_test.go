package comment_analysis

import (
	"errors"
	"testing"
)

// Topics are a CLOSED set per account (§5.2). Free-text topics drift into
// hundreds of near-duplicates within a week; a closed set is the only thing
// that makes the cluster view aggregatable.

func TestNormalizeTopicKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Saúde Pública", "saude-publica"},
		{"saude publica", "saude-publica"},
		{"  Asfalto e Pavimentação ", "asfalto-e-pavimentacao"},
		{"Educação/Escolas", "educacao-escolas"},
		{"promo!!!", "promo"},
		{"a  b", "a-b"},
		{"--x--", "x"},
		{"", ""},
		{"!!!", ""},
		{"Preço R$ 10", "preco-r-10"},
	}
	for _, tc := range cases {
		if got := NormalizeTopicKey(tc.in); got != tc.want {
			t.Errorf("NormalizeTopicKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// "Saúde Pública" and "saude publica" are one topic. This is the test the
// plan names: accents must not fork a topic.
func TestNormalizeTopicKey_CollapsesAccents(t *testing.T) {
	if NormalizeTopicKey("Saúde Pública") != NormalizeTopicKey("saude publica") {
		t.Fatal("accented and unaccented spellings must normalise to one key")
	}
}

func TestOtherTopic(t *testing.T) {
	o := OtherTopic()
	if o.Key != TopicKeyOther {
		t.Fatalf("OtherTopic().Key = %q", o.Key)
	}
}

// "other" is the pressure valve and must always be present, whatever the
// operator saved.
func TestTopicSet_NormalizeAlwaysHasOther(t *testing.T) {
	ts := TopicSet{{Key: "saude", Label: "Saúde"}}
	ts = ts.Normalize()
	if !ts.Has(TopicKeyOther) {
		t.Fatal("normalised set must contain other")
	}
	// Placed last, so the model reads the real topics first.
	if ts[len(ts)-1].Key != TopicKeyOther {
		t.Fatalf("other should be last, got %+v", ts)
	}
	// And only once, even if the operator added it themselves.
	ts = TopicSet{OtherTopic(), {Key: "saude", Label: "Saúde"}, {Key: "Other", Label: "Outros"}}.Normalize()
	if n := ts.count(TopicKeyOther); n != 1 {
		t.Fatalf("other appears %d times, want 1", n)
	}
}

func TestTopicSet_NormalizeDedupesAndFolds(t *testing.T) {
	ts := TopicSet{
		{Key: "", Label: "Saúde Pública"},
		{Key: "SAUDE-PUBLICA", Label: "Saúde"},
		{Key: "", Label: "   "},
		{Key: "asfalto", Label: "  Asfalto  ", Description: " buracos "},
	}.Normalize()

	if len(ts) != 3 { // saude-publica, asfalto, other
		t.Fatalf("expected 3 topics, got %+v", ts)
	}
	if ts[0].Key != "saude-publica" || ts[0].Label != "Saúde Pública" {
		t.Errorf("key should derive from label and first wins: %+v", ts[0])
	}
	if ts[1].Label != "Asfalto" || ts[1].Description != "buracos" {
		t.Errorf("fields should be trimmed: %+v", ts[1])
	}
}

func TestTopicSet_Validate(t *testing.T) {
	if err := DefaultTopicsFor(VerticalGov).Validate(); err != nil {
		t.Fatalf("default set should validate: %v", err)
	}
	empty := TopicSet{}.Normalize()
	if err := empty.Validate(); err != nil {
		t.Fatalf("a set of just other is valid: %v", err)
	}
	var big TopicSet
	for i := 0; i < MaxTopics+1; i++ {
		big = append(big, Topic{Key: NormalizeTopicKey("t" + string(rune('a'+i%26)) + string(rune('a'+i/26))), Label: "x"})
	}
	if err := big.Normalize().Validate(); !errors.Is(err, ErrTooManyTopics) {
		t.Fatalf("expected ErrTooManyTopics, got %v", err)
	}
	unnormalised := TopicSet{{Key: "Saúde", Label: "Saúde"}}
	if err := unnormalised.Validate(); !errors.Is(err, ErrTopicKeyInvalid) {
		t.Fatalf("an unnormalised key must be rejected, got %v", err)
	}
	long := TopicSet{{Key: "x", Label: string(make([]rune, MaxTopicLabelRunes+1))}}
	if err := long.Validate(); !errors.Is(err, ErrTopicLabelTooLong) {
		t.Fatalf("expected ErrTopicLabelTooLong, got %v", err)
	}
}

// Resolve accepts the key, the label, or any spelling that folds to either.
func TestTopicSet_Resolve(t *testing.T) {
	ts := DefaultTopicsFor(VerticalGov)
	for _, in := range []string{"saude", "Saúde", "SAUDE", " saude "} {
		key, ok := ts.Resolve(in)
		if !ok || key != "saude" {
			t.Errorf("Resolve(%q) = %q,%v", in, key, ok)
		}
	}
	if _, ok := ts.Resolve("blockchain"); ok {
		t.Error("unknown topic must not resolve")
	}
	if _, ok := ts.Resolve(""); ok {
		t.Error("empty must not resolve")
	}
}

func TestDefaultTopicsFor(t *testing.T) {
	for _, v := range []Vertical{VerticalGov, VerticalRetail, VerticalServices} {
		ts := DefaultTopicsFor(v)
		if len(ts) < 4 {
			t.Errorf("%s: too few default topics (%d)", v, len(ts))
		}
		if !ts.Has(TopicKeyOther) {
			t.Errorf("%s: missing other", v)
		}
		if err := ts.Validate(); err != nil {
			t.Errorf("%s: %v", v, err)
		}
		for _, tp := range ts {
			if tp.Key != NormalizeTopicKey(tp.Key) {
				t.Errorf("%s: key %q is not normalised", v, tp.Key)
			}
		}
	}
	// An unknown vertical still yields a usable set rather than nothing.
	if ts := DefaultTopicsFor("nope"); !ts.Has(TopicKeyOther) {
		t.Error("unknown vertical must fall back to a valid set")
	}
}

func TestVertical_Valid(t *testing.T) {
	for _, v := range []Vertical{VerticalGov, VerticalRetail, VerticalServices} {
		if !v.Valid() {
			t.Errorf("%q should be valid", v)
		}
	}
	if Vertical("Gov").Valid() || Vertical("").Valid() {
		t.Error("unknown vertical should be invalid")
	}
}
