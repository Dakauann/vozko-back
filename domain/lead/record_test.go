package lead

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

var recordNow = time.Date(2026, time.October, 8, 15, 0, 0, 0, time.UTC)

func text(s string) *string { return &s }

func TestNewLead(t *testing.T) {
	cases := []struct {
		name       string
		draft      Draft
		wantErr    error
		wantNumber string
	}{
		{"a name without a number", Draft{Name: "Maria"}, nil, ""},
		{"a number without a name", Draft{Number: "(11) 98765-4321"}, nil, "5511987654321"},
		{"neither a name nor a number", Draft{Nickname: "Mari"}, ErrLeadIdentityRequired, ""},
		{"a number that is not a phone", Draft{Name: "Maria", Number: "12345"}, ErrLeadInvalid, ""},
		{"an invalid e-mail", Draft{Name: "Maria", Email: "maria@"}, ErrLeadEmailInvalid, ""},
		{"a birth date in the future", Draft{Name: "Maria", BirthDate: "2026-10-09"}, ErrLeadBirthDateInvalid, ""},
		{"a birth date in an impossible format", Draft{Name: "Maria", BirthDate: "09/10/1990"}, ErrLeadBirthDateInvalid, ""},
		{"a name that will not fit", Draft{Name: strings.Repeat("a", MaxLeadNameLength+1)}, ErrLeadNameTooLong, ""},
		{"a nickname that will not fit", Draft{Name: "Maria", Nickname: strings.Repeat("a", MaxLeadNameLength+1)}, ErrLeadNicknameTooLong, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := New("ws-1", tc.draft, recordNow)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && l.Number != tc.wantNumber {
				t.Fatalf("number = %q, want %q", l.Number, tc.wantNumber)
			}
		})
	}
}

func TestNewLeadIsAManualRecordAtVersionOne(t *testing.T) {
	optIn := true
	l, err := New("ws-1", Draft{Name: "  Maria   Souza ", Email: " Maria@Exemplo.com.BR ", BirthDate: "1990-04-21", WhatsAppOptIn: &optIn}, recordNow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if l.Name != "Maria Souza" || l.NameSource != SourceManual || l.Source != SourceManual || l.Version != 1 {
		t.Fatalf("unexpected lead %+v", l)
	}
	if l.Email != "maria@exemplo.com.br" {
		t.Fatalf("email = %q", l.Email)
	}
	if l.WhatsAppOptIn == nil || l.WhatsAppOptIn.Source != ConsentManual || !l.WhatsAppOptIn.GrantedAt.Equal(recordNow) {
		t.Fatalf("opt-in = %+v", l.WhatsAppOptIn)
	}
	if _, err := New("", Draft{Name: "Maria"}, recordNow); !errors.Is(err, ErrLeadWorkspaceRequired) {
		t.Fatalf("a lead needs a workspace, got %v", err)
	}
}

func TestApplyEditOnlyTouchesWhatWasSent(t *testing.T) {
	birth := shared.Date{Year: 1990, Month: time.April, Day: 21}
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana", NameSource: SourceChannel, Email: "ana@x.com", BirthDate: &birth, CustomFields: map[string]any{"cor": "azul"}}

	if err := l.ApplyEdit(Edit{Nickname: text("Aninha")}, recordNow); err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}
	if l.Name != "Ana" || l.NameSource != SourceChannel || l.Email != "ana@x.com" || l.BirthDate == nil || l.CustomFields["cor"] != "azul" {
		t.Fatalf("fields that were not sent changed: %+v", l)
	}
	if l.Nickname != "Aninha" {
		t.Fatalf("nickname = %q", l.Nickname)
	}
}

func TestApplyEditMarksANewNameAsManual(t *testing.T) {
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana", NameSource: SourceChannel}
	if err := l.ApplyEdit(Edit{Name: text("Ana Souza")}, recordNow); err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}
	if l.Name != "Ana Souza" || l.NameSource != SourceManual {
		t.Fatalf("name = %q (%q)", l.Name, l.NameSource)
	}
}

func TestApplyEditClearsWithAnEmptyValue(t *testing.T) {
	birth := shared.Date{Year: 1990, Month: time.April, Day: 21}
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana", Email: "ana@x.com", BirthDate: &birth}
	if err := l.ApplyEdit(Edit{Name: text(""), Email: text(""), BirthDate: text("")}, recordNow); err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}
	if l.Name != "" || l.Email != "" || l.BirthDate != nil {
		t.Fatalf("not cleared: %+v", l)
	}
}

func TestApplyEditRefusesToLeaveALeadWithoutNameAndNumber(t *testing.T) {
	l := Lead{WorkspaceID: "ws-1", Name: "Maria"}
	if err := l.ApplyEdit(Edit{Name: text("")}, recordNow); !errors.Is(err, ErrLeadIdentityRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyEditConsent(t *testing.T) {
	optedOut := recordNow.Add(-time.Hour)
	yes, no := true, false
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321", OptedOutAt: &optedOut}

	if err := l.ApplyEdit(Edit{WhatsAppOptIn: &yes}, recordNow); err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}
	if l.WhatsAppOptIn == nil || l.OptedOutAt != nil {
		t.Fatalf("an explicit consent re-grants and clears the opt-out: %+v", l)
	}
	granted := l.WhatsAppOptIn.GrantedAt
	if err := l.ApplyEdit(Edit{WhatsAppOptIn: &yes}, recordNow.Add(time.Hour)); err != nil || !l.WhatsAppOptIn.GrantedAt.Equal(granted) {
		t.Fatalf("granting again keeps the first grant: %+v", l.WhatsAppOptIn)
	}
	if err := l.ApplyEdit(Edit{WhatsAppOptIn: &no}, recordNow); err != nil || l.WhatsAppOptIn != nil {
		t.Fatalf("revoking clears the consent: %+v", l.WhatsAppOptIn)
	}
}

func TestBlockIsIdempotent(t *testing.T) {
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321"}
	if !l.Block("u-1", recordNow) || !l.Blocked || l.BlockedBy == nil || *l.BlockedBy != "u-1" || !l.BlockedAt.Equal(recordNow) {
		t.Fatalf("Block = %+v", l)
	}
	if l.Block("u-2", recordNow.Add(time.Hour)) {
		t.Fatal("blocking a blocked lead changes nothing")
	}
	if *l.BlockedBy != "u-1" || !l.BlockedAt.Equal(recordNow) {
		t.Fatal("blocking again must keep who blocked first")
	}
	if !l.Unblock() || l.Blocked || l.BlockedBy != nil || !l.BlockedAt.IsZero() {
		t.Fatalf("Unblock = %+v", l)
	}
	if l.Unblock() {
		t.Fatal("unblocking an unblocked lead changes nothing")
	}
}

func TestSetOwner(t *testing.T) {
	cases := []struct {
		name    string
		owner   string
		wantErr error
	}{
		{"a member", "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00", nil},
		{"an agent", "ai:4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00", nil},
		{"a workflow", "workflow:4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00", nil},
		{"nobody", "", nil},
		{"the system", "system", ErrLeadOwnerInvalid},
		{"a campaign", "campaign:4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00", ErrLeadOwnerInvalid},
		{"an agent without an id", "ai:", ErrLeadOwnerInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := Lead{WorkspaceID: "ws-1", Number: "5511987654321", Owner: "someone-else"}
			changed, err := l.SetOwner(tc.owner)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && (!changed || l.Owner != tc.owner) {
				t.Fatalf("owner = %q (changed %v)", l.Owner, changed)
			}
			if err != nil && l.Owner != "someone-else" {
				t.Fatal("a refused owner must leave the lead unchanged")
			}
		})
	}
	l := Lead{Owner: "u-1"}
	if changed, err := l.SetOwner("u-1"); err != nil || changed {
		t.Fatalf("setting the same owner changes nothing: %v %v", changed, err)
	}
}

func TestOptOut(t *testing.T) {
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321", WhatsAppOptIn: &Consent{GrantedAt: recordNow.Add(-time.Hour), Source: ConsentImport}}
	changed, err := l.OptOut(recordNow, OptOutLeadRequest)
	if err != nil || !changed || l.OptedOutAt == nil || !l.OptedOutAt.Equal(recordNow) || l.WhatsAppOptIn != nil || l.OptOutSource != OptOutLeadRequest {
		t.Fatalf("OptOut = %+v (changed %v, err %v)", l, changed, err)
	}
	if changed, err := l.OptOut(recordNow.Add(time.Hour), OptOutOperator); err != nil || changed || !l.OptedOutAt.Equal(recordNow) || l.OptOutSource != OptOutLeadRequest {
		t.Fatal("opting out twice keeps the first opt-out and its source")
	}
}

func TestOptOutRefusesAnUnknownSource(t *testing.T) {
	for _, source := range []OptOutSource{"", "rumour"} {
		l := Lead{WorkspaceID: "ws-1", Number: "5511987654321"}
		if changed, err := l.OptOut(recordNow, source); !errors.Is(err, ErrLeadOptOutSourceInvalid) || changed || l.OptedOutAt != nil {
			t.Fatalf("OptOut(%q) = (%v, %v), want the source refusal and no change", source, changed, err)
		}
	}
	if !IsInputRefusal(ErrLeadOptOutSourceInvalid) || ErrorCode(ErrLeadOptOutSourceInvalid) != "lead_opt_out_source_invalid" {
		t.Fatal("an unknown opt-out source is a coded input refusal")
	}
}

func TestOnlyAnExplicitConsentClearsTheOptOut(t *testing.T) {
	l := Lead{WorkspaceID: "ws-1", Number: "5511987654321"}
	if _, err := l.OptOut(recordNow, OptOutOperator); err != nil {
		t.Fatal(err)
	}
	name := "Ana"
	if err := l.ApplyEdit(Edit{Name: &name}, recordNow); err != nil || l.OptedOutAt == nil {
		t.Fatalf("an edit without consent kept the opt-out? opted %v err %v", l.OptedOutAt, err)
	}
	refused := false
	if err := l.ApplyEdit(Edit{WhatsAppOptIn: &refused}, recordNow); err != nil || l.OptedOutAt == nil {
		t.Fatalf("withdrawing consent must not clear the opt-out: opted %v err %v", l.OptedOutAt, err)
	}
	granted := true
	if err := l.ApplyEdit(Edit{WhatsAppOptIn: &granted}, recordNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if l.OptedOutAt != nil || l.OptOutSource != "" || l.WhatsAppOptIn == nil || l.WhatsAppOptIn.Source != ConsentManual {
		t.Fatalf("explicit consent = %+v, want the opt-out and its source cleared", l)
	}
}

func TestTheOptOutSourceIsRecorded(t *testing.T) {
	before := Lead{WorkspaceID: "ws-1", Number: "5511987654321"}
	after := before
	if _, err := after.OptOut(recordNow, OptOutLeadRequest); err != nil {
		t.Fatal(err)
	}
	fields := after.RecordFields()
	if fields[FieldOptedOut] != true || fields[FieldOptOutSource] != "lead_request" {
		t.Fatalf("RecordFields() = %v, want optedOut and its source", fields)
	}
	if _, recorded := before.RecordFields()[FieldOptOutSource]; recorded {
		t.Fatal("a lead that never opted out records no source")
	}
	visible := after.VisibleTo(Viewer{}, []string{FieldOptedOut})
	if visible.OptOutSource != OptOutLeadRequest || visible.OptedOutAt == nil {
		t.Fatalf("VisibleTo(optedOut) = %+v, want the opt-out with its source", visible)
	}
}

func TestAge(t *testing.T) {
	birth := shared.Date{Year: 1990, Month: time.April, Day: 21}
	stored := 50
	cases := []struct {
		name string
		lead Lead
		want *int
	}{
		{"from the birth date", Lead{BirthDate: &birth, StoredAge: &stored}, intPtr(36)},
		{"the stored age when there is no birth date", Lead{StoredAge: &stored}, intPtr(50)},
		{"unknown", Lead{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.lead.Age(recordNow)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("Age = %v, want %v", got, tc.want)
			}
		})
	}
}

func intPtr(v int) *int { return &v }

func TestHasIdentity(t *testing.T) {
	if (&Lead{Name: "Maria"}).HasIdentity() {
		t.Fatal("a lead without a number has no identity")
	}
	if !(&Lead{Number: "5511987654321"}).HasIdentity() {
		t.Fatal("a lead with a number has an identity")
	}
}

func TestRecordFieldsDiffIntoNamedChanges(t *testing.T) {
	birth := shared.Date{Year: 1990, Month: time.April, Day: 21}
	before := Lead{WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana"}
	after := before
	after.Name = "Ana Souza"
	after.BirthDate = &birth
	after.Block("u-1", recordNow)

	got := Changes(EventUpdated, "u-1", &before, &after, nil).Fields()
	if !reflect.DeepEqual(got, []string{FieldBirthDate, FieldBlocked, FieldName}) {
		t.Fatalf("changed fields = %v", got)
	}
}

func TestValidateEmail(t *testing.T) {
	for _, ok := range []string{"ana@exemplo.com.br", "a.b+c@x.io"} {
		if err := ValidateEmail(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"ana", "ana@", "@x.com", "Ana <ana@x.com>", "ana @x.com", "ana@x"} {
		if err := ValidateEmail(bad); !errors.Is(err, ErrLeadEmailInvalid) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestALegacyNumberDoesNotStopARenameOrABlock(t *testing.T) {
	legacy := Lead{WorkspaceID: "ws-1", Number: "abc", Name: "Ana"}
	if err := legacy.ApplyEdit(Edit{Name: text("Ana Souza")}, recordNow); err != nil || legacy.Name != "Ana Souza" {
		t.Fatalf("rename of a lead with a legacy number: %v, name %q", err, legacy.Name)
	}
	if !legacy.Block("u-1", recordNow) || legacy.ValidateRecord() != nil {
		t.Fatal("a lead with a legacy number must still be blockable")
	}
}
