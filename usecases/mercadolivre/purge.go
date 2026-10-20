package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"log"

	ca "vozko/domain/commentautomation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

const (
	purgeBatch          = 500
	purgeAccountsPerRun = 50
	purgeMaxBatches     = 2000
)

var (
	errPurgeCancelled  = errors.New("mercadolivre: the account is no longer due for purge")
	errPurgeIncomplete = errors.New("mercadolivre: the purge reached its batch bound, it resumes on the next run")
)

type EntryPurger interface {
	PurgeEntries(ctx context.Context, entryType shared.EntryType, entryIDs []string) error
	PurgeContacts(ctx context.Context, contactType shared.EntryType, contactIDs []string) error
	DeleteFiles(ctx context.Context, keys []string) error
}

type PurgeDeps struct {
	Accounts mldomain.AccountRepository
	Purge    mldomain.PurgeRepository
	Entries  EntryPurger
	Rules    ca.AccountPurger
	Metrics  Metrics
}

type PurgeJob struct {
	accounts mldomain.AccountRepository
	purge    mldomain.PurgeRepository
	entries  EntryPurger
	rules    ca.AccountPurger
	metrics  Metrics
	batch    int
	now      Clock
}

func NewPurgeJob(d PurgeDeps) *PurgeJob {
	if d.Accounts == nil || d.Purge == nil || d.Entries == nil || d.Rules == nil || d.Metrics == nil {
		panic("mercadolivre: the purge requires accounts, the purge repository, the entry purge, the comment rules purge and metrics")
	}
	return &PurgeJob{accounts: d.Accounts, purge: d.Purge, entries: d.Entries, rules: d.Rules, metrics: d.Metrics, batch: purgeBatch, now: utcNow}
}

func (j *PurgeJob) Execute(ctx context.Context) error {
	due, err := j.accounts.ListDueForPurge(ctx, j.now(), purgeAccountsPerRun)
	if err != nil {
		return err
	}
	for _, account := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := j.purgeAccount(ctx, account)
		switch {
		case err == nil:
			j.metrics.AddMercadoLivreEvent("purge", "account_purged")
			log.Printf("[mercadolivre][purge] account %s and all its data were deleted", account.ID)
		case errors.Is(err, errPurgeCancelled):
			j.metrics.AddMercadoLivreEvent("purge", "cancelled")
			log.Printf("[mercadolivre][purge] account %s was reconnected, purge stopped", account.ID)
		case errors.Is(err, errPurgeIncomplete):
			j.metrics.AddMercadoLivreEvent("purge", "resumed_later")
		default:
			j.metrics.AddMercadoLivreEvent("purge", "failed")
			log.Printf("[mercadolivre][purge] account %s: %v", account.ID, err)
		}
	}
	return nil
}

type purgeRun struct {
	job     *PurgeJob
	account *mldomain.Account
	batches int
}

func (j *PurgeJob) purgeAccount(ctx context.Context, account *mldomain.Account) error {
	run := &purgeRun{job: j, account: account}
	for _, step := range []func(context.Context) (bool, error){
		run.conversations, run.questions, run.listings, run.listingSync, run.rules, run.contacts,
	} {
		for {
			done, err := run.next(ctx, step)
			if err != nil {
				return err
			}
			if done {
				break
			}
		}
	}
	if err := run.stillDue(ctx); err != nil {
		return err
	}
	return j.accounts.DeletePurged(ctx, account.ID)
}

func (r *purgeRun) next(ctx context.Context, step func(context.Context) (bool, error)) (bool, error) {
	if r.batches >= purgeMaxBatches {
		return false, errPurgeIncomplete
	}
	r.batches++
	if err := r.stillDue(ctx); err != nil {
		return false, err
	}
	return step(ctx)
}

func (r *purgeRun) stillDue(ctx context.Context) error {
	fresh, err := r.job.accounts.FindByWorkspaceAndMLUserIDUnscoped(ctx, r.account.WorkspaceID, r.account.MLUserID)
	if errors.Is(err, mldomain.ErrAccountNotFound) {
		return errPurgeCancelled
	}
	if err != nil {
		return err
	}
	if fresh.ID != r.account.ID || !fresh.PurgeDue(r.job.now()) {
		return errPurgeCancelled
	}
	return nil
}

func (r *purgeRun) conversations(ctx context.Context) (bool, error) {
	ids, err := r.job.purge.ConversationIDs(ctx, r.account.ID, r.job.batch)
	if err != nil || len(ids) == 0 {
		return true, err
	}
	if err := r.job.entries.PurgeEntries(ctx, shared.EntryTypeMercadoLivre, ids); err != nil {
		return false, fmt.Errorf("conversation data: %w", err)
	}
	keys, err := r.job.purge.PackThumbnailKeys(ctx, r.account.ID, ids)
	if err != nil {
		return false, fmt.Errorf("pack thumbnails: %w", err)
	}
	if len(keys) > 0 {
		if err := r.job.entries.DeleteFiles(ctx, keys); err != nil {
			return false, fmt.Errorf("pack thumbnails: %w", err)
		}
	}
	return false, r.job.purge.DeleteConversations(ctx, r.account.ID, ids)
}

func (r *purgeRun) questions(ctx context.Context) (bool, error) {
	deleted, err := r.job.purge.DeleteQuestions(ctx, r.account.ID, r.job.batch)
	return err == nil && deleted == 0, err
}

func (r *purgeRun) listings(ctx context.Context) (bool, error) {
	batch, err := r.job.purge.ListingThumbnails(ctx, r.account.ID, r.job.batch)
	if err != nil || len(batch) == 0 {
		return err == nil, err
	}
	keys := make([]string, 0, len(batch))
	items := make([]string, 0, len(batch))
	for _, l := range batch {
		items = append(items, l.ItemID)
		if l.Key != "" {
			keys = append(keys, l.Key)
		}
	}
	if len(keys) > 0 {
		if err := r.job.entries.DeleteFiles(ctx, keys); err != nil {
			return false, fmt.Errorf("listing thumbnails: %w", err)
		}
	}
	return false, r.job.purge.DeleteListings(ctx, r.account.ID, items)
}

func (r *purgeRun) listingSync(ctx context.Context) (bool, error) {
	return true, r.job.purge.DeleteListingSync(ctx, r.account.ID)
}

func (r *purgeRun) rules(ctx context.Context) (bool, error) {
	return true, r.job.rules.PurgeAccount(ctx, shared.EntryTypeMercadoLivre, r.account.ID)
}

func (r *purgeRun) contacts(ctx context.Context) (bool, error) {
	ids, err := r.job.purge.ContactIDs(ctx, r.account.ID, r.job.batch)
	if err != nil || len(ids) == 0 {
		return true, err
	}
	if err := r.job.entries.PurgeContacts(ctx, shared.EntryTypeMercadoLivre, ids); err != nil {
		return false, fmt.Errorf("contact data: %w", err)
	}
	return false, r.job.purge.DeleteContacts(ctx, r.account.ID, ids)
}
