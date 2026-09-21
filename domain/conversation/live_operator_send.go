package conversation

import (
	"context"
	"errors"
	"sync"
)

var ErrOperatorSendUnavailable = errors.New("conversation: operator send is not available yet")

type LiveOperatorSend struct {
	mu    sync.RWMutex
	inner OperatorSendUseCase
}

func NewLiveOperatorSend() *LiveOperatorSend {
	return &LiveOperatorSend{}
}

func (l *LiveOperatorSend) Use(inner OperatorSendUseCase) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inner = inner
}

func (l *LiveOperatorSend) Execute(ctx context.Context, in OperatorSendInput) (*Message, error) {
	l.mu.RLock()
	inner := l.inner
	l.mu.RUnlock()

	if inner == nil {
		return nil, ErrOperatorSendUnavailable
	}
	return inner.Execute(ctx, in)
}

var _ OperatorSendUseCase = (*LiveOperatorSend)(nil)
