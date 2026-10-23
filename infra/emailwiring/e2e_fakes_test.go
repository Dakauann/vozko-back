package emailwiring_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"vozko/domain/customfield"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	es "vozko/domain/emailsending"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	"vozko/domain/selection"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/infra/route53dns"
	"vozko/infra/route53dns/route53fake"
	"vozko/infra/ses"
	"vozko/infra/ses/sesfake"
	conversation_usecase "vozko/usecases/conversation"
	emuc "vozko/usecases/emailinbox"
)

type queued struct {
	body  []byte
	count int
	delay time.Duration
}

type steppedQueue struct {
	mu       sync.Mutex
	handlers map[string]func([]byte, messaging.MessageAck)
	ready    map[string][]queued
	delayed  map[string][]queued
	dead     map[string]int
	unacked  map[string]int
}

func newSteppedQueue() *steppedQueue {
	return &steppedQueue{
		handlers: map[string]func([]byte, messaging.MessageAck){},
		ready:    map[string][]queued{},
		delayed:  map[string][]queued{},
		dead:     map[string]int{},
		unacked:  map[string]int{},
	}
}

type steppedAck struct {
	q     *steppedQueue
	topic string
	item  queued
	once  sync.Once
}

func (a *steppedAck) Ack() error {
	a.once.Do(func() { a.q.settle(a.topic) })
	return nil
}

func (a *steppedAck) Nack(requeue bool) error {
	a.once.Do(func() {
		if requeue {
			a.q.mu.Lock()
			a.q.ready[a.topic] = append(a.q.ready[a.topic], queued{body: a.item.body, count: a.item.count + 1})
			a.q.mu.Unlock()
		}
		a.q.settle(a.topic)
	})
	return nil
}

func (a *steppedAck) DeliveryCount() int { return a.item.count }

func (q *steppedQueue) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[topic] = handler
	return nil
}

func (q *steppedQueue) Publish(topic string, body []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if base, ok := strings.CutSuffix(topic, messaging.DLQSuffix); ok {
		q.dead[base]++
		return nil
	}
	q.ready[topic] = append(q.ready[topic], queued{body: append([]byte(nil), body...), count: 1})
	return nil
}

func (q *steppedQueue) PublishWithDelay(topic string, body []byte, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.delayed[topic] = append(q.delayed[topic], queued{body: append([]byte(nil), body...), count: 1, delay: delay})
	return nil
}

func (q *steppedQueue) DeleteQueue(topic string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.ready, topic)
	delete(q.delayed, topic)
	return nil
}

func (q *steppedQueue) GetQueueLength(topic string) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if base, ok := strings.CutSuffix(topic, messaging.DelayQueueSuffix); ok {
		return len(q.delayed[base]), nil
	}
	return len(q.ready[topic]), nil
}

func (q *steppedQueue) ValidateConnection() error { return nil }

func (q *steppedQueue) pop(topic string) (queued, func([]byte, messaging.MessageAck), bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := q.ready[topic]
	handler := q.handlers[topic]
	if len(items) == 0 || handler == nil {
		return queued{}, nil, false
	}
	q.ready[topic] = items[1:]
	q.unacked[topic]++
	return items[0], handler, true
}

func (q *steppedQueue) settle(topic string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.unacked[topic]--
}

func (q *steppedQueue) inFlight(topic string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.unacked[topic]
}

func (q *steppedQueue) readyCount(topic string) int {
	n, _ := q.GetQueueLength(topic)
	return n
}

func (q *steppedQueue) delayedCount(topic string) int {
	n, _ := q.GetQueueLength(topic + messaging.DelayQueueSuffix)
	return n
}

func (q *steppedQueue) delays(topic string) []time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]time.Duration, 0, len(q.delayed[topic]))
	for _, item := range q.delayed[topic] {
		out = append(out, item.delay)
	}
	return out
}

func (q *steppedQueue) deadLetters(topic string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dead[topic]
}

func (q *steppedQueue) release(topic string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	moved := q.delayed[topic]
	delete(q.delayed, topic)
	q.ready[topic] = append(q.ready[topic], moved...)
	return len(moved)
}

var errUnacknowledged = errors.New("e2e: deliveries were never acknowledged")

func (q *steppedQueue) waitAcked(topic string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for q.inFlight(topic) > 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %d on %s", errUnacknowledged, q.inFlight(topic), topic)
		}
		time.Sleep(2 * time.Millisecond)
	}
	return nil
}

func (q *steppedQueue) drain(t testing.TB, topic string, workers, limit int) int {
	t.Helper()
	n, err := q.tryDrain(topic, workers, limit)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (q *steppedQueue) tryDrain(topic string, workers, limit int) (int, error) {
	var processed atomic.Int64
	reserve := func() bool {
		n := processed.Add(1)
		if limit > 0 && n > int64(limit) {
			processed.Add(-1)
			return false
		}
		return true
	}
	for {
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for reserve() {
					item, handler, ok := q.pop(topic)
					if !ok {
						processed.Add(-1)
						return
					}
					handler(item.body, &steppedAck{q: q, topic: topic, item: item})
				}
			}()
		}
		wg.Wait()
		if err := q.waitAcked(topic); err != nil {
			return int(processed.Load()), err
		}
		if q.readyCount(topic) == 0 || (limit > 0 && processed.Load() >= int64(limit)) {
			return int(processed.Load()), nil
		}
	}
}

type episodes struct {
	mu        sync.Mutex
	published []notification.Draft
	resolved  []string
}

func (e *episodes) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	if err := d.Normalize().Validate(); err != nil {
		return notification.Record{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.published = append(e.published, d)
	return notification.Record{}, nil
}

func (e *episodes) Resolve(_ context.Context, _ string, keys ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resolved = append(e.resolved, keys...)
	return nil
}

func (e *episodes) publishedTimes(key string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, d := range e.published {
		if d.EpisodeKey == key {
			n++
		}
	}
	return n
}

func (e *episodes) resolvedTimes(key string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, k := range e.resolved {
		if k == key {
			n++
		}
	}
	return n
}

type opsAlerts struct {
	mu      sync.Mutex
	subject []string
}

func (o *opsAlerts) Alert(_ context.Context, subject, _ string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.subject = append(o.subject, subject)
	return nil
}

func (o *opsAlerts) raised(subject string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, s := range o.subject {
		if s == subject {
			return true
		}
	}
	return false
}

const (
	probationZoneID = "ZE2EPROBATION"
	trustedZoneID   = "ZE2ETRUSTED"
)

type route53Zones struct {
	*route53dns.DNSRecords
	fake  *route53fake.Server
	zones map[string]string
}

type unpacedCalls struct{}

func (unpacedCalls) Await(context.Context, es.ProviderLane) error { return nil }

func newRoute53Zones(t *testing.T) *route53Zones {
	t.Helper()
	zones := map[string]string{probationDomain: probationZoneID, trustedDomain: trustedZoneID}
	fake := route53fake.New()
	for domain, id := range zones {
		fake.AddZone(id, domain)
	}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client, err := route53dns.NewClient(aws.Config{
		Region:      sesfake.Region,
		Credentials: credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
	}, srv.URL, ses.PaceCalls(unpacedCalls{}, route53dns.EveryCall))
	if err != nil {
		t.Fatal(err)
	}
	dns, err := route53dns.New(client, route53dns.Config{Zones: zones})
	if err != nil {
		t.Fatal(err)
	}
	return &route53Zones{DNSRecords: dns, fake: fake, zones: zones}
}

func (z *route53Zones) refuseNextChange(code, message string) {
	z.fake.RefuseNextChange(http.StatusForbidden, code, message)
}

func (z *route53Zones) published(sharedDomain string) []es.DNSRecord {
	var out []es.DNSRecord
	for _, set := range z.fake.RecordSets(z.zones[sharedDomain]) {
		for _, value := range set.Values {
			out = append(out, es.DNSRecord{Type: set.Type, Name: strings.TrimSuffix(set.Name, "."), Value: value, TTL: int(set.TTL)})
		}
	}
	return out
}

type quietDNS struct{}

func (quietDNS) LookupDMARC(context.Context, string) (string, error) { return "", nil }
func (quietDNS) LookupMX(context.Context, string) ([]string, error)  { return nil, nil }
func (quietDNS) LookupTXT(context.Context, string) ([]string, error) { return nil, nil }
func (quietDNS) Name() string                                        { return "postmaster-e2e" }
func (quietDNS) SpamRate(_ context.Context, domain string, day time.Time) (es.DomainStats, error) {
	return es.DomainStats{Domain: domain, Day: day.Format("2006-01-02")}, nil
}

type paidPlans struct {
	mu     sync.Mutex
	unpaid map[string]bool
}

func (p *paidPlans) lapse(workspaceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unpaid == nil {
		p.unpaid = map[string]bool{}
	}
	p.unpaid[workspaceID] = true
}

func (p *paidPlans) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unpaid[workspaceID] {
		return nil, workspace_plan.ErrSubscriptionNotCurrent
	}
	return &workspace_plan.WorkspaceSubscription{WorkspaceID: workspaceID, Status: workspace_plan.SubscriptionStatusActive}, nil
}

type activeSubscription struct{}

func (activeSubscription) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, nil
}

type everyLead struct{ db *gorm.DB }

func (e everyLead) Count(ctx context.Context, scope selection.Scope, _ selection.Selection) (int, error) {
	var n int64
	err := e.db.WithContext(ctx).Raw("SELECT count(*) FROM leads WHERE workspace_id = ? AND deleted_at IS NULL", scope.WorkspaceID).Scan(&n).Error
	return int(n), err
}

func (e everyLead) Resolve(ctx context.Context, scope selection.Scope, _ selection.Selection, after string, limit int) ([]selection.Ref, error) {
	var ids []string
	err := e.db.WithContext(ctx).Raw("SELECT id::text FROM leads WHERE workspace_id = ? AND deleted_at IS NULL AND id::text > ? ORDER BY id::text LIMIT ?",
		scope.WorkspaceID, after, limit).Scan(&ids).Error
	refs := make([]selection.Ref, len(ids))
	for i, id := range ids {
		refs[i] = selection.Ref{ID: id, Type: "lead"}
	}
	return refs, err
}

type assignments struct {
	mu      sync.Mutex
	ensured map[string]int
	claimed map[string]string
}

func (a *assignments) EnsureAssignment(entryID, _, _ string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ensured == nil {
		a.ensured = map[string]int{}
	}
	a.ensured[entryID]++
	return ""
}

func (a *assignments) ClaimIfUnassigned(entryID, _, _, _, userID, _ string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claimed == nil {
		a.claimed = map[string]string{}
	}
	if _, taken := a.claimed[entryID]; taken {
		return false, nil
	}
	a.claimed[entryID] = userID
	return true, nil
}

func (a *assignments) claimedBy(entryID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.claimed[entryID]
}

type automation struct {
	mu         sync.Mutex
	dispatched []conversation_usecase.InboundAutomationInput
	next       emuc.InboundDispatcher
}

func (a *automation) Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error {
	a.mu.Lock()
	a.dispatched = append(a.dispatched, in)
	next := a.next
	a.mu.Unlock()
	if next == nil {
		return nil
	}
	return next.Dispatch(ctx, in)
}

func (a *automation) forwardTo(next emuc.InboundDispatcher) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.next = next
}

func (a *automation) last() conversation_usecase.InboundAutomationInput {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.dispatched) == 0 {
		return conversation_usecase.InboundAutomationInput{}
	}
	return a.dispatched[len(a.dispatched)-1]
}

func (a *automation) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.dispatched)
}

type conversationEvents struct {
	mu     sync.Mutex
	events []*conv_event.ConversationEvent
}

func (c *conversationEvents) Log(ev *conv_event.ConversationEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

type announcer struct {
	mu      sync.Mutex
	updated []string
}

func (a *announcer) BroadcastMessageUpdated(entryID, _ string, m *conversation.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.updated = append(a.updated, entryID+"|"+string(m.DeliveryStatus))
}

func (a *announcer) BroadcastEntryUpdate(string, string, *conversation.Message) {}

func (a *announcer) count(entryID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, u := range a.updated {
		if strings.HasPrefix(u, entryID+"|") {
			n++
		}
	}
	return n
}

var errNoAttachmentStore = errors.New("e2e: attachments are not stored in this suite")

type refusingMedia struct{}

func (refusingMedia) Store(conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	return nil, errNoAttachmentStore
}

var errTranscriptDown = errors.New("e2e: the conversation store refused the write")

type flakyHistory struct {
	conversation.MessageHistoryManager
	failures atomic.Int64
}

func (h *flakyHistory) failNext(n int64) { h.failures.Store(n) }

func (h *flakyHistory) Record(ctx context.Context, rec conversation.MessageHistoryRecord) error {
	for {
		left := h.failures.Load()
		if left <= 0 {
			return h.MessageHistoryManager.Record(ctx, rec)
		}
		if h.failures.CompareAndSwap(left, left-1) {
			return errTranscriptDown
		}
	}
}

func (e everyLead) Selected(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error) {
	return e.Count(ctx, scope, s)
}

type noLeadFields struct{}

func (noLeadFields) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return nil, nil
}

type openAudienceGate struct{}

func (openAudienceGate) Acquire(context.Context) (func(), error) { return func() {}, nil }
