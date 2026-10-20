package oauthstate

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStrings struct {
	mu     sync.Mutex
	values map[string]string
	fail   error
}

func newMemoryStrings() *memoryStrings { return &memoryStrings{values: map[string]string{}} }

func (m *memoryStrings) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return false, m.fail
	}
	if _, ok := m.values[key]; ok {
		return false, nil
	}
	m.values[key] = value
	return true, nil
}

func (m *memoryStrings) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return "", m.fail
	}
	return m.values[key], nil
}

func (m *memoryStrings) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.values[key]
	return ok, nil
}

func (m *memoryStrings) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

func newSealedIssuer(t *testing.T, kv *memoryStrings) *SealedIssuer {
	t.Helper()
	nonces, err := NewNonceStore(kv, "ml:oauth")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewIssuer(testSecret, nonces, "/dashboard/mercadolivre-accounts")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := NewSealedIssuer(issuer, kv, "ml:oauth")
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestSealedValueNeverTravelsInTheState(t *testing.T) {
	kv := newMemoryStrings()
	sealed := newSealedIssuer(t, kv)
	secret := "pkce-verifier-that-must-stay-on-the-server-0123456789"
	raw, err := sealed.Issue(IssueInput{WorkspaceID: "ws-1", UserID: "u-1", Popup: true}, secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secret) {
		t.Fatal("the sealed value leaked into the state")
	}
	state, value, err := sealed.Redeem(raw)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if value != secret || state.WorkspaceID != "ws-1" || state.UserID != "u-1" || !state.Popup {
		t.Fatalf("state=%+v value=%q", state, value)
	}
	if _, _, err := sealed.Redeem(raw); !errors.Is(err, ErrReplayedState) {
		t.Fatalf("replay = %v", err)
	}
}

func TestSealedRedeemRefusesAStateWithoutItsValue(t *testing.T) {
	kv := newMemoryStrings()
	sealed := newSealedIssuer(t, kv)
	raw, err := sealed.Issue(IssueInput{WorkspaceID: "ws-1"}, "value")
	if err != nil {
		t.Fatal(err)
	}
	for k := range kv.values {
		if strings.Contains(k, ":sealed:") {
			delete(kv.values, k)
		}
	}
	if _, _, err := sealed.Redeem(raw); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}

func TestSealedIssueRefusesAnEmptyValueAndAStoreFailure(t *testing.T) {
	kv := newMemoryStrings()
	sealed := newSealedIssuer(t, kv)
	if _, err := sealed.Issue(IssueInput{WorkspaceID: "ws-1"}, " "); err == nil {
		t.Fatal("an empty sealed value was accepted")
	}
	kv.fail = errors.New("redis down")
	if _, err := sealed.Issue(IssueInput{WorkspaceID: "ws-1"}, "value"); err == nil {
		t.Fatal("a store failure must refuse the state")
	}
}

func TestNewSealedIssuerRequiresItsParts(t *testing.T) {
	kv := newMemoryStrings()
	nonces, _ := NewNonceStore(kv, "ml:oauth")
	issuer, _ := NewIssuer(testSecret, nonces, "/x")
	if _, err := NewSealedIssuer(nil, kv, "p"); err == nil {
		t.Fatal("nil issuer accepted")
	}
	if _, err := NewSealedIssuer(issuer, nil, "p"); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := NewSealedIssuer(issuer, kv, " "); err == nil {
		t.Fatal("empty prefix accepted")
	}
}
