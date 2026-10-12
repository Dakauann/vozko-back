package aichat_usecase

import (
	"errors"
	"testing"

	"vozko/domain/aichat"
)

type projectBinder struct {
	bound map[string]string
	calls int
	err   error
}

func (p *projectBinder) BindProject(threadID, projectID string) (bool, error) {
	p.calls++
	if p.err != nil {
		return false, p.err
	}
	if current, ok := p.bound[threadID]; ok && current != projectID {
		return false, nil
	}
	p.bound[threadID] = projectID
	return true, nil
}

func projectService(binder aichat.ThreadProjects) *Service {
	svc := NewService(&costThreads{}, nil, nil, nil)
	if binder != nil {
		svc.SetThreadProjects(binder)
	}
	return svc
}

func TestTheFirstStudioTurnBindsTheThreadToItsProject(t *testing.T) {
	binder := &projectBinder{bound: map[string]string{}}
	svc := projectService(binder)
	thread := &aichat.Thread{ID: "th1"}

	if err := svc.ClaimProject(thread, "project-a"); err != nil {
		t.Fatalf("an unbound thread must accept the open project, got %v", err)
	}
	if thread.ProjectID != "project-a" || binder.bound["th1"] != "project-a" {
		t.Fatalf("the thread must remember the project, got %q and %v", thread.ProjectID, binder.bound)
	}
	if err := svc.ClaimProject(thread, "project-a"); err != nil {
		t.Fatalf("the same project must keep working, got %v", err)
	}
	if binder.calls != 1 {
		t.Fatalf("a thread already bound to the open project needs no write, got %d writes", binder.calls)
	}
}

func TestAStudioTurnFromAnotherProjectIsRefused(t *testing.T) {
	binder := &projectBinder{bound: map[string]string{}}
	svc := projectService(binder)

	bound := &aichat.Thread{ID: "th1", ProjectID: "project-a"}
	if err := svc.ClaimProject(bound, "project-b"); !errors.Is(err, ErrOtherProject) {
		t.Fatalf("a thread bound to another project must be refused, got %v", err)
	}

	binder.bound["th2"] = "project-a"
	stale := &aichat.Thread{ID: "th2"}
	if err := svc.ClaimProject(stale, "project-b"); !errors.Is(err, ErrOtherProject) {
		t.Fatalf("a thread bound meanwhile by another request must be refused, got %v", err)
	}
	if stale.ProjectID != "" {
		t.Fatalf("a refused claim must not change the thread, got %q", stale.ProjectID)
	}
}

func TestATurnOutsideTheStudioLeavesTheProjectAlone(t *testing.T) {
	binder := &projectBinder{bound: map[string]string{}}
	svc := projectService(binder)
	thread := &aichat.Thread{ID: "th1", ProjectID: "project-a"}

	if err := svc.ClaimProject(thread, ""); err != nil {
		t.Fatalf("a turn outside the studio must pass, got %v", err)
	}
	if binder.calls != 0 || thread.ProjectID != "project-a" {
		t.Fatalf("a turn outside the studio must not touch the binding, got %d writes and %q", binder.calls, thread.ProjectID)
	}
}

func TestAStudioTurnIsRefusedWhenTheBindingCannotBeChecked(t *testing.T) {
	thread := &aichat.Thread{ID: "th1"}
	if err := projectService(nil).ClaimProject(thread, "project-a"); !errors.Is(err, ErrProjectBindingUnavailable) {
		t.Fatalf("without a binding store a studio turn must be refused, got %v", err)
	}

	failure := errors.New("database down")
	if err := projectService(&projectBinder{bound: map[string]string{}, err: failure}).ClaimProject(thread, "project-a"); !errors.Is(err, failure) {
		t.Fatalf("a failed binding must refuse the turn, got %v", err)
	}
	if thread.ProjectID != "" {
		t.Fatalf("a failed binding must not change the thread, got %q", thread.ProjectID)
	}
}
