package whatsappform_repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/whatsappform"
)

func versionRowColumns() []string {
	return []string{"id", "workspace_id", "form_id", "number", "document", "flow_json", "flow_version", "compiler_version", "hash", "problems", "created_by", "created_at"}
}

func TestVersionGetDecodesTheDocumentAndTheCompiledFlow(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(exactly(getVersionSQL)).WithArgs(wsID, versionID).
		WillReturnRows(sqlmock.NewRows(versionRowColumns()).
			AddRow(versionID, wsID, formID, 3, sampleDocumentJSON, `{"version":"7.3"}`, "7.3", "c1", "h1", `[{"code":"required","block":"b_1"}]`, userID, at))
	got, err := NewVersionStore(db).Get(ctx, wsID, versionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 3 || got.Document.Schema != 1 || len(got.Document.Screens) != 1 || got.Document.Screens[0].Key != "s_one" ||
		string(got.FlowJSON) != `{"version":"7.3"}` || got.FlowVersion != "7.3" || got.CompilerVersion != "c1" || got.Hash != "h1" ||
		len(got.Problems) != 1 || got.Problems[0].Block != "b_1" || got.CreatedBy != userID || !got.CreatedAt.Equal(at) {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestVersionGetMissesAreNotFound(t *testing.T) {
	cases := []struct {
		name      string
		versionID string
		query     bool
	}{
		{"no row", versionID, true},
		{"not a uuid", "v-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if tc.query {
				mock.ExpectQuery(exactly(getVersionSQL)).WillReturnRows(sqlmock.NewRows(versionRowColumns()))
			}
			if _, err := NewVersionStore(db).Get(ctx, wsID, tc.versionID); !errors.Is(err, whatsappform.ErrVersionNotFound) {
				t.Fatalf("err = %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestVersionListIsNewestFirstWithinTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(exactly(listVersionsSQL)).WithArgs(wsID, formID).
		WillReturnRows(sqlmock.NewRows(versionRowColumns()).
			AddRow(nextID, wsID, formID, 2, sampleDocumentJSON, nil, "", "", "", `[]`, nil, at).
			AddRow(versionID, wsID, formID, 1, sampleDocumentJSON, nil, "", "", "", `[]`, nil, at))
	got, err := NewVersionStore(db).List(ctx, wsID, formID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != nextID || got[1].Number != 1 || got[0].FlowJSON != nil || got[0].Compiled() {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}
