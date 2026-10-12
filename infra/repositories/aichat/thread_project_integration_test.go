package aichat_repository

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestAThreadBindsOnlyToTheFirstStudioProject(t *testing.T) {
	db := repotest.IsolatedDB(t, "chat_project_test", &schema.AIChatThread{})
	repo := NewThreadRepository(db)
	projects := NewThreadProjects(db)
	thread := &aichat.Thread{ID: uuid.New().String(), WorkspaceID: uuid.New().String(), UserID: uuid.New().String()}
	if err := repo.Create(thread); err != nil {
		t.Fatal(err)
	}
	first, second := uuid.New().String(), uuid.New().String()

	if ok, err := projects.BindProject(thread.ID, first); err != nil || !ok {
		t.Fatalf("an unbound thread must bind, got %v %v", ok, err)
	}
	if ok, err := projects.BindProject(thread.ID, first); err != nil || !ok {
		t.Fatalf("the same project must keep binding, got %v %v", ok, err)
	}
	if ok, err := projects.BindProject(thread.ID, second); err != nil || ok {
		t.Fatalf("another project must be refused, got %v %v", ok, err)
	}
	got, err := repo.GetByID(thread.ID)
	if err != nil || got.ProjectID != first {
		t.Fatalf("the thread must load with its first project, got %+v %v", got, err)
	}
}

func TestAThreadAdoptsTheProjectItsAssistantCreated(t *testing.T) {
	db := repotest.IsolatedDB(t, "chat_adopt_test", &schema.AIChatThread{})
	repo := NewThreadRepository(db)
	thread := &aichat.Thread{ID: uuid.New().String(), WorkspaceID: uuid.New().String(), UserID: uuid.New().String()}
	if err := repo.Create(thread); err != nil {
		t.Fatal(err)
	}
	earlier, created := uuid.New().String(), uuid.New().String()
	if ok, err := NewThreadProjects(db).BindProject(thread.ID, earlier); err != nil || !ok {
		t.Fatalf("bind: %v %v", ok, err)
	}
	adopter := NewProjectAdopter(db)
	if err := adopter.AdoptProject(thread.ID, created); err != nil {
		t.Fatalf("a thread must adopt the project its assistant created, got %v", err)
	}
	got, err := repo.GetByID(thread.ID)
	if err != nil || got.ProjectID != created {
		t.Fatalf("the thread must now belong to the created project, got %+v %v", got, err)
	}
	if err := adopter.AdoptProject(thread.ID, created); err != nil {
		t.Fatalf("adopting the same project again must pass, got %v", err)
	}
	if ok, err := NewThreadProjects(db).BindProject(thread.ID, created); err != nil || !ok {
		t.Fatalf("the next studio turn in the created project must be accepted, got %v %v", ok, err)
	}
	if ok, err := NewThreadProjects(db).BindProject(thread.ID, earlier); err != nil || ok {
		t.Fatalf("the earlier project is no longer this session's, got %v %v", ok, err)
	}
	if err := adopter.AdoptProject(uuid.New().String(), created); !errors.Is(err, aichat.ErrThreadNotFound) {
		t.Fatalf("an unknown thread must be refused, got %v", err)
	}
}

func TestTheThreadListSearchesTitlesOfTheOwnerOnly(t *testing.T) {
	repo := NewThreadRepository(repotest.IsolatedDB(t, "chat_search_test", &schema.AIChatThread{}))
	workspace, user := uuid.New().String(), uuid.New().String()
	threads := []*aichat.Thread{
		{ID: uuid.New().String(), WorkspaceID: workspace, UserID: user, Title: "Relatório da equipe"},
		{ID: uuid.New().String(), WorkspaceID: workspace, UserID: user, Title: "Desconto de 50% no banner"},
		{ID: uuid.New().String(), WorkspaceID: workspace, UserID: user, Title: "Agente de cobrança"},
		{ID: uuid.New().String(), WorkspaceID: uuid.New().String(), UserID: user, Title: "Relatório da equipe de outro espaço"},
	}
	for _, th := range threads {
		if err := repo.Create(th); err != nil {
			t.Fatal(err)
		}
	}

	found, total, err := repo.ListByUser(aichat.ListThreadsInput{WorkspaceID: workspace, UserID: user, Query: "EQUIPE", Limit: 10})
	if err != nil || total != 1 || len(found) != 1 || found[0].ID != threads[0].ID {
		t.Fatalf("the search must match titles in the owner's workspace only, got %d %+v %v", total, found, err)
	}
	found, total, err = repo.ListByUser(aichat.ListThreadsInput{WorkspaceID: workspace, UserID: user, Query: "0%", Limit: 10})
	if err != nil || total != 1 || len(found) != 1 || found[0].ID != threads[1].ID {
		t.Fatalf("a percent sign must match itself, got %d %+v %v", total, found, err)
	}
	_, total, err = repo.ListByUser(aichat.ListThreadsInput{WorkspaceID: workspace, UserID: user, Limit: 10})
	if err != nil || total != 3 {
		t.Fatalf("without a search every thread of the owner must be listed, got %d %v", total, err)
	}
}
