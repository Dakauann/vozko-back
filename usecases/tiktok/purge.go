package tiktok

import (
	"context"
	"errors"
	"log"

	ca "vozko/domain/audience"
	rules "vozko/domain/commentautomation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
)

const (
	purgeAccountsPerRun = 20
	purgeBatch          = 500
	purgeMaxBatches     = 2000
)

var (
	errPurgeCancelled  = errors.New("tiktok: the account is no longer due for purge")
	errPurgeIncomplete = errors.New("tiktok: the purge reached its batch bound, it resumes on the next run")
)

type EntryPurger interface {
	PurgeEntries(ctx context.Context, entryType shared.EntryType, entryIDs []string) error
	PurgeContacts(ctx context.Context, contactType shared.EntryType, contactIDs []string) error
	DeleteFiles(ctx context.Context, keys []string) error
}

type PurgeDeps struct {
	Accounts ttdomain.AccountRepository
	Purge    ttdomain.PurgeRepository
	Entries  EntryPurger
	Rules    rules.AccountPurger
	Audience ca.AccountPurger
	Metrics  Metrics
}

type PurgeJob struct {
	d   PurgeDeps
	now Clock
}

func NewPurgeJob(d PurgeDeps) *PurgeJob {
	if d.Accounts == nil || d.Purge == nil || d.Entries == nil || d.Rules == nil || d.Audience == nil || d.Metrics == nil {
		panic("tiktok: the purge requires accounts, the purge repository, the entry purge, the rule and audience purges and metrics")
	}
	return &PurgeJob{d: d, now: utcNow}
}

func (j *PurgeJob) Execute(ctx context.Context) error {
	due, err := j.d.Accounts.ListDueForPurge(ctx, j.now(), purgeAccountsPerRun)
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
			j.d.Metrics.AddTikTokEvent("purge", "account_purged")
			log.Printf("[tiktok][purge] account %s and all its data were deleted", account.ID)
		case errors.Is(err, errPurgeCancelled):
			j.d.Metrics.AddTikTokEvent("purge", "cancelled")
			log.Printf("[tiktok][purge] account %s was reconnected, purge stopped", account.ID)
		case errors.Is(err, errPurgeIncomplete):
			j.d.Metrics.AddTikTokEvent("purge", "resumed_later")
		default:
			j.d.Metrics.AddTikTokEvent("purge", "failed")
			log.Printf("[tiktok][purge] account %s stopped, it resumes on the next run: %v", account.ID, err)
		}
	}
	return nil
}

type purgeRun struct {
	job     *PurgeJob
	account *ttdomain.Account
	batches int
}

func (j *PurgeJob) purgeAccount(ctx context.Context, account *ttdomain.Account) error {
	run := &purgeRun{job: j, account: account}
	for _, step := range []func(context.Context) (bool, error){
		run.conversations, run.commentSource, run.storedObjects, run.accountRows, run.contacts,
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
	return j.d.Accounts.DeletePurged(ctx, account.ID)
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
	fresh, err := r.job.d.Accounts.FindByWorkspaceAndOpenIDUnscoped(ctx, r.account.WorkspaceID, r.account.OpenID)
	if errors.Is(err, ttdomain.ErrAccountNotFound) {
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
	ids, err := r.job.d.Purge.ConversationIDsForPurge(ctx, r.account.ID, purgeBatch)
	if err != nil || len(ids) == 0 {
		return true, err
	}
	if err := r.job.d.Entries.PurgeEntries(ctx, shared.EntryTypeTikTok, ids); err != nil {
		return false, err
	}
	return false, r.job.d.Purge.DeleteConversations(ctx, r.account.ID, ids)
}

func (r *purgeRun) commentSource(ctx context.Context) (bool, error) {
	if err := r.job.d.Rules.PurgeAccount(ctx, shared.EntryTypeTikTok, r.account.ID); err != nil {
		return false, err
	}
	return true, r.job.d.Audience.PurgeAccount(ctx, ca.SourceTikTok, r.account.ID)
}

func (r *purgeRun) storedObjects(ctx context.Context) (bool, error) {
	keys, err := r.job.d.Purge.StoredObjectKeys(ctx, r.account.ID, purgeBatch)
	if err != nil || len(keys) == 0 {
		return true, err
	}
	if err := r.job.d.Entries.DeleteFiles(ctx, keys); err != nil {
		return false, err
	}
	return false, r.job.d.Purge.ForgetObjectKeys(ctx, r.account.ID, keys)
}

func (r *purgeRun) accountRows(ctx context.Context) (bool, error) {
	return r.job.d.Purge.DeleteAccountRows(ctx, r.account.ID, purgeBatch)
}

func (r *purgeRun) contacts(ctx context.Context) (bool, error) {
	ids, err := r.job.d.Purge.ContactIDsForPurge(ctx, r.account.ID, purgeBatch)
	if err != nil || len(ids) == 0 {
		return true, err
	}
	if err := r.job.d.Entries.PurgeContacts(ctx, shared.EntryTypeTikTok, ids); err != nil {
		return false, err
	}
	return false, r.job.d.Purge.DeleteContacts(ctx, r.account.ID, ids)
}
