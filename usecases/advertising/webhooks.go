package advertising

import (
	"context"
	"errors"
	"log"

	ads "vozko/domain/advertising"
)

func (uc *SyncUseCase) HandleAccountChanges(ctx context.Context, changes []ads.AdAccountChange) error {
	refreshed := map[string]bool{}
	var failures []error
	for _, change := range changes {
		metaID := ads.NormalizeAccountID(change.AccountMetaID)
		if refreshed[metaID] {
			continue
		}
		refreshed[metaID] = true
		account, err := uc.access.accounts.FindByMetaAccountID(ctx, metaID)
		if errors.Is(err, ads.ErrAccountNotFound) {
			log.Printf("[ads] webhook for ad account %s that no workspace has connected; ignored", metaID)
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		token, err := uc.access.tokenFor(ctx, account, ads.UseRead)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := uc.SyncStructure(ctx, account, token); err != nil {
			failures = append(failures, uc.access.failed(ctx, account, err))
		}
	}
	return errors.Join(failures...)
}
