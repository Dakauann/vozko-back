package lead_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

func TestPlaceSuggestionsFoldThePrefixAndKeepTheirOwnMemo(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	if _, err := rig.sections.PlaceSuggestions(context.Background(), operator(), crmfilter.Filter{}, " Sto Antô"); err != nil {
		t.Fatalf("PlaceSuggestions() error = %v", err)
	}
	if _, err := rig.sections.PlaceSuggestions(context.Background(), operator(), crmfilter.Filter{}, "natal"); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.sections.Places(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(rig.reader.queries) != 3 || rig.reader.queries[0].PlacePrefix != "santo anto" || rig.reader.queries[2].PlacePrefix != "" {
		t.Fatalf("queries = %+v", rig.reader.queries)
	}
	if rig.memo.keys[0] == rig.memo.keys[1] || rig.memo.keys[0] == rig.memo.keys[2] {
		t.Fatal("each prefix and the plain section need their own memo")
	}
}

func TestPlaceSuggestionsRefuseAPrefixWithoutTwoLetters(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	_, err := rig.sections.PlaceSuggestions(context.Background(), operator(), crmfilter.Filter{}, "a")
	if !errors.Is(err, lead.ErrLeadSearchTooShort) || len(rig.reader.queries) != 0 {
		t.Fatalf("PlaceSuggestions(a) = %v after %d reads", err, len(rig.reader.queries))
	}
}

func TestPlaceSuggestionsNeedTheReadPermission(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{})
	if _, err := rig.sections.PlaceSuggestions(context.Background(), operator(), crmfilter.Filter{}, "natal"); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("PlaceSuggestions without leads:read = %v", err)
	}
}
