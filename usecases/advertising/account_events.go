package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	ads "vozko/domain/advertising"
)

const structureRefreshWindow = time.Minute

type objectFetcher interface {
	GetObject(ctx context.Context, token, metaID string, level ads.Level) (*ads.Object, error)
}

type refreshGate interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
}

type AccountEventsUseCase struct {
	sync    *SyncUseCase
	objects objectFetcher
	gate    refreshGate
}

func NewAccountEventsUseCase(sync *SyncUseCase, objects objectFetcher, gate refreshGate) *AccountEventsUseCase {
	return &AccountEventsUseCase{sync: sync, objects: objects, gate: gate}
}

type accountWork struct {
	refs      map[ads.ObjectRef]bool
	structure bool
	skipped   map[ads.AccountEventKind]bool
}

func (uc *AccountEventsUseCase) Handle(ctx context.Context, changes []ads.AdAccountChange) error {
	work := map[string]*accountWork{}
	var order []string
	for _, change := range changes {
		metaID := ads.NormalizeAccountID(change.AccountMetaID)
		w, seen := work[metaID]
		if !seen {
			w = &accountWork{refs: map[ads.ObjectRef]bool{}, skipped: map[ads.AccountEventKind]bool{}}
			work[metaID] = w
			order = append(order, metaID)
		}
		switch {
		case change.Kind() != ads.AccountObjectsChanged:
			w.skipped[change.Kind()] = true
		case change.NeedsStructure():
			w.structure = true
		default:
			for _, ref := range change.Objects {
				w.refs[ref] = true
			}
		}
	}
	var failures []error
	for _, metaID := range order {
		if err := uc.handleAccount(ctx, metaID, work[metaID]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (uc *AccountEventsUseCase) handleAccount(ctx context.Context, metaID string, w *accountWork) error {
	for kind := range w.skipped {
		log.Printf("[ads] %s event for ad account %s has no use in Vozko yet; acknowledged", kind, metaID)
	}
	if len(w.refs) == 0 && !w.structure {
		return nil
	}
	account, err := uc.sync.access.accounts.FindByMetaAccountID(ctx, metaID)
	if errors.Is(err, ads.ErrAccountNotFound) {
		log.Printf("[ads] webhook for ad account %s that no workspace has connected; ignored", metaID)
		return nil
	}
	if err != nil {
		return err
	}
	token, err := uc.sync.access.tokenFor(ctx, account, ads.UseRead)
	if err != nil {
		return err
	}
	structure := w.structure
	for ref := range w.refs {
		gone, err := uc.refreshObject(ctx, account, token, ref)
		if err != nil {
			return uc.sync.access.failed(ctx, account, err)
		}
		structure = structure || gone
	}
	if !structure {
		return nil
	}
	return uc.refreshStructure(ctx, account, token)
}

func (uc *AccountEventsUseCase) refreshObject(ctx context.Context, account *ads.AdAccount, token string, ref ads.ObjectRef) (bool, error) {
	object, err := uc.objects.GetObject(ctx, token, ref.MetaID, ref.Level)
	if ads.Classify(err) == ads.FailureRejected {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("ads: refresh %s %s: %w", ref.Level, ref.MetaID, err)
	}
	object.WorkspaceID = account.WorkspaceID
	object.AdAccountID = account.ID
	object.Level = ref.Level
	object.SyncedAt = uc.sync.access.now()
	return false, uc.sync.objects.Upsert(ctx, object)
}

func (uc *AccountEventsUseCase) refreshStructure(ctx context.Context, account *ads.AdAccount, token string) error {
	acquired, err := uc.gate.SetNX("ads:structure-refresh:"+account.ID, "1", structureRefreshWindow)
	if err != nil {
		return fmt.Errorf("ads: structure refresh lock of %s: %w", account.ID, err)
	}
	if !acquired {
		return nil
	}
	if err := uc.sync.SyncStructure(ctx, account, token); err != nil {
		return uc.sync.access.failed(ctx, account, err)
	}
	return nil
}
