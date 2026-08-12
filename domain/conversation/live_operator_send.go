package conversation

import (
	"context"
	"errors"
	"sync"
)

// ErrOperatorSendUnavailable means an operator message was submitted before the
// send use case finished wiring. It can only be seen during container startup.
var ErrOperatorSendUnavailable = errors.New("conversation: operator send is not available yet")

// LiveOperatorSend is an OperatorSendUseCase whose target is set once the
// container has built it.
//
// It exists because the object graph around sending has a genuine construction
// cycle: the WebSocket hub needs the send use case, the send use case needs the
// message sender, and the message sender needs the hub to broadcast through.
// Something in that ring must be late-bound.
//
// This is the same answer LiveAdapterRegistry gives one layer down, and for the
// same reason: making the late binding a single, purpose-built, nil-safe object
// is what lets every consumer in the ring take plain constructor injection. The
// alternative — a setter per consumer — spreads the same late binding across N
// call sites, each of which then has to nil-check, and any one of which can be
// forgotten silently.
//
// Before Use is called every Execute returns ErrOperatorSendUnavailable. It
// never returns a nil message with a nil error, so no caller can mistake
// "not wired yet" for "delivered".
type LiveOperatorSend struct {
	mu    sync.RWMutex
	inner OperatorSendUseCase
}

func NewLiveOperatorSend() *LiveOperatorSend {
	return &LiveOperatorSend{}
}

// Use points this at the real use case. Called once, by the container.
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
