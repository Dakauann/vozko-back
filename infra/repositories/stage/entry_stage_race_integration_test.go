package stage_repository

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/stage"
)

const (
	raceWorkspace = "5a018104-560d-4627-aba7-0ee0895fcf50"
	raceEntry     = "85d15c05-03c7-441d-b418-b190408d6cca"
	raceEntryType = "unofficial_whatsapp"
	initialStage  = "00000000-0000-0000-0000-00000000000a"
	movedStageB   = "00000000-0000-0000-0000-00000000000b"
	movedStageC   = "00000000-0000-0000-0000-00000000000c"
)

func isolatedStageDB(t *testing.T) *gorm.DB {
	t.Helper()
	admin := pipelineIntegrationDB(t)
	schemaName := "stage_race_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schemaName + " CASCADE") })

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable search_path=%s",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT"), schemaName)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(16)
	t.Cleanup(func() { sqlDB.Close() })

	for _, ddl := range []string{
		`CREATE TABLE stages (id text PRIMARY KEY, workspace_id uuid NOT NULL, name text NOT NULL, deleted_at timestamptz)`,
		`CREATE TABLE entry_stages (id text PRIMARY KEY, stage_id text NOT NULL REFERENCES stages(id), entry_id uuid NOT NULL,
			entry_type varchar(20) NOT NULL, workspace_id uuid NOT NULL, created_at timestamptz, deleted_at timestamptz)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	for id, name := range map[string]string{initialStage: "recebido", movedStageB: "qualificando", movedStageC: "agendado"} {
		if err := db.Exec(`INSERT INTO stages (id, workspace_id, name) VALUES (?, ?, ?)`, id, raceWorkspace, name).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func entryStage(stageID string) *stage.EntryStage {
	return &stage.EntryStage{ID: uuid.NewString(), StageID: stageID, EntryID: raceEntry, EntryType: raceEntryType, WorkspaceID: raceWorkspace}
}

func liveStages(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var ids []string
	if err := db.Raw(`SELECT stage_id FROM entry_stages WHERE entry_id = ? AND deleted_at IS NULL`, raceEntry).Scan(&ids).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestAMoveNeverLeavesTheConversationWithoutAStage(t *testing.T) {
	db := isolatedStageDB(t)
	repo := NewRepository(db)
	if err := repo.AssignStage(entryStage(movedStageB)); err != nil {
		t.Fatal(err)
	}

	for round := 0; round < 150; round++ {
		target := movedStageB
		if round%2 == 0 {
			target = movedStageC
		}
		moved := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-moved:
						return
					default:
					}
					if _, err := repo.AssignStageIfNone(entryStage(initialStage)); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		if err := repo.AssignStage(entryStage(target)); err != nil {
			t.Error(err)
		}
		close(moved)
		wg.Wait()

		live := liveStages(t, db)
		if len(live) != 1 || live[0] != target {
			t.Fatalf("round %d: the move to %s must win and stand alone, live stages = %v", round, target, live)
		}
	}
}

func TestTheInitialStageIsAssignedOnlyToAConversationWithoutOne(t *testing.T) {
	db := isolatedStageDB(t)
	repo := NewRepository(db)

	assigned, err := repo.AssignStageIfNone(entryStage(initialStage))
	if err != nil || !assigned {
		t.Fatalf("a conversation without a stage gets the initial one: assigned=%v err=%v", assigned, err)
	}
	if err := repo.AssignStage(entryStage(movedStageC)); err != nil {
		t.Fatal(err)
	}
	assigned, err = repo.AssignStageIfNone(entryStage(initialStage))
	if err != nil || assigned {
		t.Fatalf("a conversation with a stage keeps it: assigned=%v err=%v", assigned, err)
	}
	if live := liveStages(t, db); len(live) != 1 || live[0] != movedStageC {
		t.Fatalf("live stages = %v", live)
	}
}

func TestTwoMovesAtOnceLeaveExactlyOneStage(t *testing.T) {
	db := isolatedStageDB(t)
	repo := NewRepository(db)
	if err := repo.AssignStage(entryStage(movedStageB)); err != nil {
		t.Fatal(err)
	}

	for round := 0; round < 150; round++ {
		var wg sync.WaitGroup
		for _, target := range []string{movedStageB, movedStageC, movedStageB, movedStageC} {
			wg.Add(1)
			go func(target string) {
				defer wg.Done()
				if err := repo.AssignStage(entryStage(target)); err != nil {
					t.Error(err)
				}
			}(target)
		}
		wg.Wait()
		if live := liveStages(t, db); len(live) != 1 {
			t.Fatalf("round %d: live stages = %v", round, live)
		}
	}
}

func TestAnEmptyConversationGetsOneInitialStageUnderConcurrency(t *testing.T) {
	db := isolatedStageDB(t)
	repo := NewRepository(db)

	for round := 0; round < 100; round++ {
		if err := db.Exec(`UPDATE entry_stages SET deleted_at = now() WHERE deleted_at IS NULL`).Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := repo.AssignStageIfNone(entryStage(initialStage)); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if live := liveStages(t, db); len(live) != 1 {
			t.Fatalf("round %d: live stages = %v", round, live)
		}
	}
}
