package stage_usecase_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	pipeline_domain "vozko/domain/pipeline"
	pipeline_repository "vozko/infra/repositories/pipeline"
	stage_repository "vozko/infra/repositories/stage"
	stage_usecase "vozko/usecases/stage"
)

func funnelTx(t *testing.T) *gorm.DB {
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
	return tx
}

type conversationFunnels struct{ repo pipeline_domain.Repository }

func (a conversationFunnels) ListConversationFunnels(workspaceID string) ([]stage_usecase.Funnel, error) {
	rows, err := a.repo.ListByWorkspace(workspaceID, string(pipeline_domain.ObjectConversation))
	if err != nil {
		return nil, err
	}
	out := make([]stage_usecase.Funnel, 0, len(rows))
	for _, p := range rows {
		out = append(out, stage_usecase.Funnel{
			ID: p.ID, Name: p.Name, IsDefault: p.IsDefault, Position: p.Position,
		})
	}
	return out, nil
}

func seedPipeline(t *testing.T, tx *gorm.DB, ws, name string, position int, isDefault bool) string {
	t.Helper()
	id := uuid.New().String()
	if err := tx.Exec(`
		INSERT INTO pipelines (id, workspace_id, name, object_type, position, is_default, created_at, updated_at)
		VALUES (?, ?, ?, 'conversation', ?, ?, NOW(), NOW())`,
		id, ws, name, position, isDefault).Error; err != nil {
		t.Fatalf("seed pipeline %s: %v", name, err)
	}
	return id
}

func seedStage(t *testing.T, tx *gorm.DB, ws, pipelineID, name string, position int) string {
	t.Helper()
	id := uuid.New().String()
	if err := tx.Exec(`
		INSERT INTO stages (id, workspace_id, pipeline_id, name, position, is_default, is_initial, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, false, false, NOW(), NOW())`,
		id, ws, pipelineID, name, position).Error; err != nil {
		t.Fatalf("seed stage %s: %v", name, err)
	}
	return id
}

func TestListFunnelStagesAgainstPostgres(t *testing.T) {
	tx := funnelTx(t)
	ws := uuid.New().String()

	dead := seedPipeline(t, tx, ws, "NÃO USAR", 0, false)
	live := seedPipeline(t, tx, ws, "FUNIL UNIFECAF", 1, true)

	deadAgendamento := seedStage(t, tx, ws, dead, "agendamento", 1)
	liveInscricao := seedStage(t, tx, ws, live, "inscrição", 2)
	liveAgendamento := seedStage(t, tx, ws, live, "agendamento", 1)

	uc := stage_usecase.NewListFunnelStagesUseCase(
		stage_repository.NewRepository(tx),
		conversationFunnels{repo: pipeline_repository.NewRepository(tx)},
	)

	groups, err := uc.Execute(ws)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2: %+v", len(groups), groups)
	}

	if groups[0].PipelineName != "NÃO USAR" || groups[1].PipelineName != "FUNIL UNIFECAF" {
		t.Fatalf("funnel order = %q, %q", groups[0].PipelineName, groups[1].PipelineName)
	}
	if !groups[1].IsDefault {
		t.Error("the default flag did not survive the read")
	}

	if len(groups[0].Stages) != 1 || groups[0].Stages[0].ID != deadAgendamento {
		t.Errorf("dead funnel stages = %+v", groups[0].Stages)
	}
	if len(groups[1].Stages) != 2 {
		t.Fatalf("live funnel stages = %+v, want 2", groups[1].Stages)
	}
	if groups[1].Stages[0].ID != liveAgendamento || groups[1].Stages[1].ID != liveInscricao {
		t.Errorf("live funnel stage order = %s, %s; want agendamento then inscrição",
			groups[1].Stages[0].Name, groups[1].Stages[1].Name)
	}
	if groups[0].Stages[0].ID == groups[1].Stages[0].ID {
		t.Error("two funnels' 'agendamento' collapsed to one stage")
	}
}

func TestListFunnelStagesKeepsAnEmptyFunnelAgainstPostgres(t *testing.T) {
	tx := funnelTx(t)
	ws := uuid.New().String()

	full := seedPipeline(t, tx, ws, "cheio", 0, true)
	seedPipeline(t, tx, ws, "vazio", 1, false)
	seedStage(t, tx, ws, full, "novo lead", 1)

	groups, err := stage_usecase.NewListFunnelStagesUseCase(
		stage_repository.NewRepository(tx),
		conversationFunnels{repo: pipeline_repository.NewRepository(tx)},
	).Execute(ws)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want both funnels", len(groups))
	}
	if groups[1].PipelineName != "vazio" || len(groups[1].Stages) != 0 {
		t.Errorf("empty funnel = %+v", groups[1])
	}
	if groups[1].Stages == nil {
		t.Error("an empty funnel must serialize its stages as [], not null")
	}
}

func TestListFunnelStagesIsWorkspaceScopedAgainstPostgres(t *testing.T) {
	tx := funnelTx(t)
	mine := uuid.New().String()
	theirs := uuid.New().String()

	myPipe := seedPipeline(t, tx, mine, "meu funil", 0, true)
	seedStage(t, tx, mine, myPipe, "novo lead", 1)
	theirPipe := seedPipeline(t, tx, theirs, "funil alheio", 0, true)
	seedStage(t, tx, theirs, theirPipe, "novo lead", 1)

	groups, err := stage_usecase.NewListFunnelStagesUseCase(
		stage_repository.NewRepository(tx),
		conversationFunnels{repo: pipeline_repository.NewRepository(tx)},
	).Execute(mine)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(groups) != 1 || groups[0].PipelineName != "meu funil" {
		t.Fatalf("groups = %+v, want only this workspace's funnel", groups)
	}
}

func TestListFunnelStagesIgnoresLegacyCampaignClonesAgainstPostgres(t *testing.T) {
	tx := funnelTx(t)
	ws := uuid.New().String()

	live := seedPipeline(t, tx, ws, "Funil Anhanguera", 0, true)
	seedStage(t, tx, ws, live, "novo lead", 1)
	seedStage(t, tx, ws, live, "em atendimento", 2)

	for i := 0; i < 40; i++ {
		if err := tx.Exec(`
			INSERT INTO stages (id, workspace_id, campaign_id, name, position, is_default, is_initial, created_at, updated_at)
			VALUES (?, ?, ?, 'em atendimento', 1, false, false, NOW(), NOW())`,
			uuid.New().String(), ws, uuid.New().String()).Error; err != nil {
			t.Fatalf("seed legacy clone %d: %v", i, err)
		}
	}

	groups, err := stage_usecase.NewListFunnelStagesUseCase(
		stage_repository.NewRepository(tx),
		conversationFunnels{repo: pipeline_repository.NewRepository(tx)},
	).Execute(ws)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("groups = %d, want only the real funnel: %+v", len(groups), groups)
	}
	if len(groups[0].Stages) != 2 {
		t.Fatalf("stages = %d, want 2; the 40 clones must not be offered", len(groups[0].Stages))
	}
	total := 0
	for _, g := range groups {
		total += len(g.Stages)
	}
	if total != 2 {
		t.Fatalf("filter would render %d rows, want 2", total)
	}
}
