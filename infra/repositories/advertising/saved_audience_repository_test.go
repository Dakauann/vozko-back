package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

func savedAudience() *advertising.SavedAudience {
	return &advertising.SavedAudience{
		WorkspaceID: "ws",
		Name:        "Mulheres SP",
		Targeting:   advertising.Targeting{AgeMin: 25, AgeMax: 40},
		Placements:  advertising.Placements{Automatic: true},
		CreatedBy:   "u-1",
	}
}

func TestSavedAudienceCreateAssignsTheID(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ad_saved_audiences"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	s := savedAudience()
	if err := NewSavedAudienceRepository(db).Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.ID == "" || s.CreatedAt.IsZero() {
		t.Fatalf("got %+v", s)
	}
	expectationsMet(t, mock)
}

func TestSavedAudienceCreateRequiresWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	s := savedAudience()
	s.WorkspaceID = ""
	if err := NewSavedAudienceRepository(db).Create(context.Background(), s); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestSavedAudienceUpdateIsScopedToTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`UPDATE "ad_saved_audiences" SET "name"=\$1,"placements"=\$2,"targeting"=\$3,"updated_at"=\$4 WHERE id = \$5 AND workspace_id = \$6`).
		WithArgs("Mulheres SP", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "sa-1", "ws").
		WillReturnResult(sqlmock.NewResult(0, 0))
	s := savedAudience()
	s.ID = "sa-1"
	if err := NewSavedAudienceRepository(db).Update(context.Background(), s); !errors.Is(err, advertising.ErrSavedAudienceNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestSavedAudienceDeleteIsScopedToTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ad_saved_audiences" WHERE workspace_id = $1 AND id = $2`)).
		WithArgs("ws", "sa-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewSavedAudienceRepository(db).Delete(context.Background(), "ws", "sa-1"); !errors.Is(err, advertising.ErrSavedAudienceNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestSavedAudienceFindMapsTheJSON(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_saved_audiences" WHERE workspace_id = $1 AND id = $2`)).
		WithArgs("ws", "sa-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "targeting", "placements", "created_by"}).
			AddRow("sa-1", "ws", "Mulheres SP", []byte(`{"ageMin":25,"ageMax":40}`), []byte(`{"automatic":true}`), "u-1"))
	s, err := NewSavedAudienceRepository(db).Find(context.Background(), "ws", "sa-1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Mulheres SP" || s.Targeting.AgeMin != 25 || s.Targeting.AgeMax != 40 || !s.Placements.Automatic || s.CreatedBy != "u-1" {
		t.Fatalf("got %+v", s)
	}
}

func TestSavedAudienceFindMissingIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ad_saved_audiences"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewSavedAudienceRepository(db).Find(context.Background(), "ws", "sa-x"); !errors.Is(err, advertising.ErrSavedAudienceNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestSavedAudienceListIsScopedAndOrderedByName(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_saved_audiences" WHERE workspace_id = $1 ORDER BY name, id`)).
		WithArgs("ws").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sa-1").AddRow("sa-2"))
	list, err := NewSavedAudienceRepository(db).List(context.Background(), "ws")
	if err != nil || len(list) != 2 {
		t.Fatalf("got %v, %v", list, err)
	}
}

func TestSavedAudienceReadsRequireWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSavedAudienceRepository(db)
	if _, err := repo.List(context.Background(), " "); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("list: %v", err)
	}
	if _, err := repo.Find(context.Background(), "", "sa-1"); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("find: %v", err)
	}
	if err := repo.Delete(context.Background(), "", "sa-1"); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("delete: %v", err)
	}
	expectationsMet(t, mock)
}
