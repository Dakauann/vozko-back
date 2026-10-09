package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestVisibleToAReaderIsTheWholeRecord(t *testing.T) {
	l := &Lead{ID: "l-1", Number: "5511987654321", Name: "Ana", Email: "ana@x.com", Owner: "u-1", Version: 3}
	got := l.VisibleTo(Viewer{ReadsLeads: true}, nil)
	if !reflect.DeepEqual(got, l) || got == l {
		t.Fatalf("a reader sees a copy of the whole record, got %+v", got)
	}
}

func TestVisibleToSomeoneWhoCannotReadLeadsIsOnlyWhatTheySent(t *testing.T) {
	l := &Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana", Email: "ana@x.com", Owner: "u-1", Blocked: true, Version: 3}
	got := l.VisibleTo(Viewer{}, []string{FieldName})
	if got.ID != "l-1" || got.Version != 3 || got.Name != "Ana" {
		t.Fatalf("the sent field, the id and the version stay: %+v", got)
	}
	if got.Number != "" || got.Email != "" || got.Owner != "" || got.Blocked {
		t.Fatalf("fields the caller did not send leaked: %+v", got)
	}
}

func TestVersionConflictCarriesTheCurrentRecordAndIsAConflict(t *testing.T) {
	err := error(&VersionConflict{Current: &Lead{ID: "l-1", Version: 4}})
	if !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatal("a version conflict must match shared.ErrVersionConflict")
	}
	var conflict *VersionConflict
	if !errors.As(err, &conflict) || conflict.Current.Version != 4 {
		t.Fatal("the conflict must carry the current record")
	}
}

func TestEditFieldsNamesWhatWasSent(t *testing.T) {
	yes := true
	e := Edit{Name: text("Ana"), BirthDate: text(""), WhatsAppOptIn: &yes}
	if got := e.Fields(); !reflect.DeepEqual(got, []string{FieldName, FieldBirthDate, FieldWhatsAppOptIn}) {
		t.Fatalf("Fields = %v", got)
	}
}

func TestACreationRecordsOnlyTheFieldsThatWereFilled(t *testing.T) {
	l, err := New("ws-1", Draft{Name: "Maria"}, time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	event := Changes(EventCreated, "u-1", nil, l, nil)
	if got := event.Fields(); !reflect.DeepEqual(got, []string{FieldName}) {
		t.Fatalf("created fields = %v", got)
	}
}
