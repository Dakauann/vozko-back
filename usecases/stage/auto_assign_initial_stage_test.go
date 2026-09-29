package stage_usecase

import (
	"testing"

	"vozko/domain/stage"
)

type initialStageRepo struct {
	stage.Repository
	current        *stage.EntryStage
	assignedIfNone []*stage.EntryStage
	overwrote      []*stage.EntryStage
	stageAppeared  bool
}

func (r *initialStageRepo) EnsureDefaultStagesForCampaign(string, string, string) error { return nil }
func (r *initialStageRepo) GetEntryStage(string, string, string) (*stage.EntryStage, error) {
	return r.current, nil
}
func (r *initialStageRepo) GetInitialStageForCampaign(string, string, string) (*stage.Stage, error) {
	return &stage.Stage{ID: "recebido", Name: "recebido"}, nil
}
func (r *initialStageRepo) AssignStageIfNone(et *stage.EntryStage) (bool, error) {
	r.assignedIfNone = append(r.assignedIfNone, et)
	return !r.stageAppeared, nil
}
func (r *initialStageRepo) AssignStage(et *stage.EntryStage) error {
	r.overwrote = append(r.overwrote, et)
	return nil
}

func TestAConversationWithoutAStageGetsTheInitialOne(t *testing.T) {
	repo := &initialStageRepo{}
	AutoAssignInitialStage(repo, "ws", "camp", "unofficial_whatsapp", "entry", "unofficial_whatsapp")
	if len(repo.assignedIfNone) != 1 || repo.assignedIfNone[0].StageID != "recebido" {
		t.Fatalf("assigned %+v", repo.assignedIfNone)
	}
}

func TestAStageWrittenAfterTheLookupIsNeverOverwritten(t *testing.T) {
	repo := &initialStageRepo{stageAppeared: true}
	AutoAssignInitialStage(repo, "ws", "camp", "unofficial_whatsapp", "entry", "unofficial_whatsapp")
	if len(repo.overwrote) != 0 {
		t.Fatal("the initial stage may only be written where no stage exists at write time")
	}
}

func TestAConversationWithAStageIsLeftAlone(t *testing.T) {
	repo := &initialStageRepo{current: &stage.EntryStage{StageID: "agendado"}}
	AutoAssignInitialStage(repo, "ws", "camp", "unofficial_whatsapp", "entry", "unofficial_whatsapp")
	if len(repo.assignedIfNone) != 0 || len(repo.overwrote) != 0 {
		t.Fatal("an existing stage is never touched")
	}
}
