package lead

import (
	"reflect"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestEveryErasureTargetIsNamedOnce(t *testing.T) {
	seen := map[ErasureTarget]bool{}
	for _, target := range ErasureTargets() {
		if target == "" || seen[target] {
			t.Fatalf("target %q is empty or repeated", target)
		}
		seen[target] = true
	}
	for _, required := range []ErasureTarget{
		ErasureRecord, ErasurePhones, ErasureAddresses, ErasureRelations, ErasureEvents, ErasureMemories,
		ErasureCampaignEntries, ErasureUnofficialCampaignEntries, ErasureCallNumbers, ErasureTemplateSendNumbers, ErasureCallListItems, ErasureGeocodeCache,
	} {
		if !seen[required] {
			t.Errorf("%q is not erased", required)
		}
	}
}

func TestNumberMasksCoverEveryWrittenFormOfEachNumber(t *testing.T) {
	l := &Lead{Number: "5511987654321", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	got := l.NumberMasks()
	want := []NumberMask{
		{Masked: "••••4321", Forms: []string{"5511987654321", "551187654321", "+5511987654321", "+551187654321"}, Identity: true},
		{Masked: "••••4444", Forms: []string{"551133334444", "+551133334444"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("masks = %+v, want %+v", got, want)
	}
}

func TestALeadWithoutNumbersHasNoMasks(t *testing.T) {
	if got := (&Lead{Name: "Ana"}).NumberMasks(); len(got) != 0 {
		t.Fatalf("masks = %+v", got)
	}
}

func TestTheAnonymizationEventCarriesNoValue(t *testing.T) {
	e := AnonymizationEvent("u-1")
	if e.Kind != EventAnonymized || e.Actor != "u-1" || len(e.Changes) != 1 {
		t.Fatalf("event = %+v", e)
	}
	if c := e.Changes[0]; c.Field != FieldAnonymized || c.Before != nil || c.After != true {
		t.Fatalf("change = %+v", c)
	}
}

func TestErasableMasksSpareTheContactPhonesOtherLeadsStillHold(t *testing.T) {
	l := &Lead{Number: "5511987654321", Phones: []ContactPhone{
		{Number: "551133334444", Label: PhoneLandline},
		{Number: "5511999991234", Label: PhoneMessage},
	}}
	masks := l.NumberMasks()
	cases := []struct {
		name         string
		heldByOthers []string
		want         []string
	}{
		{"nobody else holds them", nil, []string{"••••4321", "••••4444", "••••1234"}},
		{"the family landline stays with the others", []string{"+551133334444"}, []string{"••••4321", "••••1234"}},
		{"a parent's mobile in the other format stays", []string{"551199991234"}, []string{"••••4321", "••••4444"}},
		{"the WhatsApp identity is always the subject's", []string{"5511987654321"}, []string{"••••4321", "••••4444", "••••1234"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, m := range ErasableMasks(masks, tc.heldByOthers) {
				got = append(got, m.Masked)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("erasable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestContactFormsListOnlyTheContactPhones(t *testing.T) {
	l := &Lead{Number: "5511987654321", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	if got := ContactForms(l.NumberMasks()); !reflect.DeepEqual(got, []string{"551133334444", "+551133334444"}) {
		t.Fatalf("contact forms = %v", got)
	}
}

func TestAnonymizedKeepsNoPersonalField(t *testing.T) {
	birth := shared.DateOf(time.Date(1990, 4, 21, 0, 0, 0, 0, time.UTC))
	age := 34
	by := "u-1"
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	l := &Lead{
		ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Maria", NameSource: SourceManual, Nickname: "Mari",
		Email: "maria@exemplo.com.br", BirthDate: &birth, Source: SourceImport, Owner: "u-2",
		CustomFields: map[string]any{"classificacao": "apoiador"}, WhatsAppOptIn: &Consent{GrantedAt: now, Source: ConsentForm},
		OptedOutAt: &now, Blocked: true, BlockedAt: now, BlockedBy: &by, ProfilePictureURL: "https://x/p.jpg", StoredAge: &age,
		Phones:         []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneLandline}},
		Addresses:      []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal.Normalize()}},
		Relations:      []Relation{{ID: "r-1", LeadID: "l-1", OtherLeadID: "l-2", Kind: KindChild}},
		RelativesCount: 1, ReferredCount: 2, Version: 7, CreatedAt: now,
	}
	got := l.Anonymized()
	if fields := got.RecordFields(); len(fields) != 0 {
		t.Fatalf("an anonymized lead still records %v", fields)
	}
	if got.ID != "l-1" || got.WorkspaceID != "ws-1" || got.Version != 7 || !got.CreatedAt.Equal(now) {
		t.Fatalf("the tombstone keeps who it was, got %+v", got)
	}
	if got.StoredAge != nil || got.ProfilePictureURL != "" || got.NameSource != "" || got.Source != "" ||
		got.RelativesCount != 0 || got.ReferredCount != 0 || !got.IsAggregate() || len(got.Relations) != 0 {
		t.Fatalf("anonymized = %+v", got)
	}
	if l.Name != "Maria" || len(l.Phones) != 1 {
		t.Fatal("Anonymized must not change the lead it was built from")
	}
}
