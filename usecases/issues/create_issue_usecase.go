package issues_usecase

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/issues"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type createIssueUseCase struct {
	repo          issues.Repository
	workspaceRepo workspace.Repository
	userRepo      user.UserRepository
	emailPub      notification.PublishEmailUseCase
}

func NewCreateIssueUseCase(
	repo issues.Repository,
	workspaceRepo workspace.Repository,
	userRepo user.UserRepository,
	emailPub notification.PublishEmailUseCase,
) issues.CreateIssueUseCase {
	return &createIssueUseCase{
		repo:          repo,
		workspaceRepo: workspaceRepo,
		userRepo:      userRepo,
		emailPub:      emailPub,
	}
}

func (uc *createIssueUseCase) Execute(workspaceID, title, description string, imageURLs []string) (*issues.Issue, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, issues.ErrIssueTitleRequired
	}
	if len(title) > 255 {
		return nil, issues.ErrIssueTitleTooLong
	}

	now := time.Now().Unix()
	issue := &issues.Issue{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Title:       title,
		Description: strings.TrimSpace(description),
		ImageURLs:   imageURLs,
		Status:      issues.IssueStatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := uc.repo.Create(issue); err != nil {
		return nil, err
	}

	go uc.notifyIssueCreated(issue)

	return issue, nil
}

func (uc *createIssueUseCase) notifyIssueCreated(issue *issues.Issue) {
	if uc.emailPub == nil {
		return
	}

	ws, err := uc.workspaceRepo.GetWorkspaceByID(issue.WorkspaceID)
	if err != nil || ws == nil {
		log.Printf("[issue-email] failed to get workspace %s for creation notification: %v", issue.WorkspaceID, err)
		return
	}

	owner, err := uc.userRepo.FindByID(ws.OwnerID)
	if err != nil || owner == nil || owner.Email == "" {
		log.Printf("[issue-email] failed to get owner %s for creation notification: %v", ws.OwnerID, err)
		return
	}

	ownerName := owner.Username
	if ownerName == "" {
		ownerName = owner.Email
	}

	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	dashboardURL := fmt.Sprintf("%s/dashboard/issues/%s", frontendURL, issue.ID)

	placeholders := map[string]interface{}{
		"OwnerName":        ownerName,
		"IssueTitle":       issue.Title,
		"IssueDescription": issue.Description,
		"DashboardURL":     dashboardURL,
	}

	subject := fmt.Sprintf("Chamado recebido: %s", issue.Title)
	if err := uc.emailPub.Publish(owner.Email, subject, "issue_created_confirmation.html", placeholders); err != nil {
		log.Printf("[issue-email] failed to publish creation notification for issue %s: %v", issue.ID, err)
	}
}
