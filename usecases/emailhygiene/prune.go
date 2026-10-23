package emailhygiene

import (
	"context"
	"errors"
	"time"

	h "vozko/domain/emailhygiene"
)

const (
	PruneBatch      = 5000
	PruneMaxBatches = 100
)

type pruneStore interface {
	Prune(ctx context.Context, before time.Time, limit int) (int64, error)
}

type PrunerDeps struct {
	Domains  pruneStore
	Activity pruneStore
	Lists    h.DisposableListStore
}

type Pruner struct {
	d   PrunerDeps
	now func() time.Time
}

func NewPruner(d PrunerDeps) *Pruner {
	return &Pruner{d: d, now: func() time.Time { return time.Now().UTC() }}
}

func (p *Pruner) Execute(ctx context.Context) error {
	now := p.now()
	var errs []error
	errs = append(errs, drain(ctx, p.d.Domains, now.AddDate(0, 0, -h.DomainCheckIdleDays)))
	errs = append(errs, drain(ctx, p.d.Activity, now.Add(-h.ActivityRetention)))
	if _, err := p.d.Lists.KeepNewest(ctx, KeepDisposableLists); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func drain(ctx context.Context, store pruneStore, before time.Time) error {
	for i := 0; i < PruneMaxBatches; i++ {
		n, err := store.Prune(ctx, before, PruneBatch)
		if err != nil || n < PruneBatch {
			return err
		}
	}
	return nil
}
