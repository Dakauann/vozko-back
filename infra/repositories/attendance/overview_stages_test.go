package attendance_repository

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/attendance"
)

func newStageDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func stageTallyRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"funnel_id", "funnel_name", "funnel_is_default",
		"stage_id", "stage_name", "color", "position", "is_won", "is_lost", "rot_days",
		"engaged", "shell", "finished", "ongoing", "pending",
		"avg_open_days", "oldest_open_days", "stuck",
	})
}

func TestOverviewStageTallies_DrivesFromTheMessageTempTable(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM tmp_att_msg_x m[\s\S]*JOIN stages s`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows())

	if _, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewStageTallies_DoesNotTouchConversationMessages(t *testing.T) {
	var captured string
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(
		sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
			captured = actualSQL
			return nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("").WillReturnRows(stageTallyRows())
	if _, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(captured, "conversation_messages") {
		t.Fatal("the stage read must go through the pre-aggregated temp table, " +
			"not add a sixth scan of conversation_messages")
	}
	if !strings.Contains(captured, "tmp_att_msg_x") {
		t.Fatalf("expected the read to drive from the temp table, got: %s", captured)
	}
}

func TestOverviewStageTallies_PicksOneLiveStagePerConversation(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`DISTINCT ON \(es\.entry_id, es\.entry_type\)[\s\S]*`+
		`ORDER BY es\.entry_id, es\.entry_type, es\.created_at DESC`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows())

	if _, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewStageTallies_ExcludesSoftDeletedAssignmentsAndStages(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`es\.deleted_at IS NULL[\s\S]*s\.deleted_at IS NULL`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows())

	if _, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewStageTallies_KeepsStagesWithNoPipeline(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`LEFT JOIN pipelines p ON p\.id = s\.pipeline_id`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows())

	if _, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewStageTallies_BindsTheDefaultStuckThreshold(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`COALESCE\(s\.rot_days, \$?\d*\??\)`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows())

	if _, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewStageTallies_MapsRowsToDomainTallies(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM tmp_att_msg_x m`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows().AddRow(
			"f1", "FUNIL UNIFECAF", true,
			"s1", "Inscrição", "#00D09A", 2, false, false, 12,
			int64(600), int64(40), int64(100), int64(400), int64(100),
			3.25, 41.0, int64(87),
		))

	got, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one tally, got %d", len(got))
	}
	tl := got[0]
	if tl.FunnelID != "f1" || tl.FunnelName != "FUNIL UNIFECAF" || !tl.FunnelIsDefault {
		t.Fatalf("funnel identity lost: %+v", tl)
	}
	if tl.StageID != "s1" || tl.StageName != "Inscrição" || tl.Color != "#00D09A" || tl.Position != 2 {
		t.Fatalf("stage identity lost: %+v", tl)
	}
	if tl.RotDays == nil || *tl.RotDays != 12 {
		t.Fatalf("expected the stage's own 12-day threshold, got %v", tl.RotDays)
	}
	if tl.Engaged != 600 || tl.Shell != 40 || tl.Stuck != 87 {
		t.Fatalf("counts lost: %+v", tl)
	}
	if tl.AvgOpenDays == nil || *tl.AvgOpenDays != 3.25 {
		t.Fatalf("expected dwell 3.25, got %v", tl.AvgOpenDays)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewStageTallies_NullDwellAndThresholdStayNil(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM tmp_att_msg_x m`).
		WithArgs("ws-1", attendance.DefaultStageStuckDays).
		WillReturnRows(stageTallyRows().AddRow(
			"", "", false,
			"s9", "Solto", "", 0, false, false, nil,
			int64(3), int64(0), int64(3), int64(0), int64(0),
			nil, nil, int64(0),
		))

	got, err := overviewStageTalliesTX(db, "ws-1", "tmp_att_msg_x")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].RotDays != nil || got[0].AvgOpenDays != nil || got[0].OldestOpenDays != nil {
		t.Fatalf("nulls must survive as nil: %+v", got[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
