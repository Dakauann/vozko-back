package sip_trunk_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/callrouting"
	cdr "vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/voip"
	"vozko/domain/workflow"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	callsession_usecase "vozko/usecases/callsession"
)

type scriptedFlows struct {
	flow     *workflow.Workflow
	findErr  error
	mu       sync.Mutex
	answered []workflow.InboundVoiceCall
	played   bool
}

func (f *scriptedFlows) FlowFor(workspaceID, trunkID string) (*workflow.Workflow, error) {
	return f.flow, f.findErr
}

func (f *scriptedFlows) Answer(flow *workflow.Workflow, call workflow.InboundVoiceCall) error {
	f.mu.Lock()
	f.answered = append(f.answered, call)
	f.mu.Unlock()
	_, err := call.Call.Play(make([]byte, voip.PCMFrameBytes), true)
	f.played = err == nil
	return nil
}

type recordingLifecycle struct {
	mu        sync.Mutex
	inputs    []callsession_usecase.OutboundCallLifecycleInput
	sawAnswer bool
	done      chan struct{}
}

func newRecordingLifecycle() *recordingLifecycle {
	return &recordingLifecycle{done: make(chan struct{})}
}

func (l *recordingLifecycle) Run(_ context.Context, input callsession_usecase.OutboundCallLifecycleInput) {
	l.mu.Lock()
	l.inputs = append(l.inputs, input)
	l.mu.Unlock()
	defer close(l.done)
	for {
		select {
		case ev, ok := <-input.Call.Events():
			if !ok {
				return
			}
			if ev.Type == conversation.CallEventAnswered {
				l.mu.Lock()
				l.sawAnswer = true
				l.mu.Unlock()
			}
		case <-input.Call.Done():
			return
		}
	}
}

type parkedCall struct {
	callrouting.RoutedCall
	call        conversation.CRMCall
	workspaceID string
	mu          sync.Mutex
	owner       callsession.CallSession
}

func (p *parkedCall) ID() string                 { return p.call.ID() }
func (p *parkedCall) WorkspaceID() string        { return p.workspaceID }
func (p *parkedCall) SendAudio(pcm []byte) error { return p.call.SendAudio(pcm) }
func (p *parkedCall) Done() <-chan struct{}      { return p.call.Done() }
func (p *parkedCall) Hangup() error              { return p.call.Hangup() }
func (p *parkedCall) OwnerSession() (callsession.CallSession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.owner, p.owner != nil
}

type recordingParking struct {
	lifecycle *recordingLifecycle
	mu        sync.Mutex
	parked    []*parkedCall
	onPark    func(*parkedCall)
}

func (r *recordingParking) Park(ctx context.Context, input callrouting.ParkInput) (callrouting.RoutedCall, error) {
	go r.lifecycle.Run(ctx, callsession_usecase.OutboundCallLifecycleInput{
		Call:        input.Call,
		Admission:   input.Admission,
		WorkspaceID: input.WorkspaceID,
		StartedAt:   input.StartedAt,
		Direction:   input.Direction,
		PhoneTo:     input.Phone,
	})
	parked := &parkedCall{call: input.Call, workspaceID: input.WorkspaceID}
	r.mu.Lock()
	r.parked = append(r.parked, parked)
	r.mu.Unlock()
	if r.onPark != nil {
		r.onPark(parked)
	}
	return parked, nil
}

type scriptedQueues struct {
	outcome callrouting.TransferOutcome
	entries []callrouting.QueueEntry
	connect callsession.CallSession
}

func (q *scriptedQueues) EnterQueue(_ context.Context, entry callrouting.QueueEntry) (callrouting.TransferOutcome, error) {
	q.entries = append(q.entries, entry)
	if q.outcome == callrouting.OutcomeConnected {
		parked := entry.Call.(*parkedCall)
		parked.mu.Lock()
		parked.owner = q.connect
		parked.mu.Unlock()
	}
	return q.outcome, nil
}

func voiceFlowFixture(flows *scriptedFlows) (inboundFixture, *recordingLifecycle) {
	f := newInboundFixture(nil, grantedCallers{})
	lifecycle := newRecordingLifecycle()
	f.handler.cfg.VoiceFlows = flows
	f.handler.cfg.Parking = &recordingParking{lifecycle: lifecycle}
	return f, lifecycle
}

func TestAVoiceWorkflowAnswersTheTrunkAndIsBilledLikeAnyCall(t *testing.T) {
	flows := &scriptedFlows{flow: &workflow.Workflow{ID: "wf-1", WorkspaceID: ownerWorkspace, Type: workflow.WorkflowTypeVoice}}
	f, lifecycle := voiceFlowFixture(flows)
	dialog := newRingingDialog()
	f.engine.mu.Lock()
	f.engine.sessions["dialog-1"] = dialog.audio
	f.engine.mu.Unlock()

	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); err != nil {
		t.Fatalf("HandleInboundInvite: %v", err)
	}
	select {
	case <-lifecycle.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the call's billing never finished")
	}

	if dialog.answered.Load() != 1 || f.admission.acquired.Load() != 1 {
		t.Fatalf("answered=%d admitted=%d", dialog.answered.Load(), f.admission.acquired.Load())
	}
	if f.admission.channel.Load() != workspace_pricing.TelephonyChannelSIP {
		t.Fatalf("admitted on channel %v, want sip", f.admission.channel.Load())
	}
	if len(flows.answered) != 1 || !flows.played {
		t.Fatalf("flow answered %d calls, played=%v", len(flows.answered), flows.played)
	}
	call := flows.answered[0]
	if call.CallerNumber != "+5511988887777" || call.TrunkID != "trunk-owned" || call.WorkspaceID != ownerWorkspace {
		t.Fatalf("flow got %+v", call)
	}
	if len(lifecycle.inputs) != 1 {
		t.Fatalf("lifecycle ran %d times", len(lifecycle.inputs))
	}
	billed := lifecycle.inputs[0]
	if billed.Admission == nil || billed.Direction != cdr.DirectionInbound || billed.PhoneTo != "+5511988887777" || !lifecycle.sawAnswer {
		t.Fatalf("billing input = %+v answered=%v", billed, lifecycle.sawAnswer)
	}
	if f.engine.hangupCount() == 0 {
		t.Fatal("the call was not hung up when the flow ended")
	}
	if len(f.executor.attached) != 0 {
		t.Fatal("a member was handed a call the workflow answered")
	}
}

func TestAWorkspaceThatCannotPayNeverRunsTheVoiceWorkflow(t *testing.T) {
	flows := &scriptedFlows{flow: &workflow.Workflow{ID: "wf-1", WorkspaceID: ownerWorkspace, Type: workflow.WorkflowTypeVoice}}
	f, _ := voiceFlowFixture(flows)
	f.admission.acquireErr = callsession.ErrInsufficientBalance
	dialog := newRingingDialog()

	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); !errors.Is(err, callsession.ErrInsufficientBalance) {
		t.Fatalf("err = %v, want insufficient balance", err)
	}
	if dialog.answered.Load() != 0 || len(flows.answered) != 0 {
		t.Fatal("an unpaid call was answered")
	}
}

func TestAnAmbiguousTrunkRefusesTheCallInsteadOfGuessing(t *testing.T) {
	flows := &scriptedFlows{findErr: workflow.ErrVoiceFlowAmbiguous}
	f, _ := voiceFlowFixture(flows)
	dialog := newRingingDialog()

	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); !errors.Is(err, workflow.ErrVoiceFlowAmbiguous) {
		t.Fatalf("err = %v", err)
	}
	if dialog.answered.Load() != 0 || f.admission.acquired.Load() != 0 {
		t.Fatal("an ambiguous call was admitted or answered")
	}
}

func TestATrunkWithoutAVoiceWorkflowStillRingsMembers(t *testing.T) {
	member := newMemberSession("agent", ownerWorkspace)
	f := newInboundFixture(map[string][]callsession.CallSession{ownerWorkspace: {member}}, grantedCallers{"agent|" + ownerWorkspace: true})
	f.handler.cfg.VoiceFlows = &scriptedFlows{}
	f.handler.cfg.Parking = &recordingParking{lifecycle: newRecordingLifecycle()}
	dialog := newRingingDialog()

	go func() { _ = f.handler.HandleInboundInvite(context.Background(), invite(dialog)) }()
	f.accept(t, member)
	close(dialog.gone)
}

type transferringFlows struct {
	flow   *workflow.Workflow
	result chan error
}

func (f *transferringFlows) FlowFor(string, string) (*workflow.Workflow, error) { return f.flow, nil }

func (f *transferringFlows) Answer(_ *workflow.Workflow, call workflow.InboundVoiceCall) error {
	transfers, ok := call.Call.(workflow.VoiceTransfers)
	if !ok {
		f.result <- workflow.ErrNotTransferable
		return nil
	}
	_, err := transfers.TransferToQueue(context.Background(), workflow.QueueTransfer{QueueID: "q1", Notes: "suporte", From: "URA"})
	f.result <- err
	return nil
}

func TestAVoiceWorkflowCallHandedToAnOperatorStaysUpAfterTheFlowEnds(t *testing.T) {
	flows := &transferringFlows{flow: &workflow.Workflow{ID: "wf-1", WorkspaceID: ownerWorkspace, Type: workflow.WorkflowTypeVoice}, result: make(chan error, 1)}
	f := newInboundFixture(nil, grantedCallers{})
	lifecycle := newRecordingLifecycle()
	queues := &scriptedQueues{outcome: callrouting.OutcomeConnected, connect: newMemberSession("agent", ownerWorkspace)}
	f.handler.cfg.VoiceFlows = flows
	f.handler.cfg.Parking = &recordingParking{lifecycle: lifecycle}
	f.handler.cfg.Queues = queues
	dialog := newRingingDialog()
	f.engine.mu.Lock()
	f.engine.sessions["dialog-1"] = dialog.audio
	f.engine.mu.Unlock()

	returned := make(chan error, 1)
	go func() { returned <- f.handler.HandleInboundInvite(context.Background(), invite(dialog)) }()
	if err := <-flows.result; err != nil {
		t.Fatalf("TransferToQueue: %v", err)
	}
	if len(queues.entries) != 1 || queues.entries[0].QueueID != "q1" || queues.entries[0].WorkspaceID != ownerWorkspace || queues.entries[0].From != "URA" {
		t.Fatalf("entries = %+v", queues.entries)
	}
	select {
	case <-returned:
		t.Fatal("the invite finished while the operator was still on the call")
	case <-time.After(100 * time.Millisecond):
	}
	if f.engine.hangupCount() != 0 {
		t.Fatal("the flow hung up a call an operator had taken")
	}
	_ = dialog.audio.Close()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("the invite never finished after the call ended")
	}
}

func TestVoiceWorkflowsCannotTransferWithoutQueues(t *testing.T) {
	flows := &transferringFlows{flow: &workflow.Workflow{ID: "wf-1", WorkspaceID: ownerWorkspace, Type: workflow.WorkflowTypeVoice}, result: make(chan error, 1)}
	f := newInboundFixture(nil, grantedCallers{})
	f.handler.cfg.VoiceFlows = flows
	f.handler.cfg.Parking = &recordingParking{lifecycle: newRecordingLifecycle()}
	dialog := newRingingDialog()
	f.engine.mu.Lock()
	f.engine.sessions["dialog-1"] = dialog.audio
	f.engine.mu.Unlock()

	if err := f.handler.HandleInboundInvite(context.Background(), invite(dialog)); err != nil {
		t.Fatalf("HandleInboundInvite: %v", err)
	}
	if err := <-flows.result; !errors.Is(err, workflow.ErrNotTransferable) {
		t.Fatalf("err = %v", err)
	}
}
