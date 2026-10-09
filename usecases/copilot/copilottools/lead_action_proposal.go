package copilottools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"vozko/domain/copilot"
)

type cardRecord struct {
	Expected    int    `json:"e"`
	Fingerprint string `json:"f"`
	PreviewID   string `json:"p,omitempty"`
	Args        string `json:"a"`
}

func cardKey(cc copilot.Context) string {
	return "copilot:lead-action:card:" + digest(cc.WorkspaceID+"|"+cc.UserID+"|"+cc.ProposalID)
}

func argsDigest(args map[string]interface{}) string {
	raw, err := json.Marshal(args)
	if err != nil {
		raw = []byte(err.Error())
	}
	return digest(string(raw))
}

func memoKey(cc copilot.Context, args map[string]interface{}) string {
	view, _ := json.Marshal(cc.View.LeadFilter)
	return digest(cc.WorkspaceID + "|" + cc.UserID + "|" + cc.ProposalID + "|" + argsDigest(args) + "|" + string(view))
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type proposalMemo[T any] struct {
	mu      sync.Mutex
	now     func() time.Time
	clone   func(T) T
	entries map[string]memoEntry[T]
}

type memoEntry[T any] struct {
	value T
	at    time.Time
}

func newProposalMemo[T any](now func() time.Time, clone func(T) T) *proposalMemo[T] {
	return &proposalMemo[T]{now: now, clone: clone, entries: map[string]memoEntry[T]{}}
}

func (m *proposalMemo[T]) get(key string) (T, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok || m.now().Sub(entry.at) > previewMemoRetention {
		delete(m.entries, key)
		var none T
		return none, false
	}
	return m.clone(entry.value), true
}

func (m *proposalMemo[T]) put(key string, value T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for k, entry := range m.entries {
		if now.Sub(entry.at) > previewMemoRetention {
			delete(m.entries, k)
		}
	}
	if len(m.entries) >= previewMemoCapacity {
		m.entries = map[string]memoEntry[T]{}
	}
	m.entries[key] = memoEntry[T]{value: m.clone(value), at: now}
}
