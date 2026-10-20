package whatsappform_repository

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/whatsappform"
)

const wabaID = "0b0e7c1a-0000-4000-8000-0000000000aa"

func publicationRowColumns() []string {
	return []string{"id", "workspace_id", "form_id", "waba_id", "meta_waba_id", "meta_flow_id", "meta_name", "version_id", "status", "problems", "last_error", "preview_url", "preview_expires_at", "published_at", "created_at", "updated_at"}
}

func addPublicationRow(rows *sqlmock.Rows, flowID interface{}, status string) *sqlmock.Rows {
	return rows.AddRow(pubID, wsID, formID, wabaID, "meta-waba", flowID, "Pesquisa · Vozko 0b0e7c1a", versionID, status, `[]`, "", "", nil, nil, at, at)
}

func samplePublication() whatsappform.Publication {
	return whatsappform.Publication{
		ID: pubID, WorkspaceID: wsID, FormID: formID, WABAID: wabaID, MetaWABAID: "meta-waba", MetaName: "Pesquisa · Vozko 0b0e7c1a",
		VersionID: versionID, Status: whatsappform.PublicationCreating, CreatedAt: at, UpdatedAt: at,
	}
}

func TestPublicationInsertWritesEveryColumn(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(exactly(insertPublicationSQL)).
		WithArgs(pubID, wsID, formID, wabaID, "meta-waba", nil, "Pesquisa · Vozko 0b0e7c1a", versionID, "creating", jsonArg{`[]`}, "", "", nil, nil, at, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewPublicationStore(db).Insert(ctx, samplePublication()); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestPublicationSaveUpdatesTheMutableColumnsWithinTheWorkspace(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"saved", 1, nil},
		{"absent", 0, whatsappform.ErrPublicationNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			p := samplePublication()
			expires := at.Add(time.Hour)
			p.MetaFlowID, p.Status, p.LastError, p.PreviewURL, p.PreviewExpiresAt, p.PublishedAt = "flow-1", whatsappform.PublicationPublished, "x", "https://p", &expires, &at
			p.Problems = whatsappform.Problems{{Code: "meta_validation", Block: "b_1"}}
			mock.ExpectExec(exactly(savePublicationSQL)).
				WithArgs(wabaID, "flow-1", p.MetaName, versionID, "published", jsonArg{`[{"block":"b_1","code":"meta_validation"}]`}, "x", "https://p", expires, at, at, wsID, pubID).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			err := NewPublicationStore(db).Save(ctx, p)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestPublicationFinds(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		args []driver.Value
		call func(*PublicationStore) (*whatsappform.Publication, error)
	}{
		{"by form and account", findPublicationSQL, []driver.Value{wsID, formID, "meta-waba"}, func(s *PublicationStore) (*whatsappform.Publication, error) {
			return s.Find(ctx, wsID, formID, "meta-waba")
		}},
		{"by Meta flow", findPublicationByFlowSQL, []driver.Value{"meta-waba", "flow-1"}, func(s *PublicationStore) (*whatsappform.Publication, error) {
			return s.FindByMetaFlow(ctx, "meta-waba", "flow-1")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+" found", func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(exactly(tc.sql)).WithArgs(tc.args...).WillReturnRows(addPublicationRow(sqlmock.NewRows(publicationRowColumns()), "flow-1", "published"))
			got, err := tc.call(NewPublicationStore(db))
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != pubID || got.WABAID != wabaID || got.MetaFlowID != "flow-1" || !got.Sendable() || got.Problems != nil {
				t.Fatalf("got %+v", got)
			}
			expectationsMet(t, mock)
		})
		t.Run(tc.name+" absent", func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(exactly(tc.sql)).WillReturnRows(sqlmock.NewRows(publicationRowColumns()))
			if _, err := tc.call(NewPublicationStore(db)); !errors.Is(err, whatsappform.ErrPublicationNotFound) {
				t.Fatalf("err = %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestPublicationFindByMetaFlowNeedsBothIDs(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewPublicationStore(db).FindByMetaFlow(ctx, "meta-waba", " "); !errors.Is(err, whatsappform.ErrPublicationNotFound) {
		t.Fatalf("err = %v", err)
	}
	expectationsMet(t, mock)
}

func TestPublicationListForForm(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(exactly(listPublicationsSQL)).WithArgs(wsID, formID).
		WillReturnRows(addPublicationRow(sqlmock.NewRows(publicationRowColumns()), nil, "creating"))
	got, err := NewPublicationStore(db).ListForForm(ctx, wsID, formID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MetaFlowID != "" || got[0].Status != whatsappform.PublicationCreating {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestPublicationListStuckIsOldestFirstAndLimited(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(exactly(listStuckPublicationsSQL)).WithArgs(at, 20).
		WillReturnRows(addPublicationRow(sqlmock.NewRows(publicationRowColumns()), nil, "creating"))
	got, err := NewPublicationStore(db).ListStuck(ctx, at, 20)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestPublicationListStuckWithoutRoomReadsNothing(t *testing.T) {
	db, mock := newMockDB(t)
	got, err := NewPublicationStore(db).ListStuck(ctx, at, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	expectationsMet(t, mock)
}
