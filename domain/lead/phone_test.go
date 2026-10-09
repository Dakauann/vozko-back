package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSetPhones(t *testing.T) {
	added := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	stored := []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneLandline, CreatedAt: added}}
	cases := []struct {
		name      string
		identity  string
		inputs    []ContactPhone
		want      []ContactPhone
		wantErr   error
		wantIndex int
	}{
		{
			name:   "numbers are parsed into the canonical form",
			inputs: []ContactPhone{{Number: "(11) 98765-4321", Label: PhoneMobile}},
			want:   []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}},
		},
		{
			name:   "an empty list removes every contact phone",
			inputs: []ContactPhone{},
			want:   []ContactPhone{},
		},
		{
			name:   "a kept phone keeps its id and the day it was added",
			inputs: []ContactPhone{{ID: "p-1", Number: "1133334444", Label: PhoneWork}},
			want:   []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneWork, CreatedAt: added}},
		},
		{
			name:   "a phone sent again without its id is the same phone",
			inputs: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}},
			want:   []ContactPhone{{ID: "p-1", Number: "551133334444", Label: PhoneLandline, CreatedAt: added}},
		},
		{
			name:      "an id that is not one of the lead's phones is refused",
			inputs:    []ContactPhone{{ID: "p-9", Number: "551133334444", Label: PhoneLandline}},
			wantErr:   ErrPhoneUnknown,
			wantIndex: 0,
		},
		{
			name:      "a number that is not Brazilian is refused",
			inputs:    []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}, {Number: "12345", Label: PhoneMobile}},
			wantErr:   ErrPhoneInvalid,
			wantIndex: 1,
		},
		{
			name:      "an unknown label is refused",
			inputs:    []ContactPhone{{Number: "5511987654321", Label: "fax"}},
			wantErr:   ErrPhoneLabelInvalid,
			wantIndex: 0,
		},
		{
			name:      "a missing label is refused",
			inputs:    []ContactPhone{{Number: "5511987654321"}},
			wantErr:   ErrPhoneLabelInvalid,
			wantIndex: 0,
		},
		{
			name:      "the WhatsApp number is not repeated as a contact phone",
			identity:  "5511987654321",
			inputs:    []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}},
			wantErr:   ErrPhoneRepeatsIdentity,
			wantIndex: 0,
		},
		{
			name:      "the WhatsApp number is not repeated in its other mobile format",
			identity:  "5511987654321",
			inputs:    []ContactPhone{{Number: "551187654321", Label: PhoneMobile}},
			wantErr:   ErrPhoneRepeatsIdentity,
			wantIndex: 0,
		},
		{
			name:      "the same phone is not listed twice",
			inputs:    []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}, {Number: "11 98765 4321", Label: PhoneWork}},
			wantErr:   ErrPhoneRepeated,
			wantIndex: 1,
		},
		{
			name:      "the same mobile is not listed twice in its two formats",
			inputs:    []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}, {Number: "551187654321", Label: PhoneWork}},
			wantErr:   ErrPhoneRepeated,
			wantIndex: 1,
		},
		{
			name: "six phones are the limit",
			inputs: []ContactPhone{
				{Number: "5511900000001", Label: PhoneMobile}, {Number: "5511900000002", Label: PhoneMobile},
				{Number: "5511900000003", Label: PhoneMobile}, {Number: "5511900000004", Label: PhoneMobile},
				{Number: "5511900000005", Label: PhoneMobile}, {Number: "5511900000006", Label: PhoneMobile},
				{Number: "5511900000007", Label: PhoneMobile},
			},
			wantErr:   ErrPhoneLimit,
			wantIndex: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &Lead{WorkspaceID: "ws-1", Name: "Maria", Number: tc.identity, Phones: append([]ContactPhone(nil), stored...)}
			err := l.SetPhones(tc.inputs)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("SetPhones error = %v, want %v", err, tc.wantErr)
				}
				var item *ItemError
				if tc.wantIndex >= 0 && (!errors.As(err, &item) || item.Field != FieldPhones || item.Index != tc.wantIndex) {
					t.Fatalf("SetPhones error = %#v, want phones[%d]", err, tc.wantIndex)
				}
				if !reflect.DeepEqual(l.Phones, stored) {
					t.Fatalf("a refused change must leave the phones as they were, got %+v", l.Phones)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetPhones: %v", err)
			}
			if !reflect.DeepEqual(l.Phones, tc.want) {
				t.Fatalf("phones = %+v, want %+v", l.Phones, tc.want)
			}
		})
	}
}

func TestContactPhonesAreNotUniqueAcrossLeads(t *testing.T) {
	home := []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}
	for _, name := range []string{"Maria", "João"} {
		l := &Lead{WorkspaceID: "ws-1", Name: name}
		if err := l.SetPhones(home); err != nil {
			t.Fatalf("%s shares the home phone: %v", name, err)
		}
	}
}

func TestNumbersListsTheIdentityThenTheContactPhones(t *testing.T) {
	l := &Lead{Number: "5511987654321", Phones: []ContactPhone{{Number: "551133334444"}, {Number: "5511912345678"}}}
	want := []string{"5511987654321", "551133334444", "5511912345678"}
	if got := l.Numbers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Numbers = %v, want %v", got, want)
	}
	if !l.HoldsNumber("551112345678") {
		t.Fatal("a contact mobile is held in its other format too")
	}
	if l.HoldsNumber("5511900000000") {
		t.Fatal("a number the lead does not have is not held")
	}
	if (&Lead{}).HoldsNumber("") {
		t.Fatal("an empty number is never held")
	}
}

func TestValidateRecordChecksTheContactPhones(t *testing.T) {
	l := &Lead{WorkspaceID: "ws-1", Number: "5511987654321", Phones: []ContactPhone{{Number: "551187654321", Label: PhoneMobile}}}
	if err := l.ValidateRecord(); !errors.Is(err, ErrPhoneRepeatsIdentity) {
		t.Fatalf("ValidateRecord = %v, want %v", err, ErrPhoneRepeatsIdentity)
	}
}

func TestHoldsIdentity(t *testing.T) {
	l := &Lead{Number: "5511987654321", Phones: []ContactPhone{{Number: "551133334444"}}}
	for number, want := range map[string]bool{"5511987654321": true, "551187654321": true, "551133334444": false, "": false} {
		if got := l.HoldsIdentity(number); got != want {
			t.Errorf("HoldsIdentity(%q) = %v, want %v", number, got, want)
		}
	}
	if (&Lead{Phones: []ContactPhone{{Number: "551133334444"}}}).HoldsIdentity("551133334444") {
		t.Error("a lead without identity holds no identity")
	}
}
