package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
)

const (
	Exchange   = "report_generation_exchange"
	QueueTopic = "report.generate"
)

type Registry struct {
	mu        sync.RWMutex
	renderers map[Kind]Renderer
}

func NewRegistry() *Registry {
	return &Registry{renderers: make(map[Kind]Renderer, 8)}
}

func (r *Registry) Register(renderer Renderer) {
	if r == nil || renderer == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderers[renderer.Kind()] = renderer
}

func (r *Registry) Lookup(kind Kind) (Renderer, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	renderer, found := r.renderers[kind]
	return renderer, found && renderer != nil
}

type KindDescriptor struct {
	Kind    Kind     `json:"kind"`
	Formats []Format `json:"formats"`
}

func (r *Registry) Descriptors() []KindDescriptor {
	if r == nil {
		return []KindDescriptor{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]KindDescriptor, 0, len(r.renderers))
	for kind, renderer := range r.renderers {
		out = append(out, KindDescriptor{Kind: kind, Formats: renderer.Formats()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

func Fingerprint(workspaceID string, kind Kind, format Format, locale string, params json.RawMessage) string {
	canonical := canonicalJSON(params)
	sum := sha256.Sum256([]byte(workspaceID + "|" + string(kind) + "|" + string(format) + "|" + locale + "|" + canonical))
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return string(raw)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

type QueueMessage struct {
	JobID       string `json:"jobId"`
	WorkspaceID string `json:"workspaceId"`
	Kind        Kind   `json:"kind"`
}
