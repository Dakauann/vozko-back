package copilot_usecase

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

const msgTurnCrashed = "Erro interno ao gerar a resposta."

type TurnRegistry struct {
	limits copilot.TurnLimits

	mu    sync.Mutex
	turns map[string]*Turn
}

func NewTurnRegistry(limits copilot.TurnLimits) *TurnRegistry {
	return &TurnRegistry{limits: limits, turns: map[string]*Turn{}}
}

type pendingCommand struct {
	id    string
	event copilot.TurnEvent
}

type Turn struct {
	registry *TurnRegistry
	threadID string
	ownerID  string
	ctx      context.Context
	cancel   context.CancelFunc

	mu          sync.Mutex
	log         *copilot.TurnLog
	pending     []pendingCommand
	subscribers map[int]chan copilot.TurnEvent
	nextID      int
	finished    bool
	orphan      *time.Timer
}

func (r *TurnRegistry) Begin(parent context.Context, threadID, ownerID string) (*Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.turns[threadID]; ok && current.running() {
		return nil, copilot.ErrTurnRunning
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	turn := &Turn{
		registry:    r,
		threadID:    threadID,
		ownerID:     ownerID,
		ctx:         ctx,
		cancel:      cancel,
		log:         copilot.NewTurnLog(r.limits),
		subscribers: map[int]chan copilot.TurnEvent{},
	}
	turn.mu.Lock()
	turn.armOrphanLocked()
	turn.mu.Unlock()
	r.turns[threadID] = turn
	return turn, nil
}

func (r *TurnRegistry) lookup(threadID, userID string) (*Turn, error) {
	r.mu.Lock()
	turn, ok := r.turns[threadID]
	r.mu.Unlock()
	if !ok {
		return nil, copilot.ErrTurnNotRunning
	}
	if turn.ownerID != userID {
		return nil, copilot.ErrTurnForbidden
	}
	return turn, nil
}

func (r *TurnRegistry) Observe(threadID, userID string) (*Turn, error) {
	return r.lookup(threadID, userID)
}

func (r *TurnRegistry) Stop(threadID, userID string) error {
	turn, err := r.lookup(threadID, userID)
	if err != nil {
		return err
	}
	if !turn.running() {
		return copilot.ErrTurnNotRunning
	}
	turn.cancel()
	return nil
}

func (r *TurnRegistry) Running(threadID string) bool {
	r.mu.Lock()
	turn, ok := r.turns[threadID]
	r.mu.Unlock()
	return ok && turn.running()
}

func (r *TurnRegistry) Settle(threadID, commandID string) {
	r.mu.Lock()
	turn, ok := r.turns[threadID]
	r.mu.Unlock()
	if ok {
		turn.settle(commandID)
	}
}

func (r *TurnRegistry) forget(turn *Turn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.turns[turn.threadID] == turn {
		delete(r.turns, turn.threadID)
	}
}

func (t *Turn) Context() context.Context {
	return t.ctx
}

func (t *Turn) running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.finished
}

func (t *Turn) Emit(eventType string, payload interface{}) {
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[copilot] dropped unencodable %s event: %v", eventType, err)
		return
	}
	event := copilot.TurnEvent{Type: eventType, Payload: raw}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return
	}
	if cmd, ok := payload.(copilot.ScreenCommand); ok && eventType == EventScreenCommand {
		t.pending = append(t.pending, pendingCommand{id: cmd.ID, event: event})
	} else {
		t.log.Append(eventType, raw)
	}
	for id, ch := range t.subscribers {
		select {
		case ch <- event:
		default:
			log.Printf("[copilot] thread %s: a watcher fell %d events behind and was detached; it must reattach to replay", t.threadID, cap(ch))
			t.dropLocked(id)
		}
	}
}

func (t *Turn) Subscribe() ([]copilot.TurnEvent, <-chan copilot.TurnEvent, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	replay := t.log.Events()
	for _, cmd := range t.pending {
		replay = append(replay, cmd.event)
	}
	ch := make(chan copilot.TurnEvent, t.registry.limits.SubscriberBuffer)
	if t.finished {
		close(ch)
		return replay, ch, func() {}
	}
	id := t.nextID
	t.nextID++
	t.subscribers[id] = ch
	t.disarmOrphanLocked()
	var once sync.Once
	leave := func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.dropLocked(id)
		})
	}
	return replay, ch, leave
}

func (t *Turn) dropLocked(id int) {
	ch, ok := t.subscribers[id]
	if !ok {
		return
	}
	delete(t.subscribers, id)
	close(ch)
	if len(t.subscribers) == 0 && !t.finished {
		t.armOrphanLocked()
	}
}

func (t *Turn) armOrphanLocked() {
	t.disarmOrphanLocked()
	t.orphan = time.AfterFunc(t.registry.limits.DetachedGrace, t.cancel)
}

func (t *Turn) disarmOrphanLocked() {
	if t.orphan != nil {
		t.orphan.Stop()
		t.orphan = nil
	}
}

func (t *Turn) settle(commandID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept := t.pending[:0]
	for _, cmd := range t.pending {
		if cmd.id != commandID {
			kept = append(kept, cmd)
		}
	}
	t.pending = kept
}

func (t *Turn) Run(run func(ctx context.Context, emit agentloop.Emit) error) {
	go func() {
		defer t.finish()
		defer func() {
			if p := recover(); p != nil {
				log.Printf("[copilot] answer on thread %s panicked: %v", t.threadID, p)
				t.Emit("error", map[string]interface{}{"error": msgTurnCrashed})
			}
		}()
		if err := run(t.ctx, t.Emit); err != nil {
			t.Emit("error", map[string]interface{}{"error": err.Error()})
		}
	}()
}

func (t *Turn) finish() {
	t.mu.Lock()
	t.finished = true
	t.pending = nil
	t.disarmOrphanLocked()
	for id, ch := range t.subscribers {
		delete(t.subscribers, id)
		close(ch)
	}
	t.mu.Unlock()
	t.cancel()
	time.AfterFunc(t.registry.limits.Retention, func() { t.registry.forget(t) })
}
