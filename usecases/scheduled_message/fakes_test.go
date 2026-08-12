package scheduled_message_usecase

import (
	"context"
	"sync"
	"time"

	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

// fakeRepo is an in-memory Repository that enforces the SAME conditional-write
// contract the SQL does.
//
// The mutex is not decoration: ClaimForDispatch must be atomic here for exactly
// the reason it must be one UPDATE in Postgres. A fake that read and then wrote
// would let the concurrency test pass against an implementation that cannot
// hold the guarantee in production.
type fakeRepo struct {
	mu       sync.Mutex
	messages map[string]*sm.ScheduledMessage
	byKey    map[string]string

	claimAttempts int
	createErr     error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		messages: map[string]*sm.ScheduledMessage{},
		byKey:    map[string]string{},
	}
}

func (r *fakeRepo) put(m *sm.ScheduledMessage) *sm.ScheduledMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages[m.ID] = m
	return m
}

func (r *fakeRepo) get(id string) *sm.ScheduledMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.messages[id]
}

func (r *fakeRepo) Create(m *sm.ScheduledMessage) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *m
	r.messages[m.ID] = &copied
	if m.IdempotencyKey != nil {
		r.byKey[m.WorkspaceID+"|"+*m.IdempotencyKey] = m.ID
	}
	return nil
}

func (r *fakeRepo) FindByID(id string) (*sm.ScheduledMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.messages[id]
	if !ok {
		return nil, sm.ErrNotFound
	}
	copied := *m
	return &copied, nil
}

func (r *fakeRepo) FindByIdempotencyKey(workspaceID, key string) (*sm.ScheduledMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byKey[workspaceID+"|"+key]
	if !ok {
		return nil, sm.ErrNotFound
	}
	copied := *r.messages[id]
	return &copied, nil
}

func (r *fakeRepo) ListByEntry(entryID, entryType string, statuses []sm.Status) ([]*sm.ScheduledMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	wanted := map[sm.Status]bool{}
	for _, s := range statuses {
		wanted[s] = true
	}

	out := []*sm.ScheduledMessage{}
	for _, m := range r.messages {
		if m.EntryID != entryID || string(m.EntryType) != entryType {
			continue
		}
		if len(wanted) > 0 && !wanted[m.Status] {
			continue
		}
		copied := *m
		out = append(out, &copied)
	}
	return out, nil
}

func (r *fakeRepo) ListByWorkspace(workspaceID string, _ sm.ListQuery) ([]*sm.ScheduledMessage, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*sm.ScheduledMessage{}
	for _, m := range r.messages {
		if m.WorkspaceID == workspaceID {
			copied := *m
			out = append(out, &copied)
		}
	}
	return out, int64(len(out)), nil
}

// ClaimForDispatch is the fake's most important method: one atomic
// check-and-set, exactly like the SQL it stands in for.
func (r *fakeRepo) ClaimForDispatch(id string, now time.Time) (*sm.ScheduledMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.claimAttempts++
	m, ok := r.messages[id]
	if !ok || m.Status != sm.StatusPending {
		return nil, nil
	}
	m.Status = sm.StatusSending
	m.ClaimedAt = &now

	copied := *m
	return &copied, nil
}

func (r *fakeRepo) ClaimDueBatch(now time.Time, limit int) ([]*sm.ScheduledMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := []*sm.ScheduledMessage{}
	for _, m := range r.messages {
		if len(out) >= limit {
			break
		}
		if m.Status != sm.StatusPending || m.ScheduledAt.After(now) {
			continue
		}
		m.Status = sm.StatusSending
		m.ClaimedAt = &now
		copied := *m
		out = append(out, &copied)
	}
	return out, nil
}

func (r *fakeRepo) MarkSent(id, messageID string, sentAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.messages[id]
	if !ok || m.Status != sm.StatusSending {
		return sm.ErrNotPending
	}
	m.Status = sm.StatusSent
	m.SentMessageID = &messageID
	m.SentAt = &sentAt
	return nil
}

func (r *fakeRepo) MarkFailed(id string, reason sm.FailureReason, detail string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.messages[id]
	if !ok || (m.Status != sm.StatusSending && m.Status != sm.StatusPending) {
		return sm.ErrNotPending
	}
	m.Status = sm.StatusFailed
	m.FailureReason = &reason
	m.FailureDetail = detail
	return nil
}

func (r *fakeRepo) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.messages[id]
	if !ok || m.Status != sm.StatusPending {
		return sm.ErrNotPending
	}
	m.Status = sm.StatusCanceled
	return nil
}

func (r *fakeRepo) Reschedule(id string, at time.Time, windowExpiresAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.messages[id]
	if !ok || m.Status != sm.StatusPending {
		return sm.ErrNotPending
	}
	m.ScheduledAt = at
	m.WindowExpiresAtAtCreation = windowExpiresAt
	return nil
}

func (r *fakeRepo) ListStuckClaims(claimedBefore time.Time, limit int) ([]*sm.ScheduledMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*sm.ScheduledMessage{}
	for _, m := range r.messages {
		if len(out) >= limit {
			break
		}
		if m.Status == sm.StatusSending && m.ClaimedAt != nil && m.ClaimedAt.Before(claimedBefore) {
			copied := *m
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeRepo) PurgeTerminalBefore(cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed int64
	for id, m := range r.messages {
		if m.Status.IsTerminal() && m.UpdatedAt.Before(cutoff) {
			delete(r.messages, id)
			removed++
		}
	}
	return removed, nil
}

// fakeWindows answers the one question every path asks.
type fakeWindows struct {
	mu        sync.Mutex
	open      bool
	expiresAt *time.Time
	reason    conversation.WindowClosedReason
	calls     int
}

func (w *fakeWindows) GetWindowStatusForEntry(string, string) conversation.WindowState {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.open {
		return conversation.OpenWindow(w.expiresAt)
	}
	reason := w.reason
	if reason == "" {
		reason = conversation.WindowReasonExpired
	}
	return conversation.ClosedWindowUntil(reason, w.expiresAt)
}

func (w *fakeWindows) set(open bool, expiresAt *time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.open, w.expiresAt = open, expiresAt
}

type scheduledFire struct {
	id     string
	fireAt time.Time
}

type fakeWake struct {
	mu    sync.Mutex
	fires []scheduledFire
	err   error
}

func (w *fakeWake) ScheduleFire(id string, fireAt time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fires = append(w.fires, scheduledFire{id, fireAt})
	return w.err
}

func (w *fakeWake) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.fires)
}

// fakeSend counts deliveries. The count is the whole point of the concurrency
// test, so it is mutex-guarded rather than a plain int.
type fakeSend struct {
	mu    sync.Mutex
	calls []conversation.OperatorSendInput
	err   error
}

func (s *fakeSend) Execute(_ context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, in)
	if s.err != nil {
		return nil, s.err
	}
	return &conversation.Message{ID: "msg-1", EntryID: in.EntryID, EntryType: shared.EntryType(in.EntryType)}, nil
}

func (s *fakeSend) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *fakeSend) first() conversation.OperatorSendInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[0]
}

type fakeBroadcaster struct {
	mu       sync.Mutex
	messages int
}

func (b *fakeBroadcaster) BroadcastNewMessage(string, string, *conversation.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages++
}
func (b *fakeBroadcaster) BroadcastMessageSent(string, string, string, string, *conversation.Message) {
}
func (b *fakeBroadcaster) BroadcastMessageError(string, string, string, string, string, string) {}
func (b *fakeBroadcaster) BroadcastRead(string, string, []string, string, time.Time)            {}
func (b *fakeBroadcaster) BroadcastUnreadCount(string, string, int64)                           {}
func (b *fakeBroadcaster) BroadcastTyping(string, string, string, bool)                         {}
func (b *fakeBroadcaster) BroadcastStageUpdate(string, string, string)                          {}
func (b *fakeBroadcaster) BroadcastLabelUpdate(string, string, string)                          {}
func (b *fakeBroadcaster) BroadcastEntryUpdate(string, string, *conversation.Message)           {}
func (b *fakeBroadcaster) BroadcastMessageStatus(string, string, string, conversation.DeliveryStatus) {
}
func (b *fakeBroadcaster) BroadcastAnalysisUpdate(string, string, interface{}) {}

// fixedClock keeps the window arithmetic deterministic; this feature is
// entirely about time, and a test that slept would be both slow and flaky.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
