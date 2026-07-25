package stage_usecase

import (
	"testing"

	"vozko/domain/stage"
)

type updateGroupRepoStub struct {
	stage.StageGroupRepository
	group *stage.StageGroup
}

func (s *updateGroupRepoStub) FindByID(string) (*stage.StageGroup, error) { return s.group, nil }
func (s *updateGroupRepoStub) Update(*stage.StageGroup) error             { return nil }
func (s *updateGroupRepoStub) RemoveItem(string) error                   { return nil }
func (s *updateGroupRepoStub) AddItem(*stage.StageGroupItem) error       { return nil }

type updateStageRepoStub struct {
	stage.Repository
	pipelineID string
	current    []*stage.Stage
	deleted    []string
	created    []*stage.Stage
	updated    []string
}

func (s *updateStageRepoStub) FindConversationPipelineByGroup(_, _ string) (string, error) {
	return s.pipelineID, nil
}
func (s *updateStageRepoStub) ListByPipeline(_, _ string) ([]*stage.Stage, error) {
	return s.current, nil
}
func (s *updateStageRepoStub) Delete(id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}
func (s *updateStageRepoStub) Update(st *stage.Stage) error {
	s.updated = append(s.updated, st.ID)
	return nil
}
func (s *updateStageRepoStub) Create(st *stage.Stage) error {
	s.created = append(s.created, st)
	return nil
}

// Editing a stage group must reconcile the shared conversation pipeline stamped from
// it: a removed item's stage is deleted (so it stops showing up on the board / new
// campaigns), a brand-new item is cloned in, and untouched items are left in place.
func TestUpdateStageGroupSyncsMaterializedPipeline(t *testing.T) {
	groupRepo := &updateGroupRepoStub{group: &stage.StageGroup{
		WorkspaceID: "ws1", Name: "Funil PIB WhatsApp",
		Items: []stage.StageGroupItem{{ID: "i1", Name: "Novo no WhatsApp"}, {ID: "i2", Name: "Em conversa"}},
	}}
	stageRepo := &updateStageRepoStub{
		pipelineID: "pipe1",
		current: []*stage.Stage{
			{ID: "s-novo", Name: "novo no whatsapp", IsInitial: true},
			{ID: "s-conv", Name: "em conversa"},
			{ID: "s-link", Name: "link enviado"},
		},
	}
	uc := NewUpdateStageGroupUseCase(groupRepo, stageRepo)

	// New group state: "em conversa" removed, "agendado" added, others kept.
	_, err := uc.Execute("ws1", "grp1", stage.UpdateStageGroupInput{
		Items: []stage.StageGroupItemInput{
			{Name: "Novo no WhatsApp"},
			{Name: "Link enviado"},
			{Name: "Agendado"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stageRepo.deleted) != 1 || stageRepo.deleted[0] != "s-conv" {
		t.Errorf("removed item's stage must be deleted, got deleted=%v", stageRepo.deleted)
	}
	if len(stageRepo.created) != 1 || stageRepo.created[0].Name != "agendado" {
		t.Errorf("new item must be cloned in as a stage, got created=%+v", stageRepo.created)
	}
	if stageRepo.created[0].IsInitial {
		t.Errorf("a new non-first item must not steal initial from the existing initial stage")
	}
}

// When no campaign has materialized the group yet there is no pipeline to reconcile;
// the update must not blow up or fabricate stages.
func TestUpdateStageGroupNoPipelineNoop(t *testing.T) {
	groupRepo := &updateGroupRepoStub{group: &stage.StageGroup{WorkspaceID: "ws1"}}
	stageRepo := &updateStageRepoStub{pipelineID: ""}
	uc := NewUpdateStageGroupUseCase(groupRepo, stageRepo)

	if _, err := uc.Execute("ws1", "grp1", stage.UpdateStageGroupInput{
		Items: []stage.StageGroupItemInput{{Name: "A"}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stageRepo.created)+len(stageRepo.deleted)+len(stageRepo.updated) != 0 {
		t.Errorf("no pipeline stamped from the group → no stage writes expected")
	}
}
