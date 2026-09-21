package unofficial_whatsapp_campaign

import (
	"errors"
	"strings"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func textSpec(bodies ...string) MessageSpec {
	m := MessageSpec{Kind: KindText, Bodies: bodies}
	m.Normalize()
	return m
}

func TestParameterCountIsTheHighestPlaceholder(t *testing.T) {
	cases := map[string]struct {
		body string
		want int
	}{
		"none":           {"bom dia", 0},
		"one":            {"bom dia {{1}}", 1},
		"gap":            {"oi {{1}}, seu pedido {{3}}", 3},
		"repeated":       {"{{1}} e {{1}} de novo", 1},
		"not a variable": {"use {{um}} assim", 0},
		"double digits":  {"{{12}}", 12},
	}
	for name, c := range cases {
		if got := textSpec(c.body).ParameterCount(); got != c.want {
			t.Errorf("%s: ParameterCount(%q) = %d, want %d", name, c.body, got, c.want)
		}
	}
}

func TestParameterCountSpansVariants(t *testing.T) {
	if got := textSpec("oi {{1}}", "ola {{1}} do {{2}}").ParameterCount(); got != 2 {
		t.Fatalf("ParameterCount = %d, want 2", got)
	}
}

func TestVariantsMustUseTheSamePlaceholders(t *testing.T) {
	if err := textSpec("oi {{1}}", "ola {{2}}").Validate(); !errors.Is(err, ErrMessageVariantMismatch) {
		t.Fatalf("err = %v, want ErrMessageVariantMismatch", err)
	}
	if err := textSpec("oi {{1}}", "ola {{1}}!").Validate(); err != nil {
		t.Fatalf("matching placeholders rejected: %v", err)
	}
	if err := textSpec("oi {{1}} {{2}}", "ola {{2}} {{1}}").Validate(); err != nil {
		t.Fatalf("same set in a different order rejected: %v", err)
	}
}

func TestValidateRejectsEmptyAndOverlongBodies(t *testing.T) {
	if err := (&MessageSpec{Kind: KindText}).Validate(); !errors.Is(err, ErrMessageBodyRequired) {
		t.Errorf("empty spec: want ErrMessageBodyRequired")
	}
	long := MessageSpec{Kind: KindText, Bodies: []string{strings.Repeat("a", uw.MaxTextRunes+1)}}
	if err := long.Validate(); !errors.Is(err, ErrMessageBodyTooLong) {
		t.Errorf("overlong body: want ErrMessageBodyTooLong, got %v", err)
	}
	exact := MessageSpec{Kind: KindText, Bodies: []string{strings.Repeat("a", uw.MaxTextRunes)}}
	if err := exact.Validate(); err != nil {
		t.Errorf("a body of exactly MaxTextRunes was rejected: %v", err)
	}
}

func TestMediaKindsNeedAnAttachment(t *testing.T) {
	for _, kind := range []MessageKind{KindImage, KindVideo, KindAudio, KindDocument} {
		spec := MessageSpec{Kind: kind, Bodies: []string{"legenda"}}
		if err := spec.Validate(); !errors.Is(err, ErrMessageMediaRequired) {
			t.Errorf("%s without media: want ErrMessageMediaRequired, got %v", kind, err)
		}
		spec.MediaID = "media-1"
		if err := spec.Validate(); err != nil {
			t.Errorf("%s with media rejected: %v", kind, err)
		}
	}
}

func TestMediaMayHaveNoCaption(t *testing.T) {
	spec := MessageSpec{Kind: KindImage, MediaID: "media-1"}
	spec.Normalize()
	if err := spec.Validate(); err != nil {
		t.Fatalf("captionless image rejected: %v", err)
	}
}

func TestAudioMapsToVoiceNote(t *testing.T) {
	if got := KindAudio.MediaKind(); got != uw.MediaVoice {
		t.Fatalf("KindAudio.MediaKind() = %q, want %q", got, uw.MediaVoice)
	}
}

func TestMenuLimitsComeFromTheChannel(t *testing.T) {
	opts := func(n int) []uw.InteractiveOption {
		out := make([]uw.InteractiveOption, n)
		for i := range out {
			out[i] = uw.InteractiveOption{ID: "opt", Title: "sim"}
		}
		return out
	}

	buttons := MessageSpec{Kind: KindMenu, Style: uw.InteractiveStyleButtons, Bodies: []string{"escolha"}}
	buttons.Options = opts(uw.MaxButtonOptions + 1)
	if err := buttons.Validate(); !errors.Is(err, ErrMenuOptionsTooMany) {
		t.Errorf("buttons over cap: want ErrMenuOptionsTooMany, got %v", err)
	}

	list := MessageSpec{Kind: KindMenu, Style: uw.InteractiveStyleList, Bodies: []string{"escolha"}}
	list.Options = opts(uw.MaxListOptions)
	if err := list.Validate(); err != nil {
		t.Errorf("list at cap rejected: %v", err)
	}
}

func TestMenuOptionsNeedIDs(t *testing.T) {
	spec := MessageSpec{
		Kind: KindMenu, Style: uw.InteractiveStyleButtons, Bodies: []string{"escolha"},
		Options: []uw.InteractiveOption{{Title: "sim"}},
	}
	if err := spec.Validate(); !errors.Is(err, ErrMenuOptionIDRequired) {
		t.Fatalf("err = %v, want ErrMenuOptionIDRequired", err)
	}
}

func TestMenuDefaultsToButtons(t *testing.T) {
	spec := MessageSpec{Kind: KindMenu}
	spec.Normalize()
	if spec.Style != uw.InteractiveStyleButtons {
		t.Fatalf("style = %q, want buttons", spec.Style)
	}
}

func TestRenderSubstitutesPositionally(t *testing.T) {
	spec := textSpec("Olá {{1}}, seu boleto de {{2}} venceu")
	got := spec.Render(0, []string{"Ana", "R$ 90"})
	if want := "Olá Ana, seu boleto de R$ 90 venceu"; got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRenderLeavesUnmatchedPlaceholders(t *testing.T) {
	got := textSpec("oi {{1}} e {{2}}").Render(0, []string{"Ana"})
	if want := "oi Ana e {{2}}"; got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRenderThenSanitizeLeavesNoLiveBraces(t *testing.T) {
	spec := textSpec("Olá {{1}}, use o cupom {{promo}} hoje")
	rendered := spec.Render(0, []string{"Ana"})
	if !strings.Contains(rendered, "{{promo}}") {
		t.Fatalf("expected the unknown placeholder to survive rendering: %q", rendered)
	}
	safe := uw.SanitizeOutboundText(rendered)
	if strings.Contains(safe, "{{") {
		t.Fatalf("a live {{ reached the wire: %q", safe)
	}
}

func TestVariantForIsStablePerEntry(t *testing.T) {
	spec := textSpec("a", "b", "c")
	first := spec.VariantFor("entry-42")
	for i := 0; i < 50; i++ {
		if got := spec.VariantFor("entry-42"); got != first {
			t.Fatalf("VariantFor drifted: %d then %d", first, got)
		}
	}
	if first < 0 || first >= 3 {
		t.Fatalf("VariantFor = %d, out of range", first)
	}
}

func TestVariantForSpreadsAcrossBodies(t *testing.T) {
	spec := textSpec("a", "b", "c")
	seen := map[int]bool{}
	for i := 0; i < 300; i++ {
		seen[spec.VariantFor(string(rune('a'+i%26))+string(rune('0'+i/26)))] = true
	}
	if len(seen) < 2 {
		t.Fatalf("variants did not spread; saw %v", seen)
	}
}

func TestSingleBodyAlwaysPicksVariantZero(t *testing.T) {
	spec := textSpec("so um corpo")
	if got := spec.VariantFor("anything"); got != 0 {
		t.Fatalf("VariantFor = %d, want 0", got)
	}
}

func TestTooManyVariantsRefused(t *testing.T) {
	bodies := make([]string, MaxBodyVariants+1)
	for i := range bodies {
		bodies[i] = "oi"
	}
	if err := textSpec(bodies...).Validate(); err == nil {
		t.Fatal("expected a refusal past MaxBodyVariants")
	}
}
