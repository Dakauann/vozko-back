package shared

import "testing"

// These pin the text handling that both campaigns and seeded conversations
// depend on. They live here rather than in either caller because the compiler
// forced the move: domain/unofficial_whatsapp_campaign imports
// domain/unofficial_whatsapp, so the second package cannot reach the first, and
// the choice was between copying this logic and lifting it somewhere both can
// see. The campaign tests that were written against the original are left
// untouched and still green, which is the proof the move changed no behaviour.

func TestPositionalParametersFindsOursAndIgnoresNamedOnes(t *testing.T) {
	got := PositionalParameters("oi {{1}}, aqui e a {{2}}")
	if len(got) != 2 {
		t.Fatalf("found %d parameters, want 2", len(got))
	}
	for _, n := range []int{1, 2} {
		if _, ok := got[n]; !ok {
			t.Errorf("{{%d}} was not found", n)
		}
	}

	// Named placeholders are the PROVIDER's syntax, substituted from its own
	// lead store. We never render them, so they are not ours to count.
	if len(PositionalParameters("use {{nome}} aqui")) != 0 {
		t.Error("a named placeholder was counted as one of ours")
	}
	if len(PositionalParameters("sem variaveis")) != 0 {
		t.Error("a body with no placeholders reported some")
	}
}

func TestPositionalParametersDeduplicates(t *testing.T) {
	got := PositionalParameters("{{1}} e {{1}} de novo")
	if len(got) != 1 {
		t.Fatalf("found %d parameters, want 1", len(got))
	}
}

func TestHighestPositionalParameterIsTheHighestNotTheCount(t *testing.T) {
	// A gap matters: {{1}} and {{3}} needs three columns, not two.
	if got := HighestPositionalParameter([]string{"oi {{1}} e {{3}}"}); got != 3 {
		t.Errorf("highest = %d, want 3", got)
	}
	// The maximum ACROSS variants, because the importer collects one set of
	// columns for the whole run: a recipient needs enough variables for
	// whichever variant they are assigned.
	if got := HighestPositionalParameter([]string{"oi {{1}}", "ola {{1}} do {{2}}"}); got != 2 {
		t.Errorf("highest across variants = %d, want 2", got)
	}
	if got := HighestPositionalParameter([]string{"bom dia"}); got != 0 {
		t.Errorf("highest with no placeholders = %d, want 0", got)
	}
	if got := HighestPositionalParameter(nil); got != 0 {
		t.Errorf("highest of nothing = %d, want 0", got)
	}
}

func TestPositionalParametersAgreeComparesSetsNotCounts(t *testing.T) {
	if !PositionalParametersAgree([]string{"so um corpo {{1}}"}) {
		t.Error("a single body should always agree with itself")
	}
	if !PositionalParametersAgree(nil) {
		t.Error("no bodies should agree")
	}
	if !PositionalParametersAgree([]string{"{{1}} {{2}}", "{{2}}, {{1}}"}) {
		t.Error("the same set in a different order should agree")
	}
	// The case the whole rule exists for: same COUNT, different SET. A variant
	// reading {{2}} beside one reading {{1}} sends a raw "{{2}}" to everyone
	// assigned the second, because only one column was collected.
	if PositionalParametersAgree([]string{"oi {{1}}", "oi {{2}}"}) {
		t.Error("{{1}} and {{2}} agreed; they use different variables")
	}
	if PositionalParametersAgree([]string{"oi {{1}}", "oi"}) {
		t.Error("a body with a variable agreed with one without")
	}
}

func TestRenderPositionalSubstitutesInOrder(t *testing.T) {
	if got := RenderPositional("oi {{1}}, do {{2}}", []string{"Ana", "Curso"}); got != "oi Ana, do Curso" {
		t.Errorf("render = %q", got)
	}
	// A placeholder with no matching variable is left AS WRITTEN rather than
	// blanked, so a misconfigured message is visibly wrong instead of silently
	// sending a sentence with a hole in it.
	if got := RenderPositional("oi {{1}} e {{5}}", []string{"Ana"}); got != "oi Ana e {{5}}" {
		t.Errorf("render with a missing variable = %q", got)
	}
	if got := RenderPositional("oi {{1}}", nil); got != "oi {{1}}" {
		t.Errorf("render with no variables = %q", got)
	}
	// Named placeholders survive untouched: the provider performs its OWN
	// {{...}} substitution from ITS lead store, and rewriting them here would
	// break that.
	if got := RenderPositional("oi {{nome}}", []string{"Ana"}); got != "oi {{nome}}" {
		t.Errorf("render of a named placeholder = %q", got)
	}
}

func TestVariantIndexForIsDeterministicAndInRange(t *testing.T) {
	// Deterministic in the key rather than random, and that is what makes the
	// feature debuggable: the same person always gets the same variant, so a
	// resumed or re-run job never gives one person two different texts.
	const key = "5511999999999"
	first := VariantIndexFor(key, 5)
	for i := 0; i < 50; i++ {
		if VariantIndexFor(key, 5) != first {
			t.Fatal("the same key produced two different variants")
		}
	}
	if first < 0 || first >= 5 {
		t.Fatalf("variant %d is outside [0,5)", first)
	}
	// One variant, or none, is index zero and never a division by zero.
	if got := VariantIndexFor(key, 1); got != 0 {
		t.Errorf("single variant index = %d, want 0", got)
	}
	if got := VariantIndexFor(key, 0); got != 0 {
		t.Errorf("zero variants index = %d, want 0", got)
	}
	if got := VariantIndexFor(key, -3); got != 0 {
		t.Errorf("negative variants index = %d, want 0", got)
	}
}

func TestVariantIndexForSpreadsAcrossBuckets(t *testing.T) {
	// The distribution only has to be even across a handful of buckets. This
	// asserts it is not constant, which is the failure that would send one text
	// to everyone and defeat the whole point of variants.
	seen := map[int]struct{}{}
	for _, key := range []string{
		"5511900000001", "5511900000002", "5511900000003", "5511900000004",
		"5511900000005", "5511900000006", "5511900000007", "5511900000008",
	} {
		seen[VariantIndexFor(key, 3)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("eight keys landed in %d bucket(s); the hash is not spreading", len(seen))
	}
}

func TestNonEmptyTrimmedDropsBlanksAndTrims(t *testing.T) {
	got := NonEmptyTrimmed([]string{"  oi  ", "", "   ", "\tola\n"})
	if len(got) != 2 || got[0] != "oi" || got[1] != "ola" {
		t.Fatalf("cleaned = %#v, want [oi ola]", got)
	}
	// Never nil: callers assign this straight back onto a slice field, and a nil
	// there marshals as null where an empty list is what the wire shape says.
	if out := NonEmptyTrimmed(nil); out == nil || len(out) != 0 {
		t.Fatalf("cleaning nothing = %#v, want an empty non-nil slice", out)
	}
}

func TestTruncateRunesNeverSplitsACharacter(t *testing.T) {
	// Bytes would cut this in half and write invalid UTF-8 into a message row.
	const accented = "ação"
	got, cut := TruncateRunes(accented, 3)
	if got != "açã" || !cut {
		t.Fatalf("TruncateRunes(%q, 3) = (%q, %v), want (\"açã\", true)", accented, got, cut)
	}

	if got, cut := TruncateRunes(accented, 10); got != accented || cut {
		t.Errorf("a short string was reported as cut: (%q, %v)", got, cut)
	}
	if got, cut := TruncateRunes(accented, len([]rune(accented))); got != accented || cut {
		t.Errorf("an exact-length string was cut: (%q, %v)", got, cut)
	}
	// A zero or negative cap yields nothing, and says so when there was
	// something to lose.
	if got, cut := TruncateRunes(accented, 0); got != "" || !cut {
		t.Errorf("TruncateRunes(%q, 0) = (%q, %v)", accented, got, cut)
	}
	if got, cut := TruncateRunes("", 0); got != "" || cut {
		t.Errorf("truncating nothing reported a cut: (%q, %v)", got, cut)
	}
}
