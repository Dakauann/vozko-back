package database

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The demotion repair decides which funnel a production workspace keeps, and no
// sqlmock test can check that: the whole question is which row a window function
// elects over real data. So this runs the actual statement against a real
// Postgres, over fixtures shaped like the five workspaces that were broken.
//
// Everything happens inside a transaction that is always rolled back, so the
// development database is untouched.
//
// Opt-in: set VOZKO_TEST_DB=1 and the DB_* variables the application reads.

func repairTx(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	t.Cleanup(func() {
		tx.Rollback()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// The repair exists for databases that predate the constraint, so the
	// fixtures below have to be able to write the broken state the constraint
	// forbids. Dropping it INSIDE the transaction is safe: DDL is transactional
	// in Postgres, so the rollback puts it back. Skipping this would make these
	// tests pass only on a database where the migration has never run, which is
	// exactly the database they are least useful on.
	if err := tx.Exec(`DROP INDEX IF EXISTS ux_pipelines_default_per_object`).Error; err != nil {
		t.Fatalf("drop constraint for fixture: %v", err)
	}
	return tx
}

// seedFunnel inserts one funnel plus `staged` conversations sitting on a stage
// of it, which is the signal the repair ranks by.
func seedFunnel(t *testing.T, tx *gorm.DB, workspaceID, name string, isDefault bool, ageDays, staged int) string {
	t.Helper()
	pipelineID := uuid.New().String()
	if err := tx.Exec(`
		INSERT INTO pipelines (id, workspace_id, name, object_type, position, is_default, created_at, updated_at)
		VALUES (?, ?, ?, 'conversation', 0, ?, NOW() - make_interval(days => ?), NOW())`,
		pipelineID, workspaceID, name, isDefault, ageDays).Error; err != nil {
		t.Fatalf("seed funnel %s: %v", name, err)
	}
	if staged == 0 {
		return pipelineID
	}

	stageID := uuid.New().String()
	if err := tx.Exec(`
		INSERT INTO stages (id, workspace_id, pipeline_id, name, position, is_default, is_initial, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, false, true, NOW(), NOW())`,
		stageID, workspaceID, pipelineID, name+" stage").Error; err != nil {
		t.Fatalf("seed stage for %s: %v", name, err)
	}
	for i := 0; i < staged; i++ {
		if err := tx.Exec(`
			INSERT INTO entry_stages (id, stage_id, entry_id, entry_type, workspace_id, created_at)
			VALUES (?, ?, ?, 'whatsapp', ?, NOW())`,
			uuid.New().String(), stageID, uuid.New().String(), workspaceID).Error; err != nil {
			t.Fatalf("seed entry_stage for %s: %v", name, err)
		}
	}
	return pipelineID
}

func isDefaultNow(t *testing.T, tx *gorm.DB, pipelineID string) bool {
	t.Helper()
	var flag bool
	if err := tx.Raw(`SELECT is_default FROM pipelines WHERE id = ?`, pipelineID).Scan(&flag).Error; err != nil {
		t.Fatalf("read is_default: %v", err)
	}
	return flag
}

func defaultCount(t *testing.T, tx *gorm.DB, workspaceID string) int64 {
	t.Helper()
	var n int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM pipelines
		WHERE workspace_id = ? AND object_type = 'conversation'
		  AND is_default = true AND deleted_at IS NULL`, workspaceID).Scan(&n).Error; err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	return n
}

// The UniFecaf shape, which is the one that prompted this work: an older funnel
// the operators renamed "NÃO USAR" holding a little history, and the funnel they
// actually work in holding more. The busy one has to win, or the CRM keeps
// resolving the dead funnel and the stage filter keeps returning nothing.
func TestDemoteDuplicateDefaults_BusiestFunnelSurvives(t *testing.T) {
	tx := repairTx(t)
	ws := uuid.New().String()

	naoUsar := seedFunnel(t, tx, ws, "NÃO USAR", true, 60, 115)
	real := seedFunnel(t, tx, ws, "FUNIL UNIFECAF", true, 30, 345)

	if got := defaultCount(t, tx, ws); got != 2 {
		t.Fatalf("fixture should start broken with 2 defaults, got %d", got)
	}

	if err := demoteDuplicateDefaultPipelines(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}

	if !isDefaultNow(t, tx, real) {
		t.Error("the funnel holding the conversations lost its default flag")
	}
	if isDefaultNow(t, tx, naoUsar) {
		t.Error("the dead funnel is still default")
	}
	if got := defaultCount(t, tx, ws); got != 1 {
		t.Fatalf("defaults after repair = %d, want exactly 1", got)
	}
}

// Age decides only when usage cannot. Two funnels with identical history must
// still produce a deterministic winner, or the repair elects a different funnel
// on every boot and the workspace's default flaps.
func TestDemoteDuplicateDefaults_TieBreaksOnAge(t *testing.T) {
	tx := repairTx(t)
	ws := uuid.New().String()

	older := seedFunnel(t, tx, ws, "primeiro", true, 90, 7)
	newer := seedFunnel(t, tx, ws, "segundo", true, 10, 7)

	if err := demoteDuplicateDefaultPipelines(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !isDefaultNow(t, tx, older) || isDefaultNow(t, tx, newer) {
		t.Fatalf("tie should elect the older funnel; older=%v newer=%v",
			isDefaultNow(t, tx, older), isDefaultNow(t, tx, newer))
	}

	// Idempotent: running again must not hand the flag to the other funnel.
	if err := demoteDuplicateDefaultPipelines(tx); err != nil {
		t.Fatalf("second repair: %v", err)
	}
	if !isDefaultNow(t, tx, older) {
		t.Error("a second run moved the default")
	}
}

// The Anhanguera shape: a workspace legitimately has one default conversation
// funnel AND one default sales funnel. Collapsing across object kinds would
// leave the opportunity board with no default at all.
func TestDemoteDuplicateDefaults_KeepsOneDefaultPerObjectKind(t *testing.T) {
	tx := repairTx(t)
	ws := uuid.New().String()

	conv := seedFunnel(t, tx, ws, "Atendimento", true, 60, 2231)
	seedFunnel(t, tx, ws, "Funil Anhanguera", true, 55, 1729)

	sales := uuid.New().String()
	if err := tx.Exec(`
		INSERT INTO pipelines (id, workspace_id, name, object_type, position, is_default, created_at, updated_at)
		VALUES (?, ?, 'Vendas', 'opportunity', 0, true, NOW(), NOW())`, sales, ws).Error; err != nil {
		t.Fatalf("seed sales funnel: %v", err)
	}

	if err := demoteDuplicateDefaultPipelines(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}

	if !isDefaultNow(t, tx, conv) {
		t.Error("the busiest conversation funnel should have survived")
	}
	if !isDefaultNow(t, tx, sales) {
		t.Error("the sales funnel was demoted by a conversation-funnel repair")
	}
}

// This runs on every boot, forever. On a database that never had the defect it
// must touch nothing at all.
func TestDemoteDuplicateDefaults_HealthyWorkspaceIsUntouched(t *testing.T) {
	tx := repairTx(t)
	ws := uuid.New().String()

	only := seedFunnel(t, tx, ws, "Atendimento", true, 40, 12)
	other := seedFunnel(t, tx, ws, "suporte", false, 20, 400)

	if err := demoteDuplicateDefaultPipelines(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !isDefaultNow(t, tx, only) {
		t.Error("the sole default was demoted")
	}
	// Busier, but never default. The repair ranks only among rows that already
	// hold the flag; it must never PROMOTE anything.
	if isDefaultNow(t, tx, other) {
		t.Error("the repair promoted a funnel that was not default")
	}
}

// A soft-deleted funnel is not a default anybody can reach, and counting it
// would demote a live one in its favour.
func TestDemoteDuplicateDefaults_IgnoresDeletedFunnels(t *testing.T) {
	tx := repairTx(t)
	ws := uuid.New().String()

	live := seedFunnel(t, tx, ws, "viva", true, 10, 5)
	dead := seedFunnel(t, tx, ws, "removida", true, 90, 900)
	if err := tx.Exec(`UPDATE pipelines SET deleted_at = NOW() WHERE id = ?`, dead).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if err := demoteDuplicateDefaultPipelines(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !isDefaultNow(t, tx, live) {
		t.Error("the live funnel was demoted in favour of a deleted one")
	}
}

// The constraint is the real guarantee, and it can only be built once the rows
// stop breaking it. This pins the ordering RunMigrations relies on.
func TestDefaultPipelineIndexBuildsAfterTheRepair(t *testing.T) {
	tx := repairTx(t)
	ws := uuid.New().String()

	seedFunnel(t, tx, ws, "a", true, 30, 10)
	seedFunnel(t, tx, ws, "b", true, 20, 3)

	const create = `CREATE UNIQUE INDEX ux_test_default_per_object
		ON pipelines (workspace_id, object_type)
		WHERE is_default AND deleted_at IS NULL`

	if err := tx.Exec(create).Error; err == nil {
		t.Fatal("the index should not build while duplicate defaults exist")
	}
	// Postgres aborts the transaction on that failure, so the ordering claim is
	// verified in a fresh one below rather than by continuing here.
	tx.Rollback()

	tx2 := repairTx(t)
	ws2 := uuid.New().String()
	kept := seedFunnel(t, tx2, ws2, "a", true, 30, 10)
	seedFunnel(t, tx2, ws2, "b", true, 20, 3)

	if err := demoteDuplicateDefaultPipelines(tx2); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := tx2.Exec(create).Error; err != nil {
		t.Fatalf("the index must build once the duplicates are demoted: %v", err)
	}
	if !isDefaultNow(t, tx2, kept) {
		t.Error("wrong funnel survived")
	}
}
