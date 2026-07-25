package issues_usecase

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/issues"
	"vozko/domain/issues/issue_response"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type createResponseUseCase struct {
	issueRepo     issues.Repository
	responseRepo  issue_response.Repository
	workspaceRepo workspace.Repository
	userRepo      user.UserRepository
	emailPub      notification.PublishEmailUseCase
}

func NewCreateResponseUseCase(
	issueRepo issues.Repository,
	responseRepo issue_response.Repository,
	workspaceRepo workspace.Repository,
	userRepo user.UserRepository,
	emailPub notification.PublishEmailUseCase,
) issue_response.CreateResponseUseCase {
	return &createResponseUseCase{
		issueRepo:     issueRepo,
		responseRepo:  responseRepo,
		workspaceRepo: workspaceRepo,
		userRepo:      userRepo,
		emailPub:      emailPub,
	}
}

func (uc *createResponseUseCase) Execute(issueID, authorID, body string, imageURL *string) (*issue_response.IssueResponse, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, issue_response.ErrResponseBodyEmpty
	}
	if len(body) > 2000 {
		return nil, issue_response.ErrResponseBodyTooLong
	}

	issue, err := uc.issueRepo.GetByID(issueID)
	if err != nil {
		return nil, issue_response.ErrIssueNotFound
	}

	resp := &issue_response.IssueResponse{
		ID:        uuid.New().String(),
		IssueID:   issueID,
		AuthorID:  authorID,
		Body:      body,
		ImageURL:  imageURL,
		CreatedAt: time.Now().Unix(),
	}

	if err := uc.responseRepo.Create(resp); err != nil {
		return nil, err
	}

	go uc.notifyIssueOwner(issue, resp)

	return resp, nil
}

func (uc *createResponseUseCase) notifyIssueOwner(issue *issues.Issue, resp *issue_response.IssueResponse) {
	if uc.emailPub == nil {
		return
	}

	ws, err := uc.workspaceRepo.GetWorkspaceByID(issue.WorkspaceID)
	if err != nil || ws == nil {
		log.Printf("[issue-email] failed to get workspace %s: %v", issue.WorkspaceID, err)
		return
	}

	if ws.OwnerID == resp.AuthorID {
		return
	}

	owner, err := uc.userRepo.FindByID(ws.OwnerID)
	if err != nil || owner == nil || owner.Email == "" {
		log.Printf("[issue-email] failed to get owner %s: %v", ws.OwnerID, err)
		return
	}

	author, _ := uc.userRepo.FindByID(resp.AuthorID)
	authorName := "Equipe de Suporte"
	authorInitial := "S"
	if author != nil && author.Username != "" {
		authorName = author.Username
		authorInitial = strings.ToUpper(author.Username[:1])
	}

	ownerName := owner.Username
	if ownerName == "" {
		ownerName = owner.Email
	}

	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	dashboardURL := fmt.Sprintf("%s/dashboard/issues/%s", frontendURL, issue.ID)

	statusLabel, statusColor := issueStatusDisplay(issue.Status)

	placeholders := map[string]interface{}{
		"OwnerName":     ownerName,
		"IssueTitle":    issue.Title,
		"StatusLabel":   statusLabel,
		"StatusColor":   statusColor,
		"AuthorName":    authorName,
		"AuthorInitial": authorInitial,
		"ResponseBody":  resp.Body,
		"ResponseDate":  time.Unix(resp.CreatedAt, 0).Format("02/01/2006 15:04"),
		"DashboardURL":  dashboardURL,
	}

	subject := fmt.Sprintf("Nova resposta no chamado: %s", issue.Title)
	if err := uc.emailPub.Publish(owner.Email, subject, "issue_response_notification.html", placeholders); err != nil {
		log.Printf("[issue-email] failed to publish response notification for issue %s: %v", issue.ID, err)
	}
}

func issueStatusDisplay(status issues.IssueStatus) (label string, color string) {
	switch status {
	case issues.IssueStatusOpen:
		return "Aberto", "#4A90E2"
	case issues.IssueStatusInProgress:
		return "Em Andamento", "#f0ad4e"
	case issues.IssueStatusClosed:
		return "Fechado", "#28a745"
	default:
		return string(status), "#6c757d"
	}
}
