package ws

import (
	"context"
	"encoding/json"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/workflow"
	callrouting_infra "vozko/infra/callrouting"
	"vozko/infra/holdmusic"
	"vozko/infra/voip/voiptest"
	callrouting_usecase "vozko/usecases/callrouting"
	callsession_usecase "vozko/usecases/callsession"
)

type routingParts struct {
	channels  *CallChannels
	transfers *callrouting_usecase.TransferCall
	flows     workflow.InboundVoiceFlows
	log       *memoryTransferLog
}

type memoryQueues map[string]*callrouting.Queue

func (q memoryQueues) Create(context.Context, *callrouting.Queue) error { return nil }
func (q memoryQueues) Update(context.Context, *callrouting.Queue) error { return nil }
func (q memoryQueues) Delete(context.Context, string, string) error     { return nil }
func (q memoryQueues) ListByWorkspace(context.Context, string) ([]*callrouting.Queue, error) {
	return nil, nil
}
func (q memoryQueues) FindInWorkspace(_ context.Context, workspaceID, id string) (*callrouting.Queue, error) {
	queue, ok := q[id]
	if !ok || queue.WorkspaceID != workspaceID {
		return nil, callrouting.ErrQueueNotFound
	}
	return queue, nil
}

type memoryTransferLog struct {
	mu       sync.Mutex
	outcomes map[string]callrouting.TransferOutcome
}

func (l *memoryTransferLog) Record(_ context.Context, record callrouting.TransferRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outcomes[record.ID] = record.Outcome
	return nil
}

func (l *memoryTransferLog) Finish(_ context.Context, _ string, id string, outcome callrouting.TransferOutcome, _ string, _ time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outcomes[id] = outcome
	return nil
}

func (l *memoryTransferLog) finished(outcome callrouting.TransferOutcome) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, got := range l.outcomes {
		if got == outcome {
			return true
		}
	}
	return false
}

type displayNames map[string]string

func (n displayNames) ResolveUsernames([]string) map[string]string { return n }

type channelAnswerers struct{ grants dialerGrants }

func (p channelAnswerers) MayAnswerCalls(userID, workspaceID, channel string) bool {
	switch channel {
	case callsession.OfferChannelSIP:
		return p.grants.MayCallThroughTrunks(userID, workspaceID, false)
	case callsession.OfferChannelWhatsApp:
		key := userID + "|" + workspaceID + "|"
		return p.grants[key+"call_session|use"] && p.grants[key+"conversations|read"]
	}
	return false
}

const supportQueue = "q-suporte"

func routedStack(t *testing.T, flows workflow.InboundVoiceFlows) *dialerStack {
	return routedStackWithGrace(t, flows, 5*time.Second)
}

func routedStackWithGrace(t *testing.T, flows workflow.InboundVoiceFlows, grace time.Duration) *dialerStack {
	t.Helper()
	library, err := holdmusic.NewLibrary(nil)
	if err != nil {
		t.Fatal(err)
	}
	return startStack(t, dialerStackConfig{mediaTimeout: 30 * time.Second, routing: func(s stackServices) *routingParts {
		activity := callrouting_infra.NewAgentActivity()
		channels := NewCallChannels(activity, s.lifecycle, s.end, nil, log.Default())
		queues := memoryQueues{supportQueue: {
			ID: supportQueue, WorkspaceID: dialerWorkspace, Name: "Suporte", Strategy: callrouting.StrategyLongestIdle,
			MemberUserIDs: []string{colleagueUser}, RingSeconds: 5, MaxWaitSeconds: 20,
		}}
		dispatcher := callrouting_usecase.NewDispatcher(callrouting_usecase.DispatcherDeps{
			Sessions: s.sessions, Permission: channelAnswerers{s.grants}, Ringer: callsession_usecase.NewInboundRinger(s.broker),
			Activity: activity, Members: callrouting_usecase.StaticMembers{}, Music: library,
		})
		transferLog := &memoryTransferLog{outcomes: map[string]callrouting.TransferOutcome{}}
		transfers := callrouting_usecase.NewTransferCall(callrouting_usecase.TransferDeps{
			Calls: channels, Queues: queues, Dispatcher: dispatcher, Ringer: callsession_usecase.NewInboundRinger(s.broker),
			Music: library, Log: transferLog, Names: displayNames{dialerUser: "Ana"}, ReconnectGrace: grace,
		})
		return &routingParts{channels: channels, transfers: transfers, flows: flows, log: transferLog}
	}})
}

func (s *dialerStack) dialIn(t *testing.T) <-chan *diago.DialogClientSession {
	t.Helper()
	dialed := make(chan *diago.DialogClientSession, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := s.provider.Dialer.Invite(ctx, sip.Uri{User: "4000", Host: "127.0.0.1", Port: s.sipPort}, diago.InviteOptions{})
		if err == nil {
			dialed <- d
		}
	}()
	return dialed
}

func answered(t *testing.T, dialed <-chan *diago.DialogClientSession) *diago.DialogClientSession {
	t.Helper()
	select {
	case remote := <-dialed:
		t.Cleanup(func() { _ = remote.Close() })
		return remote
	case <-time.After(10 * time.Second):
		t.Fatal("the caller was never answered")
		return nil
	}
}

func (c *dialerClient) acceptOffer(match func(callsession.InboundCallOffer) bool) callsession.InboundCallOffer {
	c.t.Helper()
	var offer callsession.InboundCallOffer
	c.waitFor(WSEventInboundCall, func(raw json.RawMessage) bool {
		return json.Unmarshal(raw, &offer) == nil && (match == nil || match(offer))
	})
	c.send(WSEventInboundCallAccept, InboundCallActionPayload{OfferID: offer.OfferID})
	return offer
}

func transferStatusIs(status string) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var p callsession.TransferStatus
		return json.Unmarshal(raw, &p) == nil && p.Status == status
	}
}

func (s *dialerStack) takeInboundCall(t *testing.T, agent *dialerClient) *diago.DialogClientSession {
	t.Helper()
	dialed := s.dialIn(t)
	agent.acceptOffer(nil)
	remote := answered(t, dialed)
	agent.waitFor(WSEventCallStatus, statusIs("answered"))
	return remote
}

func (s *dialerStack) waitForOneBilledCall(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.billing.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	events := s.billing.Events(t)
	if len(events) != 1 || !strings.HasPrefix(events[0].CallID, "sip-in-") {
		t.Fatalf("billing events = %+v, want the transferred call billed once", events)
	}
}

func TestAWarmTransferHandsTheCallerToAColleagueWithTheNotes(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "member", UserID: colleagueUser, Notes: "quer cancelar o plano"})
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusRinging))
	offer := bia.acceptOffer(nil)
	if offer.Transfer == nil || offer.Transfer.FromName != "Ana" || offer.Transfer.Notes != "quer cancelar o plano" {
		t.Fatalf("bia's offer = %+v", offer.Transfer)
	}
	bia.waitFor(WSEventCallStatus, statusIs("answered"))
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusConnected))

	go voiptest.SendRTP(remote, 50)
	bia.waitFor(WSEventCallAudioS, nil)

	_ = remote.Hangup(context.Background())
	bia.waitFor(WSEventCallEnded, nil)
	stack.waitForOneBilledCall(t)
	if !stack.routing.log.finished(callrouting.OutcomeConnected) {
		t.Fatal("the transfer was not logged as connected")
	}
}

func TestAColleagueWhoDeclinesSendsTheCallerBackToTheOperator(t *testing.T) {
	stack := routedStack(t, nil)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "member", UserID: colleagueUser})
	var offer callsession.InboundCallOffer
	json.Unmarshal(bia.waitFor(WSEventInboundCall, nil), &offer)
	bia.send(WSEventInboundCallDecline, InboundCallActionPayload{OfferID: offer.OfferID})

	ana.waitFor(WSEventCallStatus, statusIs("answered"))
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusReturned))
	go voiptest.SendRTP(remote, 50)
	ana.waitFor(WSEventCallAudioS, nil)
	_ = remote.Hangup(context.Background())
	ana.waitFor(WSEventCallEnded, nil)
}

func TestAnOperatorCancelsAWarmTransferWhileTheCallerHolds(t *testing.T) {
	stack := routedStack(t, nil)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "member", UserID: colleagueUser})
	bia.waitFor(WSEventInboundCall, nil)
	var status callsession.TransferStatus
	json.Unmarshal(ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusRinging)), &status)
	ana.send(WSEventCallTransferCancel, CallTransferCancelPayload{CallID: status.CallID})

	ana.waitFor(WSEventCallStatus, statusIs("answered"))
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusReturned))
	_ = remote.Hangup(context.Background())
	ana.waitFor(WSEventCallEnded, nil)
}

func TestATransferToAQueueRingsAFreeMember(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "queue", QueueID: supportQueue, Notes: "segunda via"})
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusQueued))
	offer := bia.acceptOffer(nil)
	if offer.Transfer == nil || offer.Transfer.QueueName != "Suporte" || offer.Transfer.Notes != "segunda via" {
		t.Fatalf("bia's offer = %+v", offer.Transfer)
	}
	bia.waitFor(WSEventCallStatus, statusIs("answered"))
	go voiptest.SendRTP(remote, 50)
	bia.waitFor(WSEventCallAudioS, nil)
	_ = remote.Hangup(context.Background())
	bia.waitFor(WSEventCallEnded, nil)
	stack.waitForOneBilledCall(t)
}

func TestTransfersAreRefusedOverTheSocketWhenUnsafe(t *testing.T) {
	stack := routedStack(t, nil)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	stack.takeInboundCall(t, ana)

	cases := []struct {
		payload CallTransferPayload
		code    string
	}{
		{CallTransferPayload{TargetKind: "member", UserID: colleagueUser}, "target_unavailable"},
		{CallTransferPayload{TargetKind: "member", UserID: dialerUser}, "transfer_to_self"},
		{CallTransferPayload{TargetKind: "queue", QueueID: "nope"}, "queue_not_found"},
		{CallTransferPayload{TargetKind: "fax"}, "invalid_target"},
	}
	for _, tc := range cases {
		ana.send(WSEventCallTransfer, tc.payload)
		var got ErrorPayload
		json.Unmarshal(ana.waitFor(WSEventError, nil), &got)
		if got.Code != tc.code {
			t.Fatalf("%+v: code = %q, want %q", tc.payload, got.Code, tc.code)
		}
	}
}

func TestSomeoneWithoutACallHasNothingToTransfer(t *testing.T) {
	stack := routedStack(t, nil)
	listener := stack.connect(t, listenerUser, dialerWorkspace)
	listener.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "queue", QueueID: supportQueue})
	var got ErrorPayload
	json.Unmarshal(listener.waitFor(WSEventError, nil), &got)
	if got.Code != "no_call_to_transfer" {
		t.Fatalf("code = %q, want no_call_to_transfer", got.Code)
	}
}

type queueingFlows struct {
	result chan error
}

func (f *queueingFlows) FlowFor(workspaceID, trunkID string) (*workflow.Workflow, error) {
	return &workflow.Workflow{ID: "wf-ura", Name: "URA", WorkspaceID: workspaceID, Type: workflow.WorkflowTypeVoice}, nil
}

func (f *queueingFlows) Answer(flow *workflow.Workflow, call workflow.InboundVoiceCall) error {
	transfers, ok := call.Call.(workflow.VoiceTransfers)
	if !ok {
		f.result <- workflow.ErrNotTransferable
		return nil
	}
	connected, err := transfers.TransferToQueue(context.Background(), workflow.QueueTransfer{QueueID: supportQueue, Notes: "escolheu suporte", From: flow.Name})
	if err == nil && !connected {
		err = callrouting.ErrTargetUnavailable
	}
	f.result <- err
	return nil
}

func TestAVoiceWorkflowPutsTheCallerInAQueueAndAMemberAnswers(t *testing.T) {
	flows := &queueingFlows{result: make(chan error, 1)}
	stack := routedStack(t, flows)
	defer stack.nothingLeft(t)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	dialed := stack.dialIn(t)
	remote := answered(t, dialed)
	offer := bia.acceptOffer(nil)
	if offer.Transfer == nil || offer.Transfer.FromName != "URA" || offer.Transfer.QueueName != "Suporte" || offer.Transfer.Notes != "escolheu suporte" {
		t.Fatalf("bia's offer = %+v", offer.Transfer)
	}
	bia.waitFor(WSEventCallStatus, statusIs("answered"))
	select {
	case err := <-flows.result:
		if err != nil {
			t.Fatalf("TransferToQueue: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the workflow never learned the caller was answered")
	}

	go voiptest.SendRTP(remote, 50)
	bia.waitFor(WSEventCallAudioS, nil)
	bia.sendSpeech(5)
	_ = remote.Hangup(context.Background())
	bia.waitFor(WSEventCallEnded, nil)
	stack.waitForOneBilledCall(t)
}

func resumeOffer(raw json.RawMessage) bool {
	var offer callsession.InboundCallOffer
	return json.Unmarshal(raw, &offer) == nil && offer.Resume
}

func (s *dialerStack) waitForCallsToEnd(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(s.manager.ActiveCalls(s.trunk.ID)) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(s.manager.ActiveCalls(s.trunk.ID)) != 0 {
		t.Fatal("the trunk still carries the call")
	}
}

func (s *dialerStack) nothingLeft(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:runtime.Stack(buf, true)])
		waiting := strings.Contains(stacks, "(*TransferCall).awaitOwner") || strings.Contains(stacks, "voip.LoopPCM")
		s.routing.channels.mu.RLock()
		tracked := len(s.routing.channels.calls)
		s.routing.channels.mu.RUnlock()
		if !waiting && tracked == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("left behind: waiting goroutines=%v tracked calls=%d", waiting, tracked)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *dialerClient) leave() {
	_ = c.conn.Close()
}

func TestAnOperatorWhoReloadsGetsTheCallerBack(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)

	ana.leave()
	anaAgain := stack.connect(t, dialerUser, dialerWorkspace)
	var offer callsession.InboundCallOffer
	json.Unmarshal(anaAgain.waitFor(WSEventInboundCall, resumeOffer), &offer)
	if offer.Channel != callsession.OfferChannelSIP || offer.FromNumber == "" {
		t.Fatalf("resume offer = %+v", offer)
	}
	anaAgain.send(WSEventInboundCallAccept, InboundCallActionPayload{OfferID: offer.OfferID})
	anaAgain.waitFor(WSEventCallStatus, statusIs("answered"))

	go voiptest.SendRTP(remote, 50)
	anaAgain.waitFor(WSEventCallAudioS, nil)
	_ = remote.Hangup(context.Background())
	anaAgain.waitFor(WSEventCallEnded, nil)
	stack.waitForOneBilledCall(t)
	stack.waitForCallsToEnd(t)
}

func TestACallerIsHungUpWhenTheOperatorNeverComesBack(t *testing.T) {
	stack := routedStackWithGrace(t, nil, 400*time.Millisecond)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)

	ana.leave()
	select {
	case <-remote.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the caller was kept on hold forever")
	}
	stack.waitForOneBilledCall(t)
	stack.waitForCallsToEnd(t)
}

func TestAnOperatorWhoDeclinesTheResumeEndsTheCall(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)

	ana.leave()
	anaAgain := stack.connect(t, dialerUser, dialerWorkspace)
	var offer callsession.InboundCallOffer
	json.Unmarshal(anaAgain.waitFor(WSEventInboundCall, resumeOffer), &offer)
	anaAgain.send(WSEventInboundCallDecline, InboundCallActionPayload{OfferID: offer.OfferID})
	select {
	case <-remote.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the declined call stayed up")
	}
	stack.waitForCallsToEnd(t)
}

func TestAnUnansweredOutboundCallStillEndsWhenTheOperatorLeaves(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ringing := make(chan *diago.DialogServerSession, 1)
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		ringing <- d
		<-d.Context().Done()
	})
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	ana.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-ring"})
	var dialog *diago.DialogServerSession
	select {
	case dialog = <-ringing:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider was never called")
	}
	ana.leave()
	select {
	case <-dialog.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a call nobody answered kept ringing after the operator left")
	}
	stack.waitForCallsToEnd(t)
}

func TestAWarmTransferReturnsToTheOperatorAfterAReload(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	remote := stack.takeInboundCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "member", UserID: colleagueUser})
	var biaOffer callsession.InboundCallOffer
	json.Unmarshal(bia.waitFor(WSEventInboundCall, nil), &biaOffer)
	ana.leave()
	anaAgain := stack.connect(t, dialerUser, dialerWorkspace)
	bia.send(WSEventInboundCallDecline, InboundCallActionPayload{OfferID: biaOffer.OfferID})

	var offer callsession.InboundCallOffer
	json.Unmarshal(anaAgain.waitFor(WSEventInboundCall, resumeOffer), &offer)
	anaAgain.send(WSEventInboundCallAccept, InboundCallActionPayload{OfferID: offer.OfferID})
	anaAgain.waitFor(WSEventCallStatus, statusIs("answered"))
	_ = remote.Hangup(context.Background())
	anaAgain.waitFor(WSEventCallEnded, nil)
	stack.waitForOneBilledCall(t)
}

func (s *dialerStack) takeWhatsAppCall(t *testing.T, agent *dialerClient) *fakeCallSessionCRMCall {
	t.Helper()
	session, ok := s.sessions.FindByUser(dialerWorkspace, dialerUser)
	if !ok {
		t.Fatal("ana has no call session")
	}
	call := newFakeCallSessionCRMCall("wa-in-" + dialerUser)
	call.closeOnHangup = true
	if err := s.executor.AttachInboundCRMCall(context.Background(), callsession.AttachInboundCRMCallInput{
		OfferID: "wa-offer", WorkspaceID: dialerWorkspace, UserID: dialerUser, Session: session,
		PhoneNumber: "5584994409684", Call: call, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("attach WhatsApp call: %v", err)
	}
	call.events <- conversation.CallEvent{Type: conversation.CallEventAnswered}
	agent.waitFor(WSEventCallStatus, statusIs("answered"))
	return call
}

func TestAWhatsAppCallIsTransferredToAColleague(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	call := stack.takeWhatsAppCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "member", UserID: colleagueUser, Notes: "quer falar do pedido"})
	offer := bia.acceptOffer(nil)
	if offer.Channel != callsession.OfferChannelWhatsApp || offer.Transfer == nil || offer.Transfer.Notes != "quer falar do pedido" {
		t.Fatalf("bia's offer = %+v", offer)
	}
	bia.waitFor(WSEventCallStatus, statusIs("answered"))
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusConnected))

	call.audio <- make([]byte, 320)
	bia.waitFor(WSEventCallAudioS, nil)
	bia.sendSpeech(3)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		call.mu.Lock()
		heard := len(call.sendAudioCalls) > 0
		call.mu.Unlock()
		if heard {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	call.mu.Lock()
	heard := len(call.sendAudioCalls) > 0
	call.mu.Unlock()
	if !heard {
		t.Fatal("the WhatsApp caller never heard bia")
	}
	bia.send(WSEventEndCall, map[string]string{"request_id": "end"})
	bia.waitFor(WSEventCallEnded, nil)
}

func TestAWhatsAppCallIsTransferredToAQueue(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	call := stack.takeWhatsAppCall(t, ana)
	bia := stack.connect(t, colleagueUser, dialerWorkspace)

	ana.send(WSEventCallTransfer, CallTransferPayload{TargetKind: "queue", QueueID: supportQueue})
	ana.waitFor(WSEventType(callsession.CallSessionTransferStatus), transferStatusIs(callsession.TransferStatusQueued))
	offer := bia.acceptOffer(nil)
	if offer.Channel != callsession.OfferChannelWhatsApp || offer.Transfer.QueueName != "Suporte" {
		t.Fatalf("bia's offer = %+v", offer)
	}
	bia.waitFor(WSEventCallStatus, statusIs("answered"))
	close(call.done)
	bia.waitFor(WSEventCallEnded, nil)
}

func TestAWhatsAppCallIsKeptForItsOperatorAcrossAReload(t *testing.T) {
	stack := routedStack(t, nil)
	defer stack.nothingLeft(t)
	ana := stack.connect(t, dialerUser, dialerWorkspace)
	call := stack.takeWhatsAppCall(t, ana)

	ana.leave()
	anaAgain := stack.connect(t, dialerUser, dialerWorkspace)
	var offer callsession.InboundCallOffer
	json.Unmarshal(anaAgain.waitFor(WSEventInboundCall, resumeOffer), &offer)
	if offer.Channel != callsession.OfferChannelWhatsApp {
		t.Fatalf("resume offer = %+v", offer)
	}
	anaAgain.send(WSEventInboundCallAccept, InboundCallActionPayload{OfferID: offer.OfferID})
	anaAgain.waitFor(WSEventCallStatus, statusIs("answered"))
	if atomic.LoadInt32(&call.hangupCount) != 0 {
		t.Fatal("the WhatsApp call was hung up during the reload")
	}
	close(call.done)
	anaAgain.waitFor(WSEventCallEnded, nil)
}
