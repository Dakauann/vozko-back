package emailwebhook_usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"vozko/domain/emailapi"
	wh "vozko/domain/emailwebhook"
	"vozko/domain/notification"
)

type memEndpoints struct {
	mu        sync.Mutex
	endpoints map[string]*wh.Endpoint
}

func (s *memEndpoints) Create(_ context.Context, e *wh.Endpoint, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, x := range s.endpoints {
		if x.WorkspaceID == e.WorkspaceID {
			n++
		}
	}
	if n >= limit {
		return wh.ErrLimitReached
	}
	c := *e
	s.endpoints[e.ID] = &c
	return nil
}

func (s *memEndpoints) List(_ context.Context, ws string) ([]wh.Endpoint, error) {
	var out []wh.Endpoint
	for _, e := range s.endpoints {
		if e.WorkspaceID == ws {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memEndpoints) Get(_ context.Context, ws, id string) (*wh.Endpoint, error) {
	e, ok := s.endpoints[id]
	if !ok || e.WorkspaceID != ws {
		return nil, wh.ErrEndpointNotFound
	}
	c := *e
	return &c, nil
}

func (s *memEndpoints) Update(_ context.Context, e *wh.Endpoint) error {
	c := *e
	s.endpoints[e.ID] = &c
	return nil
}

func (s *memEndpoints) SetSecret(_ context.Context, ws, id string, cipher []byte, hint string, _ time.Time) error {
	s.endpoints[id].SecretCipher, s.endpoints[id].SecretHint = cipher, hint
	return nil
}

func (s *memEndpoints) Delete(_ context.Context, ws, id string) error {
	delete(s.endpoints, id)
	return nil
}

func (s *memEndpoints) Subscribed(_ context.Context, ws string, t wh.EventType) ([]wh.Endpoint, error) {
	var out []wh.Endpoint
	for _, e := range s.endpoints {
		if e.WorkspaceID == ws && e.Wants(t) {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memEndpoints) MarkSuccess(_ context.Context, id string, at time.Time) error {
	e := s.endpoints[id]
	e.LastSuccessAt, e.FailingSince, e.AlertedAt = &at, nil, nil
	return nil
}

func (s *memEndpoints) MarkFailure(_ context.Context, id string, at time.Time) error {
	e := s.endpoints[id]
	e.LastFailureAt = &at
	if e.FailingSince == nil {
		e.FailingSince = &at
	}
	return nil
}

func (s *memEndpoints) MarkAlerted(_ context.Context, id string, at time.Time) error {
	s.endpoints[id].AlertedAt = &at
	return nil
}

type memDeliveries struct {
	mu        sync.Mutex
	endpoints *memEndpoints
	rows      map[string]*wh.Delivery
	attempts  map[string][]wh.Attempt
	events    map[string]bool
}

func (s *memDeliveries) InsertBatch(_ context.Context, ds []wh.Delivery) ([]wh.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []wh.Delivery
	for _, d := range ds {
		key := d.EndpointID + "|" + d.EventID
		if s.events[key] {
			continue
		}
		s.events[key] = true
		d.Status = wh.StatusQueued
		c := d
		s.rows[d.ID] = &c
		out = append(out, d)
	}
	return out, nil
}

func (s *memDeliveries) Target(_ context.Context, id string) (*wh.Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.rows[id]
	if !ok {
		return nil, wh.ErrDeliveryNotFound
	}
	e, ok := s.endpoints.endpoints[d.EndpointID]
	if !ok {
		return nil, wh.ErrDeliveryNotFound
	}
	return &wh.Target{Delivery: *d, Endpoint: *e}, nil
}

func (s *memDeliveries) Claim(_ context.Context, id string, attempt int, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.rows[id]
	if d == nil || d.Status != wh.StatusQueued || d.Attempts != attempt-1 {
		return false, nil
	}
	d.Status, d.Attempts, d.UpdatedAt = wh.StatusSending, attempt, at
	return true, nil
}

func (s *memDeliveries) Finish(_ context.Context, f wh.Finish) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.rows[f.ID]
	d.Status, d.NextAttemptAt, d.LastStatusCode, d.LastError, d.UpdatedAt = f.Outcome.Status, f.Outcome.NextAttemptAt, f.StatusCode, f.Error, f.At
	s.attempts[f.ID] = append(s.attempts[f.ID], f.Attempt)
	return nil
}

func (s *memDeliveries) Skip(_ context.Context, id, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[id].Status, s.rows[id].LastError = wh.StatusSkipped, reason
	return nil
}

func (s *memDeliveries) Requeue(_ context.Context, now time.Time, limit int) ([]wh.Requeued, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []wh.Requeued
	for _, d := range s.rows {
		if d.Status == wh.StatusRetrying && d.NextAttemptAt != nil && !d.NextAttemptAt.After(now) {
			d.Status = wh.StatusQueued
			out = append(out, wh.Requeued{ID: d.ID, EndpointID: d.EndpointID, Attempts: d.Attempts})
		}
	}
	return out, nil
}

func (s *memDeliveries) Retry(_ context.Context, ws, endpointID, id string, at time.Time) (*wh.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.rows[id]
	if !ok || d.WorkspaceID != ws || d.EndpointID != endpointID {
		return nil, wh.ErrDeliveryNotFound
	}
	if !d.Retryable() {
		return nil, wh.ErrDeliveryNotRetryable
	}
	d.Status = wh.StatusQueued
	c := *d
	return &c, nil
}

func (s *memDeliveries) ListByEndpoint(_ context.Context, ws, endpointID string, _ emailapi.Cursor, limit int) ([]wh.Delivery, error) {
	var out []wh.Delivery
	for _, d := range s.rows {
		if d.EndpointID == endpointID && d.WorkspaceID == ws {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit+1 {
		out = out[:limit+1]
	}
	return out, nil
}

func (s *memDeliveries) Attempts(_ context.Context, ids []string) (map[string][]wh.Attempt, error) {
	out := map[string][]wh.Attempt{}
	for _, id := range ids {
		out[id] = s.attempts[id]
	}
	return out, nil
}

func (s *memDeliveries) PurgeAttempts(context.Context, time.Time, int) (int64, error) { return 0, nil }
func (s *memDeliveries) PurgeDeliveries(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

type fakeStatuses struct {
	marked []string
	known  map[string]bool
}

func (f *fakeStatuses) MarkStatus(_ context.Context, messageID string, status emailapi.MessageStatus, event string, _ time.Time) (bool, error) {
	f.marked = append(f.marked, messageID+"|"+string(status)+"|"+event)
	return f.known[messageID], nil
}

type fakeEntries map[string]emailapi.LogEntry

func (f fakeEntries) GetByMessageID(_ context.Context, id string) (*emailapi.LogEntry, error) {
	e, ok := f[id]
	if !ok {
		return nil, emailapi.ErrMessageNotFound
	}
	return &e, nil
}

type prefixCipher struct{}

func (prefixCipher) Encrypt(p []byte) ([]byte, error) { return append([]byte("sealed:"), p...), nil }
func (prefixCipher) Decrypt(c []byte) ([]byte, error) {
	if !bytes.HasPrefix(c, []byte("sealed:")) {
		return nil, errors.New("not sealed")
	}
	return c[len("sealed:"):], nil
}

type fakeHosts struct{ blocked map[string]bool }

func (f fakeHosts) CheckHost(_ context.Context, host string) error {
	if f.blocked[host] {
		return errors.New("private address")
	}
	return nil
}

type posted struct {
	URL     string
	Headers map[string]string
	Body    []byte
}

type fakePoster struct {
	mu       sync.Mutex
	statuses []int
	err      error
	calls    []posted
}

func (p *fakePoster) Post(_ context.Context, url string, headers map[string]string, body []byte) (wh.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, posted{URL: url, Headers: headers, Body: body})
	if p.err != nil {
		return wh.Response{}, p.err
	}
	status := 200
	if len(p.statuses) > 0 {
		status, p.statuses = p.statuses[0], p.statuses[1:]
	}
	return wh.Response{StatusCode: status}, nil
}

type fakeSlots struct {
	mu    sync.Mutex
	inUse map[string]int
	max   int
	err   error
}

func (s *fakeSlots) Acquire(endpointID string) (func(), bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, false, s.err
	}
	if s.inUse[endpointID] >= s.max {
		return nil, false, nil
	}
	s.inUse[endpointID]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.inUse[endpointID]--
	}, true, nil
}

type queued struct {
	Topic   string
	Payload []byte
	Delay   time.Duration
}

type fakeQueue struct {
	mu   sync.Mutex
	msgs []queued
	err  error
}

func (q *fakeQueue) Publish(topic string, payload []byte) error {
	return q.PublishWithDelay(topic, payload, 0)
}

func (q *fakeQueue) PublishWithDelay(topic string, payload []byte, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.msgs = append(q.msgs, queued{Topic: topic, Payload: payload, Delay: delay})
	return nil
}

func (q *fakeQueue) ValidateConnection() error { return nil }

func (q *fakeQueue) take(topic string) []queued {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out, rest []queued
	for _, m := range q.msgs {
		if m.Topic == topic {
			out = append(out, m)
		} else {
			rest = append(rest, m)
		}
	}
	q.msgs = rest
	return out
}

type fakeAlerts struct {
	published []notification.Draft
	resolved  []string
}

func (a *fakeAlerts) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	a.published = append(a.published, d)
	return notification.Record{}, nil
}

func (a *fakeAlerts) Resolve(_ context.Context, _ string, keys ...string) error {
	a.resolved = append(a.resolved, keys...)
	return nil
}

type world struct {
	endpoints  *memEndpoints
	deliveries *memDeliveries
	statuses   *fakeStatuses
	entries    fakeEntries
	poster     *fakePoster
	slots      *fakeSlots
	queue      *fakeQueue
	alerts     *fakeAlerts
	hosts      fakeHosts
	now        time.Time
	ids        int
	module     *Module
}

func newWorld(t *testing.T) *world {
	t.Helper()
	endpoints := &memEndpoints{endpoints: map[string]*wh.Endpoint{}}
	w := &world{
		endpoints:  endpoints,
		deliveries: &memDeliveries{endpoints: endpoints, rows: map[string]*wh.Delivery{}, attempts: map[string][]wh.Attempt{}, events: map[string]bool{}},
		statuses:   &fakeStatuses{known: map[string]bool{}},
		entries:    fakeEntries{},
		poster:     &fakePoster{},
		slots:      &fakeSlots{inUse: map[string]int{}, max: wh.MaxInFlightPerEndpoint},
		queue:      &fakeQueue{},
		alerts:     &fakeAlerts{},
		hosts:      fakeHosts{blocked: map[string]bool{"internal.example.com": true}},
		now:        time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC),
	}
	m, err := New(Deps{
		Endpoints: w.endpoints, Deliveries: w.deliveries, Statuses: w.statuses, Entries: w.entries, Cipher: prefixCipher{},
		Hosts: w.hosts, Poster: w.poster, Slots: w.slots, Queue: w.queue, Alerts: w.alerts, UserAgent: "Acme Webhooks",
		Now:    func() time.Time { return w.now },
		NewID:  func() string { w.ids++; return fmt.Sprintf("id-%03d", w.ids) },
		Random: bytes.NewReader(counting(4096)),
		Logf:   func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.module = m
	return w
}

func (w *world) drain(t *testing.T) {
	t.Helper()
	for i := 0; i < 50; i++ {
		fan := w.queue.take(TopicFanout)
		del := w.queue.take(TopicDelivery)
		if len(fan) == 0 && len(del) == 0 {
			return
		}
		for _, m := range fan {
			var msg FanoutMessage
			_ = json.Unmarshal(m.Payload, &msg)
			if err := w.module.Fanout(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
		}
		for _, m := range del {
			if m.Delay > 0 {
				w.queue.mu.Lock()
				w.queue.msgs = append(w.queue.msgs, m)
				w.queue.mu.Unlock()
				return
			}
			var msg DeliveryMessage
			_ = json.Unmarshal(m.Payload, &msg)
			if err := w.module.Deliver(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func counting(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i % 251)
	}
	return out
}
