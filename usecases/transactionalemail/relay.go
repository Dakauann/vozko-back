package transactionalemail

import (
	"context"
	"sync"
)

type Relay struct {
	mu     sync.RWMutex
	target Sender
}

var _ Sender = (*Relay)(nil)

func NewRelay() *Relay {
	return &Relay{}
}

func (r *Relay) Bind(target Sender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = target
}

func (r *Relay) Send(ctx context.Context, req Request) (Result, error) {
	r.mu.RLock()
	target := r.target
	r.mu.RUnlock()
	if target == nil {
		return Result{}, refuse(CodeUnavailable, ErrUnavailable)
	}
	return target.Send(ctx, req)
}
