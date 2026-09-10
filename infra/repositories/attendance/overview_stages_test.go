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

// The whole point of this read: it must ride the temp table the overview has
// already built, not re-derive the scoped conversation set. Driving FROM
// tmp_att_msg means the entry_stages lookup rides
// idx_et_entry_type_ws (entry_id, entry_type, workspace_id) instead of scanning
// every stage assignment the workspace owns.
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

// Never re-read conversation_messages. Every timing widget on this page already
// reads the pre-aggregated table for exactly this reason; a stage panel that
// re-joined the message table would add a full scan to a page that spent real
// work removing five of them.
func TestOverviewStageTallies_DoesNotTouchConversationMessages(t *testing.T) {
	// Captured rather than pattern-matched: RE2 has no negative lookahead, and
	// "this table must not appear" is the assertion that matters here.
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

// AssignStage soft-deletes the previous row and inserts a new one, so a
// conversation can carry more than one live-looking assignment. Without the
// DISTINCT ON it would be counted once per row, in more than one stage at once.
func TestOverviewStageTallies_PicksOneLiveStagePerConversation(t *testing.T) {
	db, mock, sqlDB := newStageDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`DISTINCT ON \(es\.entry_id, es\.entry_type\)[\s\S]*` +
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

// Soft-deleted assignments and soft-deleted stages are both live rows in this
// schema. Counting either would report conversations in stages the workspace
// deleted, which is how a "ghost stage" appears on a dashboard.
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

// A stage with no pipeline is a real row (campaign-scoped stages predate
// funnels). An inner join would silently drop it and understate the workspace.
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

// The stuck threshold is per stage and comes from the row, with the product
// default bound as a parameter rather than written into the SQL text.
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

// Rows come back flat and are handed to the domain untouched; the repository
// does SQL, the shaping lives in domain/attendance.
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

// A stage that defines no rot_days and holds no open work must survive the scan
// as nils, not as zeros. Zero dwell would read as "everyone arrived today" and
// a zero threshold would mark the whole stage stuck.
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
