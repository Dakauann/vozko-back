package shortlink_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/shortlink"
)

type updateShortLinkUseCase struct {
	repo        shortlink.ShortLinkRepository
	hostGuard   shortlink.HostGuard
	scanner     shortlink.ThreatScanner
	passwordSvc auth.PasswordService
	shared      cache.SharedState
	baseHost    string
}

func NewUpdateShortLinkUseCase(
	repo shortlink.ShortLinkRepository,
	hostGuard shortlink.HostGuard,
	scanner shortlink.ThreatScanner,
	passwordSvc auth.PasswordService,
	shared cache.SharedState,
	baseHost string,
) shortlink.UpdateShortLinkUseCase {
	return &updateShortLinkUseCase{
		repo:        repo,
		hostGuard:   hostGuard,
		scanner:     scanner,
		passwordSvc: passwordSvc,
		shared:      shared,
		baseHost:    baseHost,
	}
}

func (uc *updateShortLinkUseCase) Execute(ctx context.Context, workspaceID, id string, input shortlink.UpdateShortLinkInput) (*shortlink.ShortLink, error) {
	link, err := uc.repo.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}

	if input.TargetURL != nil {
		targetURL := strings.TrimSpace(*input.TargetURL)
		if err := shortlink.ValidateTargetURL(targetURL); err != nil {
			return nil, err
		}
		if err := uc.guardTarget(ctx, targetURL); err != nil {
			return nil, err
		}
		link.TargetURL = targetURL
	}

	if input.Title != nil {
		link.Title = strings.TrimSpace(*input.Title)
	}

	if input.RedirectType != nil {
		rt, err := resolveRedirectType(*input.RedirectType)
		if err != nil {
			return nil, err
		}
		link.RedirectType = rt
	}

	if input.Status != nil {
		status := shortlink.LinkStatus(strings.TrimSpace(*input.Status))
		if !status.IsValid() {
			return nil, shortlink.ErrInvalidStatus
		}
		link.Status = status
	}

	if err := uc.applyPassword(link, input); err != nil {
		return nil, err
	}
	uc.applyExpiry(link, input)
	uc.applyMaxClicks(link, input)

	link.UpdatedAt = time.Now()
	link.Normalize()
	if err := link.Validate(); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(ctx, link); err != nil {
		return nil, err
	}
	uc.invalidate(link.Code)
	return link, nil
}

func (uc *updateShortLinkUseCase) applyPassword(link *shortlink.ShortLink, input shortlink.UpdateShortLinkInput) error {
	if input.ClearPassword {
		link.PasswordHash = ""
		return nil
	}
	if input.Password != nil && strings.TrimSpace(*input.Password) != "" {
		hash, err := uc.passwordSvc.Hash(*input.Password)
		if err != nil {
			return err
		}
		link.PasswordHash = hash
	}
	return nil
}

func (uc *updateShortLinkUseCase) applyExpiry(link *shortlink.ShortLink, input shortlink.UpdateShortLinkInput) {
	if input.ClearExpiry {
		link.ExpiresAt = nil
		return
	}
	if input.ExpiresAt != nil {
		link.ExpiresAt = input.ExpiresAt
	}
}

func (uc *updateShortLinkUseCase) applyMaxClicks(link *shortlink.ShortLink, input shortlink.UpdateShortLinkInput) {
	if input.ClearMaxClicks {
		link.MaxClicks = nil
		return
	}
	if input.MaxClicks != nil {
		link.MaxClicks = input.MaxClicks
	}
}

func (uc *updateShortLinkUseCase) guardTarget(ctx context.Context, targetURL string) error {
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

func (uc *updateShortLinkUseCase) invalidate(code string) {
	if uc.shared == nil {
		return
	}
	_ = uc.shared.Del(resolveCacheKey(code))
}
