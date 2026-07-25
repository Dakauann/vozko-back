package shortlink_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/domain/auth"
	"vozko/domain/shortlink"
)

type unlockShortLinkUseCase struct {
	repo        shortlink.ShortLinkRepository
	passwordSvc auth.PasswordService
}

func NewUnlockShortLinkUseCase(repo shortlink.ShortLinkRepository, passwordSvc auth.PasswordService) shortlink.UnlockShortLinkUseCase {
	return &unlockShortLinkUseCase{repo: repo, passwordSvc: passwordSvc}
}

func (uc *unlockShortLinkUseCase) Execute(ctx context.Context, code, password string) (*shortlink.ResolvedLink, error) {
	link, err := uc.repo.FindByCode(ctx, shortlink.NormalizeCode(code))
	if err != nil {
		return nil, err
	}

	if !link.IsResolvable(time.Now()) {
		return nil, shortlink.ErrShortLinkNotFound
	}

	if link.HasPasswordProtection() {
		if strings.TrimSpace(password) == "" {
			return nil, shortlink.ErrPasswordRequired
		}
		if err := uc.passwordSvc.Verify(link.PasswordHash, password); err != nil {
			return nil, shortlink.ErrInvalidPassword
		}
	}

	return &shortlink.ResolvedLink{
		State:        shortlink.ResolveOK,
		ShortLinkID:  link.ID,
		WorkspaceID:  link.WorkspaceID,
		Code:         link.Code,
		TargetURL:    link.TargetURL,
		RedirectType: link.RedirectType,
		HasPassword:  link.HasPasswordProtection(),
	}, nil
}
