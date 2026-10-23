package googleaccount

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/googleaccount"
	"vozko/usecases/shared/oauthstate"
)

type fakeRepo struct {
	mu       sync.Mutex
	accounts map[string]*googleaccount.Account
	bindings map[string]googleaccount.Binding
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{accounts: map[string]*googleaccount.Account{}, bindings: map[string]googleaccount.Binding{}}
}

func bindingKey(ws string, p googleaccount.Product) string { return ws + "|" + string(p) }

func cloneAccount(a *googleaccount.Account) *googleaccount.Account {
	c := *a
	c.Scopes = slices.Clone(a.Scopes)
	return &c
}

func (r *fakeRepo) SaveConnected(_ context.Context, a *googleaccount.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email := googleaccount.NormalizeEmail(a.Email)
	for _, existing := range r.accounts {
		if existing.WorkspaceID == a.WorkspaceID && existing.Email == email {
			a.ID = existing.ID
		}
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.Email = email
	a.Status = googleaccount.StatusConnected
	a.UpdatedAt = time.Now().UTC()
	r.accounts[a.ID] = cloneAccount(a)
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id string) (*googleaccount.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok {
		return nil, googleaccount.ErrAccountNotFound
	}
	return cloneAccount(a), nil
}

func (r *fakeRepo) FindByEmail(_ context.Context, ws, email string) (*googleaccount.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.accounts {
		if a.WorkspaceID == ws && a.Email == googleaccount.NormalizeEmail(email) {
			return cloneAccount(a), nil
		}
	}
	return nil, googleaccount.ErrAccountNotFound
}

func (r *fakeRepo) FindBound(ctx context.Context, ws string, p googleaccount.Product) (*googleaccount.Account, error) {
	r.mu.Lock()
	b, ok := r.bindings[bindingKey(ws, p)]
	r.mu.Unlock()
	if !ok {
		return nil, googleaccount.ErrNotConnected
	}
	return r.FindByID(ctx, b.AccountID)
}

func (r *fakeRepo) ListBound(ctx context.Context, ws string) ([]googleaccount.BoundAccount, error) {
	var out []googleaccount.BoundAccount
	for _, p := range googleaccount.Products() {
		if a, err := r.FindBound(ctx, ws, p); err == nil {
			out = append(out, googleaccount.BoundAccount{Product: p, Account: a})
		}
	}
	return out, nil
}

func (r *fakeRepo) Bind(_ context.Context, b googleaccount.Binding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[bindingKey(b.WorkspaceID, b.Product)] = b
	return nil
}

func (r *fakeRepo) Unbind(_ context.Context, ws string, p googleaccount.Product) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bindings[bindingKey(ws, p)]
	if !ok {
		return "", googleaccount.ErrNotConnected
	}
	delete(r.bindings, bindingKey(ws, p))
	return b.AccountID, nil
}

func (r *fakeRepo) CountBindings(_ context.Context, accountID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, b := range r.bindings {
		if b.AccountID == accountID {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) Disconnect(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	a.Status = googleaccount.StatusDisconnected
	a.AccessToken, a.RefreshToken = "", ""
	return nil
}

func (r *fakeRepo) ConnectedElsewhere(_ context.Context, email, exceptID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.accounts {
		if a.ID != exceptID && a.Email == googleaccount.NormalizeEmail(email) && a.Status != googleaccount.StatusDisconnected {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) ListDueForRefresh(_ context.Context, before time.Time, limit int) ([]*googleaccount.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*googleaccount.Account
	for _, a := range r.accounts {
		if a.Status == googleaccount.StatusConnected && a.AccessExpiresAt.Before(before) {
			out = append(out, cloneAccount(a))
		}
	}
	return out, nil
}

func (r *fakeRepo) LockForRefresh(ctx context.Context, id string, refresh googleaccount.RefreshFunc) (bool, error) {
	locked, err := r.FindByID(ctx, id)
	if err != nil {
		return false, nil
	}
	outcome, err := refresh(ctx, locked)
	if err != nil {
		return true, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	if outcome.Status != "" {
		a.Status, a.StatusReason = outcome.Status, outcome.StatusReason
		if outcome.Status != googleaccount.StatusConnected {
			a.AccessToken, a.RefreshToken = "", ""
		}
	}
	if t := outcome.Tokens; t != nil {
		a.AccessToken, a.RefreshToken, a.AccessExpiresAt, a.Scopes = t.AccessToken, t.RefreshToken, t.AccessExpiresAt, t.Scopes
	}
	return true, nil
}

type fakeOAuth struct {
	grant       googleaccount.Grant
	exchangeErr error
	refresh     googleaccount.Grant
	refreshErr  error
	profile     googleaccount.Profile
	revoked     []string
	refreshes   int
}

func (o *fakeOAuth) AuthorizeURL(state string, scopes []string) string {
	return "https://accounts.example.com/auth?state=" + state
}

func (o *fakeOAuth) ExchangeCode(context.Context, string) (googleaccount.Grant, error) {
	return o.grant, o.exchangeErr
}

func (o *fakeOAuth) Refresh(context.Context, string) (googleaccount.Grant, error) {
	o.refreshes++
	return o.refresh, o.refreshErr
}

func (o *fakeOAuth) Profile(context.Context, string) (googleaccount.Profile, error) {
	return o.profile, nil
}

func (o *fakeOAuth) Revoke(_ context.Context, token string) error {
	o.revoked = append(o.revoked, token)
	return nil
}

var errAuth = errors.New("invalid_grant")

type fakeClassifier struct{}

func (fakeClassifier) IsAuth(err error) bool { return errors.Is(err, errAuth) }

type fakeAlerts struct {
	needsReconnect [][]googleaccount.Product
	resolved       []googleaccount.Product
}

func (a *fakeAlerts) NeedsReconnect(_ context.Context, _ *googleaccount.Account, products []googleaccount.Product) error {
	a.needsReconnect = append(a.needsReconnect, products)
	return nil
}

func (a *fakeAlerts) Reconnected(_ context.Context, _ *googleaccount.Account, p googleaccount.Product) error {
	a.resolved = append(a.resolved, p)
	return nil
}

type fakeHooks struct {
	connected    []string
	disconnected []string
}

func (h *fakeHooks) AfterConnect(_ context.Context, ws string) error {
	h.connected = append(h.connected, ws)
	return nil
}

func (h *fakeHooks) BeforeDisconnect(_ context.Context, ws string) error {
	h.disconnected = append(h.disconnected, ws)
	return nil
}

type memoryStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemoryStore() *memoryStore { return &memoryStore{data: map[string]string{}} }

func (m *memoryStore) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; ok {
		return false, nil
	}
	m.data[key] = value
	return true, nil
}

func (m *memoryStore) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok, nil
}

func (m *memoryStore) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

func (m *memoryStore) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}

func newSealedIssuer() *oauthstate.SealedIssuer {
	store := newMemoryStore()
	nonces, err := oauthstate.NewNonceStore(store, "google:oauth")
	if err != nil {
		panic(err)
	}
	issuer, err := oauthstate.NewIssuer("secret", nonces, IntegrationsPath)
	if err != nil {
		panic(err)
	}
	sealed, err := oauthstate.NewSealedIssuer(issuer, store, "google:oauth")
	if err != nil {
		panic(err)
	}
	return sealed
}
