package emailsending

import (
	"context"
	"sort"
	"sync"
	"time"

	es "vozko/domain/emailsending"
)

func (m *memAccounts) ListByStatus(_ context.Context, status es.AccountStatus) ([]es.SenderAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []es.SenderAccount
	for _, a := range m.rows {
		if a.Status == status {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out, nil
}

type fakeTenants struct {
	mu         sync.Mutex
	retireErr  error
	reviveErr  error
	retired    []es.TenantResources
	revived    []es.TenantResources
	reviveSeq  int
	reviveWait time.Duration
}

func (f *fakeTenants) RetireTenant(_ context.Context, tenant es.TenantResources) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.retireErr != nil {
		return f.retireErr
	}
	f.retired = append(f.retired, tenant)
	return nil
}

func (f *fakeTenants) ReviveTenant(_ context.Context, tenant es.TenantResources) (es.Tenant, error) {
	time.Sleep(f.reviveWait)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reviveErr != nil {
		return es.Tenant{}, f.reviveErr
	}
	f.reviveSeq++
	f.revived = append(f.revived, tenant)
	return es.Tenant{Name: tenant.Name, Ref: "arn:tenant/" + tenant.Name + "/revived"}, nil
}

func (f *fakeTenants) revivals() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.revived)
}

type fakeActivity struct {
	lastSent    map[string]time.Time
	sharedSends map[string]bool
	err         error
}

func (f *fakeActivity) SentFromSharedDomains(_ context.Context, workspaceID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.sharedSends[workspaceID], nil
}

func (f *fakeActivity) LastSentAt(_ context.Context, workspaceID string) (*time.Time, error) {
	if f.err != nil {
		return nil, f.err
	}
	at, ok := f.lastSent[workspaceID]
	if !ok {
		return nil, nil
	}
	return &at, nil
}

type fakeLive struct {
	live map[string]bool
	err  error
}

func (f *fakeLive) HasLiveCampaign(_ context.Context, workspaceID string) (bool, error) {
	return f.live[workspaceID], f.err
}

type memLocks struct {
	mu   sync.Mutex
	held map[string]chan struct{}
	err  error
}

func (l *memLocks) Acquire(ctx context.Context, key string) (func() (bool, error), error) {
	if l.err != nil {
		return nil, l.err
	}
	for {
		l.mu.Lock()
		wait, taken := l.held[key]
		if !taken {
			mine := make(chan struct{})
			l.held[key] = mine
			l.mu.Unlock()
			return func() (bool, error) {
				l.mu.Lock()
				defer l.mu.Unlock()
				if l.held[key] != mine {
					return false, nil
				}
				delete(l.held, key)
				close(mine)
				return true, nil
			}, nil
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
		}
	}
}
