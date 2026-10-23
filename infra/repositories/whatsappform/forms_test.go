package whatsappform_repository

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/whatsappform"
	"vozko/infra/database/schema"
)

type jsonArg struct{ want string }

func (a jsonArg) Match(v driver.Value) bool {
	text, ok := v.(string)
	if !ok {
		return false
	}
	var got, want interface{}
	if json.Unmarshal([]byte(text), &got) != nil || json.Unmarshal([]byte(a.want), &want) != nil {
		return false
	}
	return reflect.DeepEqual(got, want)
}

func sampleForm() whatsappform.Form {
	return whatsappform.Form{
		ID: formID, WorkspaceID: wsID, Name: "Pesquisa", Description: "Satisfação", Kind: whatsappform.KindBuilt,
		DraftVersionID: versionID, CreatedBy: userID, CreatedAt: at, UpdatedAt: at,
	}
}

func sampleVersion(id string, number int) whatsappform.Version {
	return whatsappform.Version{
		ID: id, WorkspaceID: wsID, FormID: formID, Number: number,
		Document: whatsappform.Document{Schema: 1, Screens: []whatsappform.Screen{{Key: "s_one", Title: "Início"}}},
		Hash:     "h1", CreatedBy: userID, CreatedAt: at,
	}
}

const sampleDocumentJSON = `{"schema":1,"categories":null,"screens":[{"key":"s_one","title":"Início","blocks":null,"route":{},"submitLabel":""}]}`

func expectVersionInsert(mock sqlmock.Sqlmock, id string, number int) {
	mock.ExpectExec(exactly(insertVersionSQL)).
		WithArgs(id, wsID, formID, number, sqlmock.AnyArg(), nil, "", "", "h1", jsonArg{`[]`}, userID, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestFormCreateWritesTheFormAndItsFirstVersionInOneTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(exactly(insertFormSQL)).
		WithArgs(formID, wsID, "Pesquisa", "Satisfação", "built", versionID, nil, userID, at, at, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectVersionInsert(mock, versionID, 1)
	mock.ExpectCommit()
	if err := NewFormStore(db).Create(ctx, sampleForm(), sampleVersion(versionID, 1)); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestFormCreateTurnsTheLiveNameIndexIntoNameTaken(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(exactly(insertFormSQL)).WillReturnError(uniqueViolation(schema.WhatsAppFormLiveNameIndex))
	mock.ExpectRollback()
	if err := NewFormStore(db).Create(ctx, sampleForm(), sampleVersion(versionID, 1)); !errors.Is(err, whatsappform.ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
	expectationsMet(t, mock)
}

func TestFormCreateRefusesAFirstVersionThatIsNotTheDraft(t *testing.T) {
	cases := []struct {
		name    string
		version whatsappform.Version
	}{
		{"another version id", sampleVersion(nextID, 1)},
		{"another form", func() whatsappform.Version { v := sampleVersion(versionID, 1); v.FormID = nextID; return v }()},
		{"another workspace", func() whatsappform.Version { v := sampleVersion(versionID, 1); v.WorkspaceID = otherWsID; return v }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if err := NewFormStore(db).Create(ctx, sampleForm(), tc.version); err == nil {
				t.Fatal("expected a refusal")
			}
			expectationsMet(t, mock)
		})
	}
}

func formRowColumns() []string {
	return []string{"id", "workspace_id", "name", "description", "kind", "draft_version_id", "published_version_id", "created_by", "created_at", "updated_at", "archived_at"}
}

func TestFormGetReadsTheRowOfTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(exactly(getFormSQL)).WithArgs(wsID, formID).
		WillReturnRows(sqlmock.NewRows(formRowColumns()).AddRow(formID, wsID, "Pesquisa", "", "built", versionID, nextID, nil, at, at, nil))
	got, err := NewFormStore(db).Get(ctx, wsID, formID)
	if err != nil {
		t.Fatal(err)
	}
	want := whatsappform.Form{ID: formID, WorkspaceID: wsID, Name: "Pesquisa", Kind: whatsappform.KindBuilt, DraftVersionID: versionID, PublishedVersionID: nextID, CreatedAt: at, UpdatedAt: at}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
	expectationsMet(t, mock)
}

func TestFormGetMissesAreNotFound(t *testing.T) {
	cases := []struct {
		name   string
		formID string
		query  bool
	}{
		{"no row", formID, true},
		{"not a uuid", "f-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if tc.query {
				mock.ExpectQuery(exactly(getFormSQL)).WillReturnRows(sqlmock.NewRows(formRowColumns()))
			}
			if _, err := NewFormStore(db).Get(ctx, wsID, tc.formID); !errors.Is(err, whatsappform.ErrFormNotFound) {
				t.Fatalf("err = %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestFormListFiltersAndSummarises(t *testing.T) {
	cases := []struct {
		name  string
		query whatsappform.FormQuery
		sql   string
		args  []driver.Value
	}{
		{"active forms only", whatsappform.FormQuery{WorkspaceID: wsID}, listFormsSQL + listActiveFormsSQL + listFormsOrderSQL, []driver.Value{wsID}},
		{"archived included", whatsappform.FormQuery{WorkspaceID: wsID, IncludeArchived: true}, listFormsSQL + listFormsOrderSQL, []driver.Value{wsID}},
		{"search folds accents and escapes wildcards", whatsappform.FormQuery{WorkspaceID: wsID, Search: " 50%_off "}, listFormsSQL + listActiveFormsSQL + listFormsSearchSQL + listFormsOrderSQL, []driver.Value{wsID, `%50\%\_off%`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			columns := append(formRowColumns(), "publications", "sent", "responses", "last_response_at")
			mock.ExpectQuery(exactly(tc.sql)).WithArgs(tc.args...).
				WillReturnRows(sqlmock.NewRows(columns).AddRow(formID, wsID, "Pesquisa", "", "built", versionID, nil, nil, at, at, nil,
					`[{"metaWabaId":"waba-1","status":"published","versionId":"`+versionID+`"}]`, int64(4), int64(2), at))
			got, err := NewFormStore(db).List(ctx, tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Form.ID != formID || got[0].Sent != 4 || got[0].Responses != 2 || got[0].LastResponseAt == nil ||
				!reflect.DeepEqual(got[0].Publications, []whatsappform.PublicationBrief{{MetaWABAID: "waba-1", Status: whatsappform.PublicationPublished, VersionID: versionID}}) {
				t.Fatalf("got %+v", got)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestFormListOfAnInvalidWorkspaceIsEmpty(t *testing.T) {
	db, mock := newMockDB(t)
	got, err := NewFormStore(db).List(ctx, whatsappform.FormQuery{WorkspaceID: "ws"})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestFormSaveDraftMovesTheDraftOnlyFromTheBaseVersion(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"the base is still the draft", 1, nil},
		{"someone saved in between", 0, whatsappform.ErrStaleDraft},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectBegin()
			mock.ExpectExec(exactly(moveDraftSQL)).WithArgs(nextID, at, wsID, formID, versionID).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.affected == 1 {
				expectVersionInsert(mock, nextID, 2)
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := NewFormStore(db).SaveDraft(ctx, wsID, formID, versionID, sampleVersion(nextID, 2), at)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestFormSaveDraftTurnsATakenNumberIntoStale(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(exactly(moveDraftSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(exactly(insertVersionSQL)).WillReturnError(uniqueViolation(schema.WhatsAppFormVersionNumberIndex))
	mock.ExpectRollback()
	if err := NewFormStore(db).SaveDraft(ctx, wsID, formID, versionID, sampleVersion(nextID, 2), at); !errors.Is(err, whatsappform.ErrStaleDraft) {
		t.Fatalf("err = %v", err)
	}
	expectationsMet(t, mock)
}

func TestFormSaveDraftRefusesAVersionOfAnotherForm(t *testing.T) {
	db, mock := newMockDB(t)
	v := sampleVersion(nextID, 2)
	v.FormID = otherWsID
	if err := NewFormStore(db).SaveDraft(ctx, wsID, formID, versionID, v, at); err == nil {
		t.Fatal("expected a refusal")
	}
	expectationsMet(t, mock)
}

func TestFormRename(t *testing.T) {
	cases := []struct {
		name   string
		result func(*sqlmock.ExpectedExec)
		want   error
	}{
		{"renamed", func(e *sqlmock.ExpectedExec) { e.WillReturnResult(sqlmock.NewResult(0, 1)) }, nil},
		{"absent", func(e *sqlmock.ExpectedExec) { e.WillReturnResult(sqlmock.NewResult(0, 0)) }, whatsappform.ErrFormNotFound},
		{"name taken", func(e *sqlmock.ExpectedExec) { e.WillReturnError(uniqueViolation(schema.WhatsAppFormLiveNameIndex)) }, whatsappform.ErrNameTaken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			tc.result(mock.ExpectExec(exactly(renameFormSQL)).WithArgs("Novo", "d", at, wsID, formID))
			err := NewFormStore(db).Rename(ctx, wsID, formID, "Novo", "d", at)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestFormSetPublished(t *testing.T) {
	cases := []struct {
		name      string
		versionID string
		sql       string
		args      []driver.Value
		affected  int64
		want      error
	}{
		{"a version of the form", versionID, publishFormSQL, []driver.Value{versionID, at, wsID, formID, versionID}, 1, nil},
		{"a version the form does not own", versionID, publishFormSQL, []driver.Value{versionID, at, wsID, formID, versionID}, 0, whatsappform.ErrFormNotFound},
		{"no version clears it", "", unpublishFormSQL, []driver.Value{at, wsID, formID}, 1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(exactly(tc.sql)).WithArgs(tc.args...).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			err := NewFormStore(db).SetPublished(ctx, wsID, formID, tc.versionID, at)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestFormArchiveKeepsTheFirstArchiveTime(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"archived", 1, nil},
		{"absent", 0, whatsappform.ErrFormNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(exactly(archiveFormSQL)).WithArgs(at, at, wsID, formID).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			err := NewFormStore(db).Archive(ctx, wsID, formID, at)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}
