package emailapi_usecase

import (
	"context"
	"errors"
	"sync"

	"vozko/domain/emailapi"
	tx "vozko/usecases/transactionalemail"
)

var ErrNotBound = errors.New("emailapi: the email API is not running in this process")

type Sender interface {
	Send(ctx context.Context, req emailapi.Request) (Result, error)
}

type Relay struct {
	mu     sync.RWMutex
	target Sender
}

var _ Sender = (*Relay)(nil)

func NewRelay() *Relay { return &Relay{} }

func (r *Relay) Bind(target Sender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = target
}

func (r *Relay) Send(ctx context.Context, req emailapi.Request) (Result, error) {
	r.mu.RLock()
	target := r.target
	r.mu.RUnlock()
	if target == nil {
		return Result{}, tx.Refuse(tx.CodeUnavailable, ErrNotBound)
	}
	return target.Send(ctx, req)
}
