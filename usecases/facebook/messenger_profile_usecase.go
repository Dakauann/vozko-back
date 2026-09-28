package facebook

import (
	"context"
	"errors"

	fbdomain "vozko/domain/facebook"
)

type MessengerProfileUseCases struct {
	pages   fbdomain.PageRepository
	service fbdomain.ProfileSettingsService
}

func NewMessengerProfileUseCases(pages fbdomain.PageRepository, service fbdomain.ProfileSettingsService) *MessengerProfileUseCases {
	return &MessengerProfileUseCases{pages: pages, service: service}
}

func (uc *MessengerProfileUseCases) Get(ctx context.Context, workspaceID, pageID string) (*fbdomain.MessengerProfile, error) {
	page, err := pageWith(ctx, uc.pages, workspaceID, pageID, fbdomain.CapMessaging)
	if err != nil {
		return nil, err
	}
	return uc.service.Get(ctx, page.FBPageID, page.PageToken)
}

func (uc *MessengerProfileUseCases) Update(ctx context.Context, workspaceID, pageID string, profile fbdomain.MessengerProfile) error {
	page, err := pageWith(ctx, uc.pages, workspaceID, pageID, fbdomain.CapMessaging)
	if err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	if profile.HasContent() {
		if err := uc.service.Set(ctx, page.FBPageID, page.PageToken, profile); err != nil {
			return rateLimited(err)
		}
	}
	return rateLimited(uc.service.Delete(ctx, page.FBPageID, page.PageToken, profile.ClearedFields()))
}

func rateLimited(err error) error {
	if err != nil && fbdomain.Classify(err) == fbdomain.FailureRetryable {
		return errors.Join(fbdomain.ErrProfileRateLimited, err)
	}
	return err
}
