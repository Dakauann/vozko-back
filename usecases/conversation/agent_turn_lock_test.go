package conversation_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
)

type lockState struct {
	cache.SharedState
	mu    sync.Mutex
	held  map[string]string
	err   error
	order []string
}

func newLockState() *lockState { return &lockState{held: map[string]string{}} }

func (s *lockState) SetNX(key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	if _, taken := s.held[key]; taken {
		return false, nil
	}
	s.held[key] = value
	s.order = append(s.order, key)
	return true, nil
}

func (s *lockState) Del(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.held, k)
	}
	return nil
}

func (s *lockState) DelIfValue(key, value string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, taken := s.held[key]; !taken || current != value {
		return false, nil
	}
	delete(s.held, key)
	return true, nil
}

func (s *lockState) expire(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.held, key)
}

type delOnlyState struct {
	cache.SharedState
}

func (delOnlyState) SetNX(string, string, time.Duration) (bool, error) { return true, nil }

func TestAnOverrunningTurnCannotReleaseTheNextHoldersLock(t *testing.T) {
	state := newLockState()
	lock := NewAgentTurnLock(state)
	lock.poll = time.Millisecond

	overrun, err := lock.Acquire(context.Background(), "e-1", shared.EntryTypeTelegram)
	if err != nil {
		t.Fatal(err)
	}
	state.expire(agentTurnKey("e-1", shared.EntryTypeTelegram))
	next, err := lock.Acquire(context.Background(), "e-1", shared.EntryTypeTelegram)
	if err != nil {
		t.Fatal(err)
	}
	defer next()

	overrun()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := lock.Acquire(ctx, "e-1", shared.EntryTypeTelegram); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; a third turn got in because the overrunning turn freed the newer holder's lock", err)
	}
}

func TestALockStoreWithoutAGuardedDeleteIsRefused(t *testing.T) {
	if _, err := NewAgentTurnLock(delOnlyState{}).Acquire(context.Background(), "e-1", shared.EntryTypeWhatsApp); err == nil {
		t.Fatal("a store that can only delete blindly must not hand out a turn lock")
	}
}

func TestTwoAgentTurnsOnTheSameConversationNeverOverlap(t *testing.T) {
	lock := NewAgentTurnLock(newLockState())
	lock.poll = time.Millisecond

	release, err := lock.Acquire(context.Background(), "e-1", shared.EntryTypeTelegram)
	if err != nil {
		t.Fatal(err)
	}
	second := make(chan struct{})
	go func() {
		next, err := lock.Acquire(context.Background(), "e-1", shared.EntryTypeTelegram)
		if err == nil {
			next()
		}
		close(second)
	}()

	select {
	case <-second:
		t.Fatal("a second turn started while the first held the conversation")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("the second turn never ran after the first finished")
	}
}

func TestOtherConversationsAreNotHeldBack(t *testing.T) {
	lock := NewAgentTurnLock(newLockState())
	release, _ := lock.Acquire(context.Background(), "e-1", shared.EntryTypeTelegram)
	defer release()

	other, err := lock.Acquire(context.Background(), "e-2", shared.EntryTypeTelegram)
	if err != nil {
		t.Fatalf("another conversation waited: %v", err)
	}
	other()
}

func TestAWaitThatOutlivesTheCallerGivesUp(t *testing.T) {
	lock := NewAgentTurnLock(newLockState())
	lock.poll = time.Millisecond
	release, _ := lock.Acquire(context.Background(), "e-1", shared.EntryTypeWhatsApp)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := lock.Acquire(ctx, "e-1", shared.EntryTypeWhatsApp); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
}

func TestAnUnreachableLockStoreIsReported(t *testing.T) {
	state := newLockState()
	state.err = errors.New("redis down")
	if _, err := NewAgentTurnLock(state).Acquire(context.Background(), "e-1", shared.EntryTypeWhatsApp); err == nil {
		t.Fatal("the caller must learn the lock could not be taken")
	}
}

func TestAnInboundReplyGoesAheadWhenTheLockStoreIsDownAsBefore(t *testing.T) {
	state := newLockState()
	state.err = errors.New("redis down")
	release, ok := NewAgentTurnLock(state).HoldForInbound(context.Background(), "e-1", shared.EntryTypeWhatsApp)
	defer release()
	if !ok {
		t.Fatal("a lock store outage must not silence every agent")
	}
}

func TestAnInboundReplyThatWaitedPastItsDeadlineDoesNotReply(t *testing.T) {
	lock := NewAgentTurnLock(newLockState())
	lock.poll = time.Millisecond
	held, _ := lock.Acquire(context.Background(), "e-1", shared.EntryTypeWhatsApp)
	defer held()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	release, ok := lock.HoldForInbound(ctx, "e-1", shared.EntryTypeWhatsApp)
	release()
	if ok {
		t.Fatal("a reply that never got the conversation must not be sent alongside the other turn")
	}
}
