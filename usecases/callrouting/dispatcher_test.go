package callrouting_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
	dept_domain "vozko/domain/workspace/workspace_department"
	callrouting_infra "vozko/infra/callrouting"
	callsession_usecase "vozko/usecases/callsession"
)

type operatorBehaviour int

const (
	accepts operatorBehaviour = iota
	declines
	ignores
)

type operator struct {
	id, userID string
	behaviour  operatorBehaviour
	broker     *callsession_usecase.InboundOfferBroker

	mu       sync.Mutex
	reserved string
	onCall   bool
	closed   bool
	offers   []callsession.InboundCallOffer
	statuses []callsession.TransferStatus
}

func (o *operator) ID() string           { return o.id }
func (o *operator) UserID() string       { return o.userID }
func (o *operator) WorkspaceID() string  { return "ws1" }
func (o *operator) ActiveCallID() string { return "" }
func (o *operator) HasActiveCall() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.onCall || o.reserved != ""
}
func (o *operator) Reserve(token string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.onCall || o.reserved != "" {
		return false
	}
	o.reserved = token
	return true
}
func (o *operator) Release(token string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.reserved == token {
		o.reserved = ""
	}
}
func (o *operator) Notify(msg callsession.CallSessionControlMessage) error {
	if status, ok := msg.Payload.(callsession.TransferStatus); ok {
		o.mu.Lock()
		o.statuses = append(o.statuses, status)
		o.mu.Unlock()
		return nil
	}
	offer, ok := msg.Payload.(callsession.InboundCallOffer)
	if !ok {
		return nil
	}
	o.mu.Lock()
	o.offers = append(o.offers, offer)
	o.mu.Unlock()
	go func() {
		switch o.behaviour {
		case accepts:
			_ = o.broker.Accept(context.Background(), callsession.AcceptInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws1", UserID: o.userID, SessionID: o.id})
		case declines:
			_ = o.broker.Decline(context.Background(), callsession.DeclineInboundCallInput{OfferID: offer.OfferID, WorkspaceID: "ws1", UserID: o.userID, SessionID: o.id})
		}
	}()
	return nil
}
func (o *operator) transferStatuses() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, 0, len(o.statuses))
	for _, s := range o.statuses {
		out = append(out, s.Status)
	}
	return out
}

func (o *operator) isOnCall() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.onCall
}

func (o *operator) receivedOffers() []callsession.InboundCallOffer {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]callsession.InboundCallOffer(nil), o.offers...)
}

type presence struct {
	callsession.CallSessionRegistry
	operators []*operator
}

func (p presence) ListAvailable(string) []callsession.CallSession {
	var out []callsession.CallSession
	for _, o := range p.operators {
		if !o.HasActiveCall() {
			out = append(out, o)
		}
	}
	return out
}

type heldCall struct {
	id        string
	mu        sync.Mutex
	channel   string
	contact   conversation.CallContact
	held      int
	unheld    int
	owner     callsession.CallSession
	done      chan struct{}
	endedOnce sync.Once
}

func newHeldCall(id string) *heldCall {
	return &heldCall{id: id, channel: callsession.OfferChannelSIP, done: make(chan struct{})}
}

func (c *heldCall) ID() string           { return c.id }
func (c *heldCall) WorkspaceID() string  { return "ws1" }
func (c *heldCall) RemoteNumber() string { return "5584994409684" }
func (c *heldCall) Channel() string      { return c.channel }
func (c *heldCall) Contact() (conversation.CallContact, bool) {
	return c.contact, c.contact.Known()
}
func (c *heldCall) OwnerSession() (callsession.CallSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.owner, c.owner != nil
}
func (c *heldCall) Hold([]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held++
	if previous, ok := c.owner.(*operator); ok {
		previous.mu.Lock()
		previous.onCall = false
		previous.mu.Unlock()
	}
	c.owner = nil
	return nil
}
func (c *heldCall) Connect(s callsession.CallSession) error {
	o := s.(*operator)
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return errors.New("session closed")
	}
	if o.onCall {
		o.mu.Unlock()
		return errors.New("busy")
	}
	o.onCall = true
	o.reserved = ""
	o.mu.Unlock()
	c.mu.Lock()
	c.owner = s
	c.mu.Unlock()
	return nil
}
func (c *heldCall) StopHold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unheld++
}
func (c *heldCall) SendAudio([]byte) error { return nil }
func (c *heldCall) Done() <-chan struct{}  { return c.done }
func (c *heldCall) Hangup() error {
	c.endedOnce.Do(func() { close(c.done) })
	return nil
}

type answerPermission map[string]bool

func (p answerPermission) MayAnswerCalls(userID, workspaceID, channel string) bool {
	if workspaceID != "ws1" {
		return false
	}
	if allowed, listed := p[userID+"|"+channel]; listed {
		return allowed
	}
	allowed, listed := p[userID]
	return !listed || allowed
}

func (p answerPermission) MayTransferCalls(userID, workspaceID string) bool {
	allowed, listed := p[userID+"|transfer"]
	return workspaceID == "ws1" && (!listed || allowed)
}

type music struct{ callrouting.HoldMusicLibrary }

func (music) PCM(context.Context, string, callrouting.HoldMusicRef) ([]byte, error) {
	return []byte{1, 2, 3, 4}, nil
}

type fixture struct {
	dispatcher *Dispatcher
	activity   *callrouting_infra.AgentActivity
	broker     *callsession_usecase.InboundOfferBroker
}

func newFixture(operators ...*operator) fixture {
	return newFixtureWith(answerPermission{}, operators...)
}

func newFixtureWith(permission callrouting.AnswerPermission, operators ...*operator) fixture {
	broker := callsession_usecase.NewInboundOfferBroker()
	for _, o := range operators {
		o.broker = broker
		o.id = "session-" + o.userID
	}
	activity := callrouting_infra.NewAgentActivity()
	d := NewDispatcher(DispatcherDeps{
		Sessions:   presence{operators: operators},
		Permission: permission,
		Ringer:     callsession_usecase.NewInboundRinger(broker),
		Activity:   activity,
		Members:    StaticMembers{},
		Music:      music{},
	})
	d.retry = 20 * time.Millisecond
	return fixture{dispatcher: d, activity: activity, broker: broker}
}

func testQueue(members ...string) *callrouting.Queue {
	return &callrouting.Queue{
		ID: "q1", WorkspaceID: "ws1", Name: "Vendas", Strategy: callrouting.StrategyLongestIdle,
		MemberUserIDs: members, RingSeconds: 1, MaxWaitSeconds: 1, WrapUpSeconds: 10,
	}
}

func TestTheQueueConnectsTheCallerToTheLongestIdleMember(t *testing.T) {
	ana := &operator{userID: "ana"}
	bia := &operator{userID: "bia"}
	outsider := &operator{userID: "zé"}
	f := newFixture(ana, bia, outsider)
	f.activity.CallEnded("ws1", "ana", time.Now().Add(-time.Minute))
	f.activity.CallEnded("ws1", "bia", time.Now().Add(-time.Hour))
	call := newHeldCall("c1")

	result, err := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: testQueue("ana", "bia"), Call: call, Notes: "quer segunda via"})
	if err != nil || result.Outcome != callrouting.OutcomeConnected || result.AgentUserID != "bia" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if call.held != 1 {
		t.Fatal("the caller never heard hold music")
	}
	offers := bia.receivedOffers()
	if len(offers) != 1 || offers[0].Transfer == nil || offers[0].Transfer.QueueName != "Vendas" || offers[0].Transfer.Notes != "quer segunda via" {
		t.Fatalf("bia's offer = %+v", offers)
	}
	if len(outsider.receivedOffers()) != 0 {
		t.Fatal("someone outside the queue was rung")
	}
}

func TestAMemberInWrapUpIsSkippedAndADeclineMovesOn(t *testing.T) {
	ana := &operator{userID: "ana", behaviour: declines}
	bia := &operator{userID: "bia"}
	caio := &operator{userID: "caio"}
	f := newFixture(ana, bia, caio)
	f.activity.CallEnded("ws1", "caio", time.Now())

	result, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: testQueue("ana", "bia", "caio"), Call: newHeldCall("c1")})
	if result.Outcome != callrouting.OutcomeConnected || result.AgentUserID != "bia" {
		t.Fatalf("result = %+v", result)
	}
	if len(caio.receivedOffers()) != 0 {
		t.Fatal("a member still in wrap-up was rung")
	}
}

func TestNobodyAnsweringEndsInATimeout(t *testing.T) {
	f := newFixture(&operator{userID: "ana", behaviour: ignores})
	start := time.Now()
	result, err := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: testQueue("ana"), Call: newHeldCall("c1")})
	if err != nil || result.Outcome != callrouting.OutcomeTimedOut {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("waited %v past the queue limit", elapsed)
	}
}

func TestACallerWhoHangsUpLeavesTheQueue(t *testing.T) {
	f := newFixture()
	call := newHeldCall("c1")
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = call.Hangup()
	}()
	q := testQueue("ana")
	q.MaxWaitSeconds = 30
	result, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: q, Call: call})
	if result.Outcome != callrouting.OutcomeAbandoned {
		t.Fatalf("result = %+v", result)
	}
}

func TestCallersAreServedInTheOrderTheyArrived(t *testing.T) {
	ana := &operator{userID: "ana"}
	f := newFixture(ana)
	q := testQueue("ana")
	q.MaxWaitSeconds = 5
	first := newHeldCall("first")
	second := newHeldCall("second")

	ana.mu.Lock()
	ana.onCall = true
	ana.mu.Unlock()

	results := make(chan string, 2)
	for _, call := range []*heldCall{first, second} {
		go func(c *heldCall) {
			res, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: q, Call: c})
			if res.Outcome == callrouting.OutcomeConnected {
				results <- c.id
			}
		}(call)
		time.Sleep(30 * time.Millisecond)
	}

	ana.mu.Lock()
	ana.onCall = false
	ana.mu.Unlock()

	select {
	case got := <-results:
		if got != "first" {
			t.Fatalf("%s was served before the caller who arrived first", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nobody was served")
	}
	_ = second.Hangup()
}

type departmentStub map[string][]dept_domain.DepartmentMember

func (d departmentStub) ListMembers(departmentID string) ([]dept_domain.DepartmentMember, error) {
	return d[departmentID], nil
}

func TestADepartmentQueueRingsItsMembers(t *testing.T) {
	members := NewQueueMembers(departmentStub{"vendas": {{UserID: "ana"}, {UserID: "bia"}}})
	got, err := members.UserIDs(context.Background(), callrouting.Queue{DepartmentID: "vendas"})
	if err != nil || len(got) != 2 || got[1] != "bia" {
		t.Fatalf("UserIDs = %v, %v", got, err)
	}
	people, _ := members.UserIDs(context.Background(), callrouting.Queue{MemberUserIDs: []string{"caio"}})
	if len(people) != 1 || people[0] != "caio" {
		t.Fatalf("explicit members = %v", people)
	}
}

func TestOnlyMembersAllowedToAnswerCallsAreRung(t *testing.T) {
	ana := &operator{userID: "ana"}
	bia := &operator{userID: "bia"}
	f := newFixtureWith(answerPermission{"ana": false}, ana, bia)
	f.activity.CallEnded("ws1", "bia", time.Now().Add(-time.Minute))

	result, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: testQueue("ana", "bia"), Call: newHeldCall("c1")})
	if result.AgentUserID != "bia" || len(ana.receivedOffers()) != 0 {
		t.Fatalf("result = %+v, ana offers = %d", result, len(ana.receivedOffers()))
	}
}

func TestWithoutAPermissionCheckNobodyIsRung(t *testing.T) {
	ana := &operator{userID: "ana"}
	f := newFixtureWith(nil, ana)
	result, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: testQueue("ana"), Call: newHeldCall("c1")})
	if result.Outcome != callrouting.OutcomeTimedOut || len(ana.receivedOffers()) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
