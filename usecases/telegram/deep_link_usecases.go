package telegram

import (
	"context"
	"strings"
	"time"

	tgdomain "vozko/domain/telegram"
)

type CreateDeepLinkInput struct {
	WorkspaceID  string
	AccountID    string
	Label        string
	LeadID       *string
	CampaignID   *string
	AgentID      *string
	DepartmentID *string
	TTL          time.Duration
}

type CreateDeepLinkUseCase struct {
	accounts  tgdomain.AccountRepository
	deepLinks tgdomain.DeepLinkRepository
}

func NewCreateDeepLinkUseCase(
	accounts tgdomain.AccountRepository,
	deepLinks tgdomain.DeepLinkRepository,
) *CreateDeepLinkUseCase {
	return &CreateDeepLinkUseCase{accounts: accounts, deepLinks: deepLinks}
}

type DeepLinkResult struct {
	Link *tgdomain.DeepLink `json:"link"`
	URL  string             `json:"url"`
}

func (uc *CreateDeepLinkUseCase) Execute(ctx context.Context, in CreateDeepLinkInput) (*DeepLinkResult, error) {
	account, err := uc.accounts.FindByID(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != in.WorkspaceID {
		return nil, tgdomain.ErrAccountNotFound
	}

	token, err := tgdomain.GenerateDeepLinkToken()
	if err != nil {
		return nil, err
	}
	if !tgdomain.ValidDeepLinkToken(token) {
		return nil, tgdomain.ErrDeepLinkNotFound
	}

	link := &tgdomain.DeepLink{
		Token:        token,
		AccountID:    account.ID,
		WorkspaceID:  account.WorkspaceID,
		LeadID:       in.LeadID,
		CampaignID:   in.CampaignID,
		AgentID:      in.AgentID,
		DepartmentID: in.DepartmentID,
		Label:        strings.TrimSpace(in.Label),
	}
	if in.TTL > 0 {
		expires := time.Now().UTC().Add(in.TTL)
		link.ExpiresAt = &expires
	}

	if err := uc.deepLinks.Create(ctx, link); err != nil {
		return nil, err
	}
	return &DeepLinkResult{Link: link, URL: link.URL(account.BotUsername)}, nil
}

type ListDeepLinksUseCase struct {
	accounts  tgdomain.AccountRepository
	deepLinks tgdomain.DeepLinkRepository
}

func NewListDeepLinksUseCase(
	accounts tgdomain.AccountRepository,
	deepLinks tgdomain.DeepLinkRepository,
) *ListDeepLinksUseCase {
	return &ListDeepLinksUseCase{accounts: accounts, deepLinks: deepLinks}
}

func (uc *ListDeepLinksUseCase) Execute(ctx context.Context, workspaceID, accountID string, limit int) ([]*DeepLinkResult, error) {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, tgdomain.ErrAccountNotFound
	}

	links, err := uc.deepLinks.ListByAccount(ctx, accountID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*DeepLinkResult, 0, len(links))
	for _, link := range links {
		out = append(out, &DeepLinkResult{Link: link, URL: link.URL(account.BotUsername)})
	}
	return out, nil
}

type DeleteDeepLinkUseCase struct {
	accounts  tgdomain.AccountRepository
	deepLinks tgdomain.DeepLinkRepository
}

func NewDeleteDeepLinkUseCase(
	accounts tgdomain.AccountRepository,
	deepLinks tgdomain.DeepLinkRepository,
) *DeleteDeepLinkUseCase {
	return &DeleteDeepLinkUseCase{accounts: accounts, deepLinks: deepLinks}
}

func (uc *DeleteDeepLinkUseCase) Execute(ctx context.Context, workspaceID, accountID, token string) error {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.WorkspaceID != workspaceID {
		return tgdomain.ErrAccountNotFound
	}
	return uc.deepLinks.Delete(ctx, accountID, token)
}
