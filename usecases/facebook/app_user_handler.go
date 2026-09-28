package facebook

import (
	"context"
	"errors"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/domain/metaplatform"
)

type AppUserHandler struct {
	grants fbdomain.GrantRepository
	pages  fbdomain.PageRepository
	now    func() time.Time
}

func NewAppUserHandler(grants fbdomain.GrantRepository, pages fbdomain.PageRepository) *AppUserHandler {
	return &AppUserHandler{grants: grants, pages: pages, now: func() time.Time { return time.Now().UTC() }}
}

var _ metaplatform.AppUserHandler = (*AppUserHandler)(nil)

func (h *AppUserHandler) RevokeAppUser(ctx context.Context, appScopedUserID string) error {
	grants, err := h.grants.ListByAppScopedUser(ctx, appScopedUserID)
	if err != nil {
		return err
	}
	var errs []error
	for _, grant := range grants {
		errs = append(errs, h.revoke(ctx, grant))
	}
	return errors.Join(errs...)
}

func (h *AppUserHandler) EraseAppUser(ctx context.Context, appScopedUserID string) error {
	grants, err := h.grants.ListByAppScopedUser(ctx, appScopedUserID)
	if err != nil {
		return err
	}
	var errs []error
	for _, grant := range grants {
		if err := h.revoke(ctx, grant); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, h.grants.EraseToken(ctx, grant.ID))
		pages, err := h.pages.ListByGrant(ctx, grant.ID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, page := range pages {
			errs = append(errs,
				h.pages.UpdateToken(ctx, page.ID, grant.ID, "", nil, nil),
				h.pages.Delete(ctx, page.ID))
		}
	}
	return errors.Join(errs...)
}

func (h *AppUserHandler) revoke(ctx context.Context, grant *fbdomain.Grant) error {
	if err := h.grants.Revoke(ctx, grant.ID, h.now()); err != nil {
		return err
	}
	pages, err := h.pages.ListByGrant(ctx, grant.ID)
	if err != nil {
		return err
	}
	var errs []error
	for _, page := range pages {
		if page.Status.CanTransitionTo(fbdomain.StatusTokenRevoked) {
			errs = append(errs, h.pages.UpdateStatus(ctx, page.ID, fbdomain.StatusTokenRevoked, "access removed in Facebook"))
		}
	}
	return errors.Join(errs...)
}
