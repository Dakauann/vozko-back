package shortlink_usecase

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/auth"
	"vozko/domain/shortlink"
)

type createShortLinkUseCase struct {
	repo        shortlink.ShortLinkRepository
	hostGuard   shortlink.HostGuard
	scanner     shortlink.ThreatScanner
	passwordSvc auth.PasswordService
	codeLength  int
	baseHost    string
}

func NewCreateShortLinkUseCase(
	repo shortlink.ShortLinkRepository,
	hostGuard shortlink.HostGuard,
	scanner shortlink.ThreatScanner,
	passwordSvc auth.PasswordService,
	codeLength int,
	baseHost string,
) shortlink.CreateShortLinkUseCase {
	return &createShortLinkUseCase{
		repo:        repo,
		hostGuard:   hostGuard,
		scanner:     scanner,
		passwordSvc: passwordSvc,
		codeLength:  codeLength,
		baseHost:    baseHost,
	}
}

func (uc *createShortLinkUseCase) Execute(ctx context.Context, input shortlink.CreateShortLinkInput) (*shortlink.ShortLink, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" {
		return nil, shortlink.ErrWorkspaceIDRequired
	}

	targetURL := strings.TrimSpace(input.TargetURL)
	if err := shortlink.ValidateTargetURL(targetURL); err != nil {
		return nil, err
	}
	if err := uc.guardTarget(ctx, targetURL); err != nil {
		return nil, err
	}

	redirectType, err := resolveRedirectType(input.RedirectType)
	if err != nil {
		return nil, err
	}

	count, err := uc.repo.CountByWorkspace(ctx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if count >= shortlink.MaxShortLinksPerWorkspace {
		return nil, shortlink.ErrMaxShortLinksReached
	}

	code, err := uc.resolveCode(ctx, input.CustomAlias)
	if err != nil {
		return nil, err
	}

	passwordHash := ""
	if strings.TrimSpace(input.Password) != "" {
		passwordHash, err = uc.passwordSvc.Hash(input.Password)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now()
	link := &shortlink.ShortLink{
		ID:           uuid.New().String(),
		WorkspaceID:  input.WorkspaceID,
		CreatedBy:    input.CreatedBy,
		Code:         code,
		TargetURL:    targetURL,
		Title:        input.Title,
		RedirectType: redirectType,
		Status:       shortlink.LinkStatusActive,
		PasswordHash: passwordHash,
		ExpiresAt:    input.ExpiresAt,
		MaxClicks:    input.MaxClicks,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if input.DepartmentID != nil {
		link.DepartmentID = strings.TrimSpace(*input.DepartmentID)
	}

	link.Normalize()
	if err := link.Validate(); err != nil {
		return nil, err
	}

	if err := uc.repo.Create(ctx, link); err != nil {
		return nil, err
	}
	return link, nil
}

func (uc *createShortLinkUseCase) guardTarget(ctx context.Context, targetURL string) error {
	host := shortlink.TargetHost(targetURL)
	if uc.baseHost != "" && strings.EqualFold(host, uc.baseHost) {
		return shortlink.ErrTargetURLLoop
	}
	if uc.hostGuard != nil && uc.hostGuard.ResolvesToBlocked(host) {
		return shortlink.ErrTargetURLBlocked
	}
	if uc.scanner != nil {
		verdict, err := uc.scanner.Scan(ctx, targetURL)
		if err != nil {
			return err
		}
		if !verdict.Safe {
			return shortlink.ErrThreatDetected
		}
	}
	return nil
}

func (uc *createShortLinkUseCase) resolveCode(ctx context.Context, customAlias string) (string, error) {
	alias := strings.TrimSpace(customAlias)
	if alias != "" {
		if err := shortlink.ValidateCustomAlias(alias); err != nil {
			return "", err
		}
		exists, err := uc.repo.CodeExists(ctx, alias)
		if err != nil {
			return "", err
		}
		if exists {
			return "", shortlink.ErrCodeTaken
		}
		return alias, nil
	}

	for range shortlink.MaxCodeGenerationAttempts {
		code, err := generateCode(uc.codeLength)
		if err != nil {
			return "", err
		}
		exists, err := uc.repo.CodeExists(ctx, code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", shortlink.ErrCodeGenerationFailed
}
