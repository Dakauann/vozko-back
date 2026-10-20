package mercadolivre

import (
	"context"
	"log"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/pkg/bounded"
)

const (
	reconcileEvery       = 10 * time.Minute
	reconcilePage        = 200
	reconcileConcurrency = 8
	reconcileMaxAccounts = 20000
)

type Reconciler struct {
	accounts    mldomain.AccountRepository
	tokens      *TokenSource
	metrics     Metrics
	reconcilers []AccountReconciler
	now         Clock
}

func NewReconciler(accounts mldomain.AccountRepository, tokens *TokenSource, metrics Metrics, reconcilers ...AccountReconciler) *Reconciler {
	if accounts == nil || tokens == nil || metrics == nil || len(reconcilers) == 0 {
		panic("mercadolivre: the reconciler requires accounts, tokens, metrics and at least one account reconciler")
	}
	for _, r := range reconcilers {
		if r == nil {
			panic("mercadolivre: nil account reconciler")
		}
	}
	return &Reconciler{accounts: accounts, tokens: tokens, metrics: metrics, reconcilers: reconcilers, now: utcNow}
}

func (r *Reconciler) Execute(ctx context.Context) error {
	ctx = mldomain.WithBackgroundPriority(ctx)
	now := r.now()
	marker := "reconcile:" + now.Format("200601021504")
	seen := map[string]bool{}
	for len(seen) < reconcileMaxAccounts {
		due, err := r.accounts.ListDueForReconcile(ctx, now.Add(-reconcileEvery), reconcilePage)
		if err != nil {
			return err
		}
		var fresh []*mldomain.Account
		for _, a := range due {
			if !seen[a.ID] {
				seen[a.ID] = true
				fresh = append(fresh, a)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		bounded.ForEach(ctx, fresh, reconcileConcurrency, func(ctx context.Context, a *mldomain.Account) {
			r.reconcileOne(ctx, a, marker)
		})
		if len(due) < reconcilePage {
			return nil
		}
	}
	return nil
}

func (r *Reconciler) reconcileOne(ctx context.Context, account *mldomain.Account, marker string) {
	defer func() {
		if err := r.accounts.MarkReconciled(ctx, account.ID, r.now()); err != nil {
			log.Printf("[mercadolivre][reconcile] account %s not marked: %v", account.ID, err)
		}
	}()
	creds, err := r.tokens.Credentials(ctx, account)
	if err != nil {
		r.metrics.AddMercadoLivreEvent("reconcile", "failed")
		log.Printf("[mercadolivre][reconcile] account %s has no usable token: %v", account.ID, err)
		return
	}
	for _, rec := range r.reconcilers {
		published, err := rec.Reconcile(ctx, account, creds, marker)
		if err != nil {
			r.metrics.AddMercadoLivreEvent("reconcile", "failed")
			log.Printf("[mercadolivre][reconcile] account %s: %v", account.ID, err)
			continue
		}
		for i := 0; i < published; i++ {
			r.metrics.AddMercadoLivreEvent("reconcile", "published")
		}
	}
}

type UnreadThreadsReconciler struct {
	messages  mldomain.MessageService
	publisher *JobPublisher
}

func NewUnreadThreadsReconciler(messages mldomain.MessageService, publisher *JobPublisher) *UnreadThreadsReconciler {
	if messages == nil || publisher == nil {
		panic("mercadolivre: the unread reconciler requires messages and the job publisher")
	}
	return &UnreadThreadsReconciler{messages: messages, publisher: publisher}
}

func (u *UnreadThreadsReconciler) Reconcile(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, marker string) (int, error) {
	unread, err := u.messages.Unread(ctx, creds)
	if err != nil {
		return 0, err
	}
	published := 0
	for _, t := range unread {
		if t.SellerID != account.MLUserID {
			continue
		}
		job, err := mldomain.ThreadJob(account.MLUserID, t.PackKey, marker)
		if err != nil {
			log.Printf("[mercadolivre][reconcile] unread thread %q of %s skipped: %v", t.PackKey, account.ID, err)
			continue
		}
		if err := u.publisher.Publish(job); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}
