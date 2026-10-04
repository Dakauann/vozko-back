package advertising

import (
	"errors"
	"strconv"
	"testing"
)

func issueOf(err error) string {
	var invalid *ValidationError
	if !errors.As(err, &invalid) || len(invalid.Issues) != 1 {
		return ""
	}
	return invalid.Issues[0].Field + ":" + invalid.Issues[0].Code
}

func TestBulkTargetsAreBoundedAndUnique(t *testing.T) {
	many := make([]string, MaxBulkObjects+1)
	for i := range many {
		many[i] = strconv.Itoa(i)
	}
	cases := map[string][]string{
		"metaIds:required": nil,
		"metaIds:too_many": many,
		"metaIds:invalid":  {"1", "1"},
	}
	for want, ids := range cases {
		if got := issueOf(ValidateBulkTargets(ids)); got != want {
			t.Errorf("want %s got %s", want, got)
		}
	}
	if err := ValidateBulkTargets([]string{"1", "2"}); err != nil {
		t.Fatal(err)
	}
}

func TestBulkChangeValidation(t *testing.T) {
	cases := map[string]BulkChange{
		"change.field:invalid":  {Field: "budget", Mode: BulkSet, Value: "x"},
		"change.mode:invalid":   {Field: BulkName, Mode: "append"},
		"change.value:required": {Field: BulkName, Mode: BulkSet, Value: "  "},
		"change.find:required":  {Field: BulkHeadline, Mode: BulkReplace},
	}
	for want, change := range cases {
		if got := issueOf(change.Validate()); got != want {
			t.Errorf("want %s got %s", want, got)
		}
	}
	if err := (BulkChange{Field: BulkDescription, Mode: BulkSet}).Validate(); err != nil {
		t.Fatalf("clearing a description is allowed: %v", err)
	}
}

func TestBulkReplaceHonoursCase(t *testing.T) {
	insensitive := BulkChange{Mode: BulkReplace, Find: "promo", Replace: "Oferta"}
	if got := insensitive.Apply("PROMO de Promo $1"); got != "Oferta de Oferta $1" {
		t.Fatalf("got %q", got)
	}
	sensitive := BulkChange{Mode: BulkReplace, Find: "Promo", Replace: "Oferta", MatchCase: true}
	if got := sensitive.Apply("PROMO de Promo"); got != "PROMO de Oferta" {
		t.Fatalf("got %q", got)
	}
}

func TestBulkRenameBuildsANameEdit(t *testing.T) {
	detail := ObjectDetail{Object: &Object{Level: LevelCampaign, Name: "Promo Natal"}}
	edit, err := BulkChange{Field: BulkName, Mode: BulkReplace, Find: "Natal", Replace: "Ano Novo"}.EditFor(detail)
	if err != nil || *edit.Name != "Promo Ano Novo" || edit.Creative != nil {
		t.Fatalf("got %+v %v", edit, err)
	}
	if _, err := (BulkChange{Field: BulkName, Mode: BulkReplace, Find: "Páscoa"}).EditFor(detail); !errors.Is(err, ErrNothingToChange) {
		t.Fatalf("got %v", err)
	}
}

func TestBulkCreativeTextOnlyOnAds(t *testing.T) {
	change := BulkChange{Field: BulkPrimaryText, Mode: BulkSet, Value: "Novo texto"}
	if _, err := change.EditFor(ObjectDetail{Object: &Object{Level: LevelAdSet}}); !errors.Is(err, ErrEditNotForLevel) {
		t.Fatalf("got %v", err)
	}
	post := ObjectDetail{Object: &Object{Level: LevelAd}, Creative: &CreativeDraft{Format: FormatExistingPost}}
	if got := issueOf(mustFail(change.EditFor(post))); got != "change.field:not_for_existing_post" {
		t.Fatalf("got %s", got)
	}
}

func mustFail(_ ObjectEdit, err error) error { return err }

func TestBulkCreativeTextChangesEveryVariantWithoutTouchingTheOriginal(t *testing.T) {
	original := &CreativeDraft{Format: FormatFlexible, PrimaryText: "Promo hoje", Texts: []string{"Promo A", "Outro"}}
	detail := ObjectDetail{Object: &Object{Level: LevelAd}, Creative: original}
	edit, err := BulkChange{Field: BulkPrimaryText, Mode: BulkReplace, Find: "Promo", Replace: "Oferta"}.EditFor(detail)
	if err != nil {
		t.Fatal(err)
	}
	if edit.Creative.PrimaryText != "Oferta hoje" || edit.Creative.Texts[0] != "Oferta A" || edit.Creative.Texts[1] != "Outro" {
		t.Fatalf("got %+v", edit.Creative)
	}
	if original.Texts[0] != "Promo A" {
		t.Fatal("the current creative must not change")
	}
}

func TestBulkChangeReadsTheCurrentValueOfItsField(t *testing.T) {
	detail := ObjectDetail{
		Object:   &Object{Name: "Anúncio v1", Level: LevelAd},
		Creative: &CreativeDraft{PrimaryText: "Texto", Headline: "Título", Description: "Descrição", Link: "https://vozkoia.com"},
	}
	cases := map[BulkField]string{BulkName: "Anúncio v1", BulkPrimaryText: "Texto", BulkHeadline: "Título", BulkDescription: "Descrição", BulkLink: "https://vozkoia.com"}
	for field, want := range cases {
		if got := (BulkChange{Field: field}).Current(detail); got != want {
			t.Fatalf("%s: %q", field, got)
		}
	}
	if got := (BulkChange{Field: BulkHeadline}).Current(ObjectDetail{Object: &Object{Level: LevelAdSet}}); got != "" {
		t.Fatalf("an ad set has no headline: %q", got)
	}
}
