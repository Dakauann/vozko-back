package whatsapp

import (
	"sync"

	"vozko/domain/conversation"
)

type InMemoryCallRegistry struct {
	mu        sync.Mutex
	listeners map[string]chan conversation.WhatsAppCallSignal
}

func NewInMemoryCallRegistry() *InMemoryCallRegistry {
	return &InMemoryCallRegistry{
		listeners: make(map[string]chan conversation.WhatsAppCallSignal),
	}
}

func (r *InMemoryCallRegistry) Register(callID string) <-chan conversation.WhatsAppCallSignal {
	ch := make(chan conversation.WhatsAppCallSignal, 8)
	r.mu.Lock()
	r.listeners[callID] = ch
	r.mu.Unlock()
	return ch
}

func (r *InMemoryCallRegistry) Deliver(signal conversation.WhatsAppCallSignal) {
	r.mu.Lock()
	ch, ok := r.listeners[signal.CallID]
	r.mu.Unlock()
	if !ok {
		return
	}

	select {
	case ch <- signal:
	default:
	}
}

func (r *InMemoryCallRegistry) Unregister(callID string) {
	r.mu.Lock()
	if ch, ok := r.listeners[callID]; ok {
		delete(r.listeners, callID)
		close(ch)
	}
	r.mu.Unlock()
}

var _ conversation.WhatsAppCallRegistry = (*InMemoryCallRegistry)(nil)
