package callrouting_usecase

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	callsession_usecase "vozko/usecases/callsession"
)

const queueRetryInterval = 2 * time.Second

type DispatcherDeps struct {
	Sessions   callsession.CallSessionRegistry
	Permission callrouting.AnswerPermission
	Ringer     *callsession_usecase.InboundRinger
	Activity   callrouting.AgentActivity
	Members    callrouting.QueueMembers
	Music      callrouting.HoldMusicLibrary
	Logger     *log.Logger
}

type EnqueueInput struct {
	Queue      *callrouting.Queue
	Call       callrouting.RoutedCall
	Notes      string
	FromUserID string
	FromName   string
}

type EnqueueResult struct {
	Outcome     callrouting.TransferOutcome
	AgentUserID string
}

type Dispatcher struct {
	deps  DispatcherDeps
	retry time.Duration
	now   func() time.Time

	mu          sync.Mutex
	waiting     map[string][]*waitingCaller
	lastOffered map[string]string
}

type waitingCaller struct {
	callID       string
	remoteNumber string
	since        time.Time
}

func NewDispatcher(deps DispatcherDeps) *Dispatcher {
	if deps.Logger == nil {
		deps.Logger = log.Default()
	}
	return &Dispatcher{
		deps:        deps,
		retry:       queueRetryInterval,
		now:         time.Now,
		waiting:     map[string][]*waitingCaller{},
		lastOffered: map[string]string{},
	}
}

func (d *Dispatcher) Enqueue(ctx context.Context, input EnqueueInput) (EnqueueResult, error) {
	queue, call := input.Queue, input.Call
	music, err := d.holdMusic(ctx, queue)
	if err != nil {
		return EnqueueResult{}, err
	}
	if err := call.Hold(music); err != nil {
		return EnqueueResult{}, err
	}
	caller := d.join(queue.ID, call)
	defer d.leave(queue.ID, caller)

	deadline := d.now().Add(queue.MaxWait())
	for {
		select {
		case <-call.Done():
			return EnqueueResult{Outcome: callrouting.OutcomeAbandoned}, nil
		case <-ctx.Done():
			return EnqueueResult{Outcome: callrouting.OutcomeAbandoned}, ctx.Err()
		default:
		}
		if !d.now().Before(deadline) {
			return EnqueueResult{Outcome: callrouting.OutcomeTimedOut}, nil
		}
		candidates, err := d.readyAgents(ctx, queue, call.Channel())
		if err != nil {
			return EnqueueResult{}, err
		}
		if d.position(queue.ID, caller) < len(candidates) {
			if result, done := d.ring(ctx, input, candidates, deadline); done {
				return result, nil
			}
		}
		if !d.pause(ctx, call, deadline) {
			continue
		}
	}
}

func (d *Dispatcher) holdMusic(ctx context.Context, queue *callrouting.Queue) ([]byte, error) {
	return playableMusic(ctx, d.deps.Music, queue.WorkspaceID, queue.HoldMusic, d.deps.Logger)
}

func playableMusic(ctx context.Context, library callrouting.HoldMusicLibrary, workspaceID string, ref callrouting.HoldMusicRef, logger *log.Logger) ([]byte, error) {
	music, err := library.PCM(ctx, workspaceID, ref)
	if err == nil {
		return music, nil
	}
	logger.Printf("[CallQueue] hold music %+v unavailable in workspace %s, using the default: %v", ref, workspaceID, err)
	return library.PCM(ctx, workspaceID, callrouting.HoldMusicRef{PresetID: callrouting.DefaultHoldPreset})
}

func (d *Dispatcher) ring(ctx context.Context, input EnqueueInput, candidates []callsession.CallSession, deadline time.Time) (EnqueueResult, bool) {
	queue, call := input.Queue, input.Call
	ringUntil := d.now().Add(queue.RingTimeout() * time.Duration(len(candidates)))
	if ringUntil.After(deadline) {
		ringUntil = deadline
	}
	outcome := d.deps.Ringer.Ring(ctx, callsession_usecase.RingRequest{
		Offer: callsession.InboundCallOffer{
			CallID:      call.ID(),
			WorkspaceID: queue.WorkspaceID,
			FromNumber:  call.RemoteNumber(),
			Channel:     call.Channel(),
			Transfer: &callsession.TransferContext{
				FromUserID: input.FromUserID,
				FromName:   input.FromName,
				QueueID:    queue.ID,
				QueueName:  queue.Name,
				Notes:      input.Notes,
			},
		},
		Candidates:   candidates,
		PerCandidate: queue.RingTimeout(),
		Deadline:     ringUntil,
		CallerGone:   call.Done(),
	})
	if outcome.CallerGone {
		return EnqueueResult{Outcome: callrouting.OutcomeAbandoned}, true
	}
	if outcome.Session == nil {
		return EnqueueResult{}, false
	}
	defer outcome.Session.Release(outcome.OfferID)
	if err := call.Connect(outcome.Session); err != nil {
		d.deps.Logger.Printf("[CallQueue] queue %s could not connect call %s to %s: %v", queue.ID, call.ID(), outcome.Session.UserID(), err)
		return EnqueueResult{}, false
	}
	d.mu.Lock()
	d.lastOffered[queue.ID] = outcome.Session.UserID()
	d.mu.Unlock()
	return EnqueueResult{Outcome: callrouting.OutcomeConnected, AgentUserID: outcome.Session.UserID()}, true
}

func (d *Dispatcher) readyAgents(ctx context.Context, queue *callrouting.Queue, channel string) ([]callsession.CallSession, error) {
	members, err := d.deps.Members.UserIDs(ctx, *queue)
	if err != nil {
		return nil, err
	}
	available := d.answerers(queue.WorkspaceID, channel)
	for userID := range available {
		if !slices.Contains(members, userID) {
			delete(available, userID)
		}
	}
	now := d.now()
	var ready []callrouting.AgentStats
	for _, agent := range d.deps.Activity.Stats(queue.WorkspaceID, members) {
		if _, online := available[agent.UserID]; online && agent.Ready(now, queue.WrapUp()) {
			ready = append(ready, agent)
		}
	}
	d.mu.Lock()
	lastOffered := d.lastOffered[queue.ID]
	d.mu.Unlock()
	ranked := callrouting.RankAgents(queue.Strategy, ready, lastOffered)
	sessions := make([]callsession.CallSession, 0, len(ranked))
	for _, agent := range ranked {
		sessions = append(sessions, available[agent.UserID])
	}
	return sessions, nil
}

func (d *Dispatcher) answerers(workspaceID, channel string) map[string]callsession.CallSession {
	answerers := map[string]callsession.CallSession{}
	for _, session := range d.deps.Sessions.ListAvailable(workspaceID) {
		if session == nil || session.WorkspaceID() != workspaceID {
			continue
		}
		if d.mayAnswer(session.UserID(), workspaceID, channel) {
			answerers[session.UserID()] = session
		}
	}
	return answerers
}

func (d *Dispatcher) mayAnswer(userID, workspaceID, channel string) bool {
	return d.deps.Permission != nil && d.deps.Permission.MayAnswerCalls(userID, workspaceID, channel)
}

func (d *Dispatcher) mayAnswerAny(userID, workspaceID string) bool {
	for _, channel := range callsession.OfferChannels() {
		if d.mayAnswer(userID, workspaceID, channel) {
			return true
		}
	}
	return false
}

func (d *Dispatcher) pause(ctx context.Context, call callrouting.RoutedCall, deadline time.Time) bool {
	wait := min(d.retry, time.Until(deadline))
	if wait <= 0 {
		return false
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-call.Done():
	case <-ctx.Done():
	}
	return true
}

func (d *Dispatcher) join(queueID string, call callrouting.RoutedCall) *waitingCaller {
	caller := &waitingCaller{callID: call.ID(), remoteNumber: call.RemoteNumber(), since: d.now()}
	d.mu.Lock()
	d.waiting[queueID] = append(d.waiting[queueID], caller)
	d.mu.Unlock()
	return caller
}

func (d *Dispatcher) leave(queueID string, caller *waitingCaller) {
	d.mu.Lock()
	defer d.mu.Unlock()
	line := d.waiting[queueID]
	if i := slices.Index(line, caller); i >= 0 {
		d.waiting[queueID] = slices.Delete(line, i, i+1)
	}
	if len(d.waiting[queueID]) == 0 {
		delete(d.waiting, queueID)
	}
}

func (d *Dispatcher) position(queueID string, caller *waitingCaller) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Index(d.waiting[queueID], caller)
}

func (d *Dispatcher) Waiting(queueID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.waiting[queueID])
}

func (d *Dispatcher) Live(ctx context.Context, queue *callrouting.Queue) (callrouting.QueueLive, error) {
	members, err := d.deps.Members.UserIDs(ctx, *queue)
	if err != nil {
		return callrouting.QueueLive{}, err
	}
	online := map[string]callsession.MemberPresence{}
	for _, p := range d.deps.Sessions.ListPresence(queue.WorkspaceID) {
		online[p.UserID] = p
	}
	now := d.now()
	agents := make([]callrouting.AgentStatus, 0, len(members))
	for _, stats := range d.deps.Activity.Stats(queue.WorkspaceID, members) {
		p, isOnline := online[stats.UserID]
		presence := callrouting.AgentPresence{
			Online:    isOnline && p.HasBrowser,
			MayAnswer: d.mayAnswerAny(stats.UserID, queue.WorkspaceID),
			OnCall:    p.OnCall,
			Ringing:   p.Ringing,
		}
		agents = append(agents, callrouting.AgentStatus{UserID: stats.UserID, State: callrouting.StateOf(presence, stats, now, queue.WrapUp())})
	}
	return callrouting.QueueLive{QueueID: queue.ID, Name: queue.Name, Waiting: d.waitingCallers(queue.ID), Agents: agents}, nil
}

func (d *Dispatcher) waitingCallers(queueID string) []callrouting.WaitingCaller {
	d.mu.Lock()
	defer d.mu.Unlock()
	line := d.waiting[queueID]
	out := make([]callrouting.WaitingCaller, 0, len(line))
	for _, caller := range line {
		out = append(out, callrouting.WaitingCaller{CallID: caller.callID, RemoteNumber: caller.remoteNumber, Since: caller.since})
	}
	return out
}
