package emaildesign

import (
	"strings"
	"testing"
)

func TestSubjectGapsFollowTheSubjectRule(t *testing.T) {
	cases := map[string][]string{
		"Oferta para {{first_name|você}}":      nil,
		"   ":                                  {"subject_empty"},
		"Oferta {{first_name":                  {"merge_tag_malformed"},
		"Oferta {{shoe_size}}":                 {"unknown_merge_tag"},
		strings.Repeat("a", MaxSubjectRunes+1): {"subject_too_long"},
	}
	for subject, want := range cases {
		gaps := SubjectGaps(subject)
		if len(gaps) != len(want) {
			t.Fatalf("%q: gaps = %+v, want %v", subject, gaps, want)
		}
		for i, gap := range gaps {
			if gap.Code != want[i] || gap.Field != "subject" || gap.BlockID != "" {
				t.Fatalf("%q: gap %d = %+v, want %s on subject", subject, i, gap, want[i])
			}
		}
	}
}

func TestAnEmptySubjectIsADraftGapAndABrokenOneIsNot(t *testing.T) {
	if !(Issue{Code: "subject_empty", Field: "subject"}).DraftGap() {
		t.Fatal("an empty subject must block sending only, like an empty text")
	}
	for _, code := range []string{"subject_too_long", "subject_invalid_characters"} {
		if (Issue{Code: code, Field: "subject"}).DraftGap() {
			t.Fatalf("%s must make the content malformed", code)
		}
	}
}

func TestReviewLintsTheSubjectItIsGiven(t *testing.T) {
	doc := docWith(imageBlock("hero", "https://x.com/a.jpg", "Vitrine"))
	if review := Review(doc, "OFERTA IMPERDÍVEL HOJE"); !hasHint(review.Hints, "subject_all_caps", "") {
		t.Fatalf("hints = %+v", review.Hints)
	}
	if review := Review(doc, ""); hasHint(review.Hints, "subject_all_caps", "") {
		t.Fatalf("a design has no subject to lint: %+v", review.Hints)
	}
}
