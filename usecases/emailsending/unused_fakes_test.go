package emailsending

import (
	"context"
	"sort"
	"sync"
	"time"

	es "vozko/domain/emailsending"
)

type memUnused struct {
	mu          sync.Mutex
	accounts    *memAccounts
	domains     *memDomains
	senders     *memSenders
	used        map[string]bool
	useOnForget map[string]bool
	listErr     error
	forgotten   []string
	pages       int
}

func (m *memUnused) ListUnusedCandidates(_ context.Context, activatedBefore time.Time, afterID string, limit int) ([]es.SenderAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pages++
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.accounts.mu.Lock()
	var out []es.SenderAccount
	for _, a := range m.accounts.rows {
		old := !a.CreatedAt.After(activatedBefore) && (a.Status == es.AccountActive || a.Status == es.AccountDormant) && !m.used[a.WorkspaceID]
		if (a.UnusedWarnedAt != nil || old) && a.ID > afterID {
			out = append(out, a)
		}
	}
	m.accounts.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memUnused) InUse(_ context.Context, workspaceID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used[workspaceID], nil
}

func (m *memUnused) Forget(_ context.Context, workspaceID, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.useOnForget[workspaceID] {
		m.used[workspaceID] = true
	}
	if m.used[workspaceID] {
		return es.ErrAccountInUse
	}
	m.accounts.mu.Lock()
	delete(m.accounts.rows, accountID)
	m.accounts.mu.Unlock()
	m.domains.mu.Lock()
	for id, d := range m.domains.rows {
		if d.WorkspaceID == workspaceID {
			delete(m.domains.rows, id)
		}
	}
	m.domains.mu.Unlock()
	m.senders.mu.Lock()
	for id, s := range m.senders.rows {
		if s.WorkspaceID == workspaceID {
			delete(m.senders.rows, id)
		}
	}
	m.senders.mu.Unlock()
	m.forgotten = append(m.forgotten, workspaceID)
	return nil
}
