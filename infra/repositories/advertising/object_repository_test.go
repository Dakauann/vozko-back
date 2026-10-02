package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"vozko/domain/advertising"
)

func adSet(metaID string) *advertising.Object {
	return &advertising.Object{
		MetaID:          metaID,
		WorkspaceID:     "ws",
		AdAccountID:     "a-1",
		Level:           advertising.LevelAdSet,
		CampaignMetaID:  "c-1",
		Name:            "Conjunto " + metaID,
		Status:          advertising.StatusActive,
		EffectiveStatus: advertising.EffectiveActive,
		DailyBudget:     5000,
		Creative:        &advertising.Creative{Title: "Oferta"},
		Issues:          []advertising.Issue{{Code: 1, Summary: "s"}},
	}
}

func TestReplaceLevelUpsertsTheSetAndRemovesTheRestInOneTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "ad_objects" .* ON CONFLICT \("meta_id"\) DO UPDATE SET .*"synced_at"="excluded"\."synced_at".*"removed"=\$\d+`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_objects" SET "removed"=$1 WHERE (ad_account_id = $2 AND level = $3) AND meta_id NOT IN ($4,$5)`)).
		WithArgs(true, "a-1", "adset", "s-1", "s-2").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	err := NewObjectRepository(db).ReplaceLevel(context.Background(), "a-1", advertising.LevelAdSet,
		[]*advertising.Object{adSet("s-1"), adSet("s-2")}, at)
	if err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestReplaceLevelSyncNeverOverwritesBudgetChanges(t *testing.T) {
	db, mock := newMockDB(t)
	var insert string
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "ad_objects"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "ad_objects"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	db.Callback().Create().After("gorm:create").Register("capture_sql", func(tx *gorm.DB) {
		insert = tx.Statement.SQL.String()
	})
	if err := NewObjectRepository(db).ReplaceLevel(context.Background(), "a-1", advertising.LevelAdSet,
		[]*advertising.Object{adSet("s-1")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	update := insert[strings.Index(insert, "DO UPDATE SET"):]
	if strings.Contains(update, "budget_changes") {
		t.Fatalf("sync update touches budget_changes: %s", update)
	}
	for _, column := range []string{"status", "effective_status", "daily_budget", "name", "creative", "issues", "workspace_id", "special_category"} {
		if !strings.Contains(update, `"`+column+`"="excluded"."`+column+`"`) {
			t.Errorf("sync update misses %s: %s", column, update)
		}
	}
}

func TestReplaceLevelWithNoObjectsRemovesTheWholeLevel(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_objects" SET "removed"=$1 WHERE ad_account_id = $2 AND level = $3`)).
		WithArgs(true, "a-1", "ad").
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()
	if err := NewObjectRepository(db).ReplaceLevel(context.Background(), "a-1", advertising.LevelAd, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestReplaceLevelRejectsObjectsOfAnotherAccountOrLevel(t *testing.T) {
	db, mock := newMockDB(t)
	foreign := adSet("s-1")
	foreign.AdAccountID = "a-2"
	wrongLevel := adSet("s-2")
	wrongLevel.Level = advertising.LevelAd
	unscoped := adSet("s-3")
	unscoped.WorkspaceID = ""
	repo := NewObjectRepository(db)
	for _, object := range []*advertising.Object{foreign, wrongLevel, unscoped} {
		if err := repo.ReplaceLevel(context.Background(), "a-1", advertising.LevelAdSet, []*advertising.Object{object}, time.Now()); err == nil {
			t.Fatalf("accepted %+v", object)
		}
	}
	expectationsMet(t, mock)
}

func TestObjectUpsertPersistsBudgetChanges(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "ad_objects" .* ON CONFLICT \("meta_id"\) DO UPDATE SET .*"status"="excluded"\."status".*"budget_changes"="excluded"\."budget_changes"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	object := adSet("s-1")
	object.RecordBudgetChange(time.Now())
	if err := NewObjectRepository(db).Upsert(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestObjectFindIsScopedToTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_objects" WHERE workspace_id = $1 AND meta_id = $2`)).
		WithArgs("ws", "s-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}))
	if _, err := NewObjectRepository(db).Find(context.Background(), "ws", "s-1"); !errors.Is(err, advertising.ErrObjectNotFound) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestObjectFindMapsJSONColumns(t *testing.T) {
	db, mock := newMockDB(t)
	changed := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_objects"`)).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id", "level", "status", "special_category", "creative", "review_feedback", "issues", "budget_changes"}).
			AddRow("s-1", "adset", "PAUSED", "HOUSING", []byte(`{"title":"Oferta"}`), []byte(`{"global":"ok"}`),
				[]byte(`[{"code":7,"summary":"x"}]`), []byte(`["2026-10-01T10:00:00Z"]`)))
	o, err := NewObjectRepository(db).Find(context.Background(), "ws", "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if o.Level != advertising.LevelAdSet || o.Status != advertising.StatusPaused || o.Creative == nil || o.Creative.Title != "Oferta" ||
		o.ReviewFeedback["global"] != "ok" || o.Issues[0].Code != 7 || !o.BudgetChanges[0].Equal(changed) ||
		o.SpecialCategory != advertising.CategoryHousing {
		t.Fatalf("got %+v", o)
	}
}

func TestObjectListRequiresWorkspaceAndAccount(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewObjectRepository(db)
	for _, q := range []advertising.ObjectQuery{{AdAccountID: "a-1"}, {WorkspaceID: "ws"}} {
		if _, err := repo.List(context.Background(), q); err == nil {
			t.Fatalf("listed without scope: %+v", q)
		}
	}
	expectationsMet(t, mock)
}

func TestObjectListFiltersAndEscapesSearch(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_objects" WHERE (workspace_id = $1 AND ad_account_id = $2) AND level = $3 `+
		`AND campaign_meta_id IN ($4,$5) AND adset_meta_id IN ($6) AND name ILIKE $7 ESCAPE '\' AND removed = $8 `+
		`ORDER BY created_time DESC NULLS LAST, meta_id`)).
		WithArgs("ws", "a-1", "ad", "c-1", "c-2", "s-1", `%50\%\_off\\%`, false).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}).AddRow("ad-1"))
	objects, err := NewObjectRepository(db).List(context.Background(), advertising.ObjectQuery{
		WorkspaceID: "ws", AdAccountID: "a-1", Level: advertising.LevelAd,
		CampaignIDs: []string{"c-1", "c-2"}, AdSetIDs: []string{"s-1"}, Search: ` 50%_off\ `,
	})
	if err != nil || len(objects) != 1 {
		t.Fatalf("got %v, %v", objects, err)
	}
	expectationsMet(t, mock)
}

func TestObjectListByExactIDs(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_objects" WHERE (workspace_id = $1 AND ad_account_id = $2) AND level = $3 `+
		`AND meta_id IN ($4) AND removed = $5 ORDER BY`)).
		WithArgs("ws", "a-1", "adset", "s-1", false).
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}).AddRow("s-1"))
	objects, err := NewObjectRepository(db).List(context.Background(), advertising.ObjectQuery{
		WorkspaceID: "ws", AdAccountID: "a-1", Level: advertising.LevelAdSet, MetaIDs: []string{"s-1"},
	})
	if err != nil || len(objects) != 1 {
		t.Fatalf("got %v, %v", objects, err)
	}
	expectationsMet(t, mock)
}

func TestObjectListIncludingRemovedDropsTheFilter(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_objects" WHERE workspace_id = $1 AND ad_account_id = $2 ORDER BY`)).
		WithArgs("ws", "a-1").
		WillReturnRows(sqlmock.NewRows([]string{"meta_id"}))
	if _, err := NewObjectRepository(db).List(context.Background(), advertising.ObjectQuery{
		WorkspaceID: "ws", AdAccountID: "a-1", IncludeRemoved: true,
	}); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}
