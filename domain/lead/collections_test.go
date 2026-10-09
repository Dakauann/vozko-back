package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/recordevent"
)

var collectionsNow = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

func TestNewTakesPhonesAndAddresses(t *testing.T) {
	l, err := New("ws-1", Draft{
		Name:      "Maria",
		Phones:    []ContactPhone{{Number: "(11) 3333-4444", Label: PhoneLandline}},
		Addresses: []AddressInput{{Label: AddressHome, Postal: homePostal}},
	}, collectionsNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(l.Phones) != 1 || l.Phones[0].Number != "551133334444" {
		t.Fatalf("phones = %+v", l.Phones)
	}
	if len(l.Addresses) != 1 || !l.Addresses[0].Primary || l.Addresses[0].GeoStatus != GeoPending {
		t.Fatalf("addresses = %+v", l.Addresses)
	}
	if l.Relations == nil || len(l.Relations) != 0 {
		t.Fatalf("a new lead starts with an empty, loaded list of relations, got %#v", l.Relations)
	}
}

func TestNewRefusesAContactPhoneThatRepeatsTheIdentity(t *testing.T) {
	_, err := New("ws-1", Draft{Number: "5511987654321", Phones: []ContactPhone{{Number: "551187654321", Label: PhoneMobile}}}, collectionsNow)
	if !errors.Is(err, ErrPhoneRepeatsIdentity) {
		t.Fatalf("New = %v, want %v", err, ErrPhoneRepeatsIdentity)
	}
}

func TestApplyEditReplacesTheListsThatWereSent(t *testing.T) {
	l := &Lead{WorkspaceID: "ws-1", Name: "Maria",
		Phones:    []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneLandline}},
		Addresses: []Address{storedHome(&cepFix, GeoApproximate)}}

	if err := l.ApplyEdit(Edit{Name: text("Maria Souza")}, collectionsNow); err != nil {
		t.Fatal(err)
	}
	if len(l.Phones) != 1 || len(l.Addresses) != 1 {
		t.Fatalf("lists that were not sent are kept, got %+v %+v", l.Phones, l.Addresses)
	}

	phones := []ContactPhone{}
	addresses := []AddressInput{{ID: "a-1", Label: AddressHome, Postal: homePostal}, {Label: AddressWork, Primary: true, Postal: workPostal}}
	if err := l.ApplyEdit(Edit{Phones: &phones, Addresses: &addresses}, collectionsNow); err != nil {
		t.Fatal(err)
	}
	if len(l.Phones) != 0 || len(l.Addresses) != 2 || l.Addresses[0].Fix == nil || !l.Addresses[1].Primary {
		t.Fatalf("phones = %+v addresses = %+v", l.Phones, l.Addresses)
	}
}

func TestApplyEditRefusesABadListAndChangesNothing(t *testing.T) {
	l := &Lead{WorkspaceID: "ws-1", Name: "Maria"}
	phones := []ContactPhone{{Number: "123", Label: PhoneMobile}}
	if err := l.ApplyEdit(Edit{Name: text("Ana"), Phones: &phones}, collectionsNow); !errors.Is(err, ErrPhoneInvalid) {
		t.Fatalf("ApplyEdit = %v", err)
	}
	if l.Name != "Maria" {
		t.Fatalf("a refused edit changes nothing, got %+v", l)
	}
}

func TestEditFieldsNameTheListsAndTheNumber(t *testing.T) {
	phones, addresses, number := []ContactPhone{}, []AddressInput{}, ""
	e := Edit{Number: &number, Phones: &phones, Addresses: &addresses}
	if got := e.Fields(); !reflect.DeepEqual(got, []string{FieldNumber, FieldPhones, FieldAddresses}) {
		t.Fatalf("Fields = %v", got)
	}
}

func TestRecordFieldsDiffPhonesAndAddressesButNotTheirPositions(t *testing.T) {
	before := &Lead{Name: "Maria", Addresses: []Address{storedHome(nil, GeoPending)}}
	after := *before
	after.Phones = []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}
	located := storedHome(&cepFix, GeoApproximate)
	after.Addresses = []Address{located}

	event := Changes(EventUpdated, "u-1", before, &after, nil)
	if got := event.Fields(); !reflect.DeepEqual(got, []string{FieldPhones}) {
		t.Fatalf("changed fields = %v, want only phones: a position is not an edit of the address", got)
	}
}

func TestVisibleToProjectsTheSentLists(t *testing.T) {
	l := &Lead{ID: "l-1", Name: "Maria", Phones: []ContactPhone{{Number: "551133334444"}}, Addresses: []Address{storedHome(nil, GeoPending)},
		Relations: []Relation{{ID: "r-1", LeadID: "l-1", OtherLeadID: "l-2", Kind: KindChild}}}
	got := l.VisibleTo(Viewer{}, []string{FieldPhones})
	if len(got.Phones) != 1 || got.Addresses != nil || got.Relations != nil || got.Name != "" {
		t.Fatalf("only the sent list is visible, got %+v", got)
	}
	got = l.VisibleTo(Viewer{}, []string{FieldAddresses, FieldRelations})
	if got.Phones != nil || len(got.Addresses) != 1 || len(got.Relations) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestRelationEvents(t *testing.T) {
	r := Relation{ID: "r-1", LeadID: "maria", OtherLeadID: "joao", Kind: KindChild}
	added := RelationEvent(EventRelationAdded, "u-1", "joao", r)
	want := []recordevent.Change{{Field: FieldRelations, Before: nil, After: map[string]any{"leadId": "maria", "kind": "parent"}}}
	if added.Kind != EventRelationAdded || added.Actor != "u-1" || !reflect.DeepEqual(added.Changes, want) {
		t.Fatalf("added = %#v", added)
	}
	removed := RelationEvent(EventRelationRemoved, "u-1", "maria", r)
	if removed.Changes[0].After != nil || !reflect.DeepEqual(removed.Changes[0].Before, map[string]any{"leadId": "joao", "kind": "child"}) {
		t.Fatalf("removed = %#v", removed)
	}
}
