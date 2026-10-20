package mercadolivre

import (
	"context"
	"errors"
	"log"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

const (
	missedFeedPages      = 20
	missedFeedCandidates = 10
)

type MissedFeedsJob struct {
	accounts mldomain.AccountRepository
	tokens   *TokenSource
	feeds    mldomain.MissedFeedService
	accept   *AcceptNotification
	now      Clock
}

func NewMissedFeedsJob(accounts mldomain.AccountRepository, tokens *TokenSource, feeds mldomain.MissedFeedService, accept *AcceptNotification) *MissedFeedsJob {
	if accounts == nil || tokens == nil || feeds == nil || accept == nil {
		panic("mercadolivre: the missed feeds job requires accounts, tokens, the missed feed service and the notification intake")
	}
	return &MissedFeedsJob{accounts: accounts, tokens: tokens, feeds: feeds, accept: accept, now: utcNow}
}

func (j *MissedFeedsJob) Execute(ctx context.Context) error {
	ctx = mldomain.WithBackgroundPriority(ctx)
	creds, err := j.anyCredentials(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, topic := range mldomain.SubscribedTopics() {
		if err := j.replay(ctx, creds, topic); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (j *MissedFeedsJob) anyCredentials(ctx context.Context) (mldomain.Credentials, error) {
	candidates, err := j.accounts.ListDueForReconcile(ctx, j.now().Add(24*time.Hour), missedFeedCandidates)
	if err != nil {
		return mldomain.Credentials{}, err
	}
	for _, a := range candidates {
		if creds, err := j.tokens.Credentials(ctx, a); err == nil {
			return creds, nil
		}
	}
	return mldomain.Credentials{}, ErrAccountUnavailable
}

func (j *MissedFeedsJob) replay(ctx context.Context, creds mldomain.Credentials, topic mldomain.Topic) error {
	for page := 0; page < missedFeedPages; page++ {
		feeds, err := j.feeds.MissedFeeds(ctx, creds, mldomain.MissedFeedQuery{
			AppID:  j.accept.appID,
			Topic:  topic,
			SiteID: mldomain.SiteMLB,
			Offset: page * mldomain.MaxMissedFeedLimit,
			Limit:  mldomain.MaxMissedFeedLimit,
		})
		if err != nil {
			return err
		}
		for _, n := range feeds {
			if err := j.accept.AcceptDecoded(n); err != nil {
				if errors.Is(err, ErrQueueUnavailable) {
					return err
				}
				log.Printf("[mercadolivre][reconcile] missed %s notification %q refused: %v", topic, n.ID, err)
			}
		}
		if len(feeds) < mldomain.MaxMissedFeedLimit {
			return nil
		}
	}
	return nil
}
