package shortlink_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"golang.org/x/sync/singleflight"

	"vozko/domain/cache"
	"vozko/domain/shortlink"
)

const (
	resolvePositiveTTL = 5 * time.Minute
	resolveNegativeTTL = 30 * time.Second
)

type resolveShortLinkUseCase struct {
	repo   shortlink.ShortLinkRepository
	shared cache.SharedState
	group  *singleflight.Group
}

func NewResolveShortLinkUseCase(repo shortlink.ShortLinkRepository, shared cache.SharedState) shortlink.ResolveShortLinkUseCase {
	return &resolveShortLinkUseCase{
		repo:   repo,
		shared: shared,
		group:  &singleflight.Group{},
	}
}

func (uc *resolveShortLinkUseCase) Execute(ctx context.Context, code string) (*shortlink.ResolvedLink, error) {
	code = shortlink.NormalizeCode(code)
	if code == "" {
		return &shortlink.ResolvedLink{State: shortlink.ResolveNotFound}, nil
	}

	if cached, ok := uc.fromCache(code); ok {
		return cached, nil
	}

	result, err, _ := uc.group.Do(code, func() (any, error) {
		return uc.loadAndCache(ctx, code)
	})
	if err != nil {
		return nil, err
	}
	return result.(*shortlink.ResolvedLink), nil
}

func (uc *resolveShortLinkUseCase) fromCache(code string) (*shortlink.ResolvedLink, bool) {
	if uc.shared == nil {
		return nil, false
	}
	raw, err := uc.shared.GetString(resolveCacheKey(code))
	if err != nil || raw == "" {
		return nil, false
	}
	var resolved shortlink.ResolvedLink
	if err := json.Unmarshal([]byte(raw), &resolved); err != nil {
		return nil, false
	}
	return &resolved, true
}

func (uc *resolveShortLinkUseCase) loadAndCache(ctx context.Context, code string) (*shortlink.ResolvedLink, error) {
	link, err := uc.repo.FindByCode(ctx, code)
	if err != nil {
		if errors.Is(err, shortlink.ErrShortLinkNotFound) {
			resolved := &shortlink.ResolvedLink{State: shortlink.ResolveNotFound, Code: code}
			uc.cache(code, resolved, resolveNegativeTTL)
			return resolved, nil
		}
		return nil, err
	}

	resolved := resolvedFromLink(link)
	uc.cache(code, resolved, resolvePositiveTTL)
	return resolved, nil
}

func resolvedFromLink(link *shortlink.ShortLink) *shortlink.ResolvedLink {
	resolved := &shortlink.ResolvedLink{
		ShortLinkID:  link.ID,
		WorkspaceID:  link.WorkspaceID,
		Code:         link.Code,
		TargetURL:    link.TargetURL,
		RedirectType: link.RedirectType,
		HasPassword:  link.HasPasswordProtection(),
	}
	if !link.IsResolvable(time.Now()) {
		resolved.State = shortlink.ResolveGone
		return resolved
	}
	if link.HasPasswordProtection() {
		resolved.State = shortlink.ResolvePassword
		return resolved
	}
	resolved.State = shortlink.ResolveOK
	return resolved
}

func (uc *resolveShortLinkUseCase) cache(code string, resolved *shortlink.ResolvedLink, ttl time.Duration) {
	if uc.shared == nil {
		return
	}
	payload, _ := json.Marshal(resolved)
	_ = uc.shared.SetString(resolveCacheKey(code), string(payload), ttl)
}
