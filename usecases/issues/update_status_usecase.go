package issues_usecase

import (
	"fmt"
	"log"
	"os"
	"strings"

	"vozko/domain/issues"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type updateIssueStatusUseCase struct {
	repo          issues.Repository
	workspaceRepo workspace.Repository
	userRepo      user.UserRepository
	emailPub      notification.PublishEmailUseCase
}

func NewUpdateIssueStatusUseCase(
	repo issues.Repository,
	workspaceRepo workspace.Repository,
	userRepo user.UserRepository,
	emailPub notification.PublishEmailUseCase,
) issues.UpdateIssueStatusUseCase {
	return &updateIssueStatusUseCase{
		repo:          repo,
		workspaceRepo: workspaceRepo,
		userRepo:      userRepo,
		emailPub:      emailPub,
	}
}

func (uc *updateIssueStatusUseCase) Execute(id string, status issues.IssueStatus) (*issues.Issue, error) {
	if !status.IsValid() {
		return nil, issues.ErrInvalidStatus
	}

	existing, err := uc.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if existing.Status == issues.IssueStatusClosed {
		return nil, issues.ErrIssueAlreadyClosed
	}

	oldStatus := existing.Status

	if err := uc.repo.UpdateStatus(id, status); err != nil {
		return nil, err
	}

	updated, err := uc.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	go uc.notifyStatusChange(updated, oldStatus, status)

	return updated, nil
}

func (uc *updateIssueStatusUseCase) notifyStatusChange(issue *issues.Issue, oldStatus, newStatus issues.IssueStatus) {
	if uc.emailPub == nil {
		return
	}

	ws, err := uc.workspaceRepo.GetWorkspaceByID(issue.WorkspaceID)
	if err != nil || ws == nil {
		log.Printf("[issue-email] failed to get workspace %s for status update: %v", issue.WorkspaceID, err)
		return
	}

	owner, err := uc.userRepo.FindByID(ws.OwnerID)
	if err != nil || owner == nil || owner.Email == "" {
		log.Printf("[issue-email] failed to get owner %s for status update: %v", ws.OwnerID, err)
		return
	}

	ownerName := owner.Username
	if ownerName == "" {
		ownerName = owner.Email
	}

	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	dashboardURL := fmt.Sprintf("%s/dashboard/issues/%s", frontendURL, issue.ID)

	oldLabel, oldColor := issueStatusDisplay(oldStatus)
	newLabel, newColor := issueStatusDisplay(newStatus)

	placeholders := map[string]interface{}{
		"OwnerName":      ownerName,
		"IssueTitle":     issue.Title,
		"OldStatusLabel": oldLabel,
		"OldStatusColor": oldColor,
		"NewStatusLabel": newLabel,
		"NewStatusColor": newColor,
		"IsClosed":       newStatus == issues.IssueStatusClosed,
		"DashboardURL":   dashboardURL,
	}

	subject := fmt.Sprintf("Atualização no chamado: %s", issue.Title)
	if err := uc.emailPub.Publish(owner.Email, subject, "issue_status_update.html", placeholders); err != nil {
		log.Printf("[issue-email] failed to publish status update notification for issue %s: %v", issue.ID, err)
	}
}
