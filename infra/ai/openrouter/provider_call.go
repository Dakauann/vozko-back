package openrouter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

const (
	abandonedStreamDrainTimeout = 15 * time.Second
	abandonedCompletionWait     = 5 * time.Minute
	generationIDHeader          = "X-Generation-Id"
)

type billingTarget struct {
	workspaceID string
	reference   string
	model       string
}

type generationLedger struct {
	mu      sync.Mutex
	id      string
	usage   *openrouter.Usage
	learned chan struct{}
	once    sync.Once
}

func newGenerationLedger() *generationLedger {
	return &generationLedger{learned: make(chan struct{})}
}

func (l *generationLedger) noteID(id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	l.mu.Lock()
	l.id = id
	l.mu.Unlock()
	l.once.Do(func() { close(l.learned) })
}

func (l *generationLedger) noteUsage(usage *openrouter.Usage) {
	if usage == nil {
		return
	}
	l.mu.Lock()
	l.usage = usage
	l.mu.Unlock()
	l.once.Do(func() { close(l.learned) })
}

func (l *generationLedger) snapshot() (string, *openrouter.Usage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.id, l.usage
}

type ledgerKey struct{}

type generationIDRecorder struct {
	next openrouter.HTTPDoer
}

func (r generationIDRecorder) Do(req *http.Request) (*http.Response, error) {
	resp, err := r.next.Do(req)
	if ledger, ok := req.Context().Value(ledgerKey{}).(*generationLedger); ok && resp != nil {
		ledger.noteID(resp.Header.Get(generationIDHeader))
	}
	return resp, err
}

func recordingGenerationIDs(cfg *openrouter.ClientConfig) {
	cfg.HTTPClient = generationIDRecorder{next: cfg.HTTPClient}
}

type callExit int

const (
	callEnded callExit = iota
	callRefused
	callerLeft
)

type providerCall struct {
	ctx       context.Context
	cancel    context.CancelFunc
	ledger    *generationLedger
	target    billingTarget
	stopsAtID bool
	bound     time.Duration
	abandoned chan struct{}
	finished  chan struct{}
}

type streamItem struct {
	chunk openrouter.ChatCompletionStreamResponse
	err   error
}

type completionReply struct {
	response openrouter.ChatCompletionResponse
	err      error
}

func (s *Service) openCall(ctx context.Context, target billingTarget) *providerCall {
	ledger := newGenerationLedger()
	callCtx, cancel := context.WithCancel(context.WithValue(context.WithoutCancel(ctx), ledgerKey{}, ledger))
	return &providerCall{
		ctx:       callCtx,
		cancel:    cancel,
		ledger:    ledger,
		target:    target,
		abandoned: make(chan struct{}),
		finished:  make(chan struct{}),
	}
}

func (s *Service) startStream(ctx context.Context, req openrouter.ChatCompletionRequest, target billingTarget) (*providerCall, <-chan streamItem) {
	call := s.openCall(ctx, target)
	call.stopsAtID = true
	call.bound = s.streamDrainBound()
	items := make(chan streamItem)
	go call.pumpStream(s.client, req, items)
	return call, items
}

func (c *providerCall) pumpStream(client *openrouter.Client, req openrouter.ChatCompletionRequest, items chan<- streamItem) {
	defer close(c.finished)
	defer close(items)
	stream, err := client.CreateChatCompletionStream(c.ctx, req)
	if err != nil {
		c.deliver(items, streamItem{err: fmt.Errorf("stream: %w", err)})
		return
	}
	defer stream.Close()
	for {
		chunk, err := stream.Recv()
		if err == io.EOF || isStreamClosedError(err) {
			return
		}
		if err != nil {
			c.deliver(items, streamItem{err: err})
			return
		}
		c.ledger.noteID(chunk.ID)
		c.ledger.noteUsage(chunk.Usage)
		c.deliver(items, streamItem{chunk: chunk})
	}
}

func (c *providerCall) deliver(items chan<- streamItem, item streamItem) {
	select {
	case items <- item:
	case <-c.abandoned:
	case <-c.ctx.Done():
	}
}

func (s *Service) complete(ctx context.Context, req openrouter.ChatCompletionRequest, target billingTarget) (openrouter.ChatCompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return openrouter.ChatCompletionResponse{}, err
	}
	call := s.openCall(ctx, target)
	call.bound = s.completionBound()
	replies := make(chan completionReply, 1)
	go func() {
		defer close(call.finished)
		resp, err := s.client.CreateChatCompletion(call.ctx, req)
		if err == nil {
			call.ledger.noteID(resp.ID)
			call.ledger.noteUsage(resp.Usage)
		}
		replies <- completionReply{response: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		s.settle(call, callerLeft)
		return openrouter.ChatCompletionResponse{}, ctx.Err()
	case reply := <-replies:
		s.settle(call, exitAfter(reply.err))
		return reply.response, reply.err
	}
}

func exitAfter(err error) callExit {
	if err != nil && neverReachedProvider(err) {
		return callRefused
	}
	return callEnded
}

func neverReachedProvider(err error) bool {
	if _, refused := openrouter.HTTPStatusCode(err); refused {
		return true
	}
	if errors.Is(err, openrouter.ErrChatCompletionInvalidModel) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func (s *Service) settle(call *providerCall, exit callExit) {
	if exit == callRefused {
		call.cancel()
		return
	}
	if exit == callerLeft && call.awaitsOutcome() {
		close(call.abandoned)
		s.drainAbandoned(call)
		return
	}
	call.cancel()
	s.billCall(call.target, call.ledger, leakProviderError)
}

func (c *providerCall) awaitsOutcome() bool {
	id, usage := c.ledger.snapshot()
	if usage != nil {
		return false
	}
	return !c.stopsAtID || id == ""
}

func (s *Service) drainAbandoned(call *providerCall) {
	var enough <-chan struct{}
	if call.stopsAtID {
		enough = call.ledger.learned
	}
	s.recoveries.Add(1)
	go func() {
		defer s.recoveries.Done()
		timer := time.NewTimer(call.bound)
		defer timer.Stop()
		reason := leakProviderError
		select {
		case <-enough:
		case <-call.finished:
		case <-timer.C:
			reason = leakNoIDBeforeTimeout
		}
		call.cancel()
		<-call.finished
		s.billCall(call.target, call.ledger, reason)
	}()
}

func (s *Service) abandonStream(call *providerCall, eventCh chan<- ai.StreamEvent, cause error) {
	if call != nil {
		s.settle(call, callerLeft)
	}
	select {
	case eventCh <- ai.StreamEvent{Type: ai.StreamEventError, Error: cause}:
	default:
	}
}

func (s *Service) streamDrainBound() time.Duration {
	if s.streamDrainTimeout > 0 {
		return s.streamDrainTimeout
	}
	return abandonedStreamDrainTimeout
}

func (s *Service) completionBound() time.Duration {
	if s.completionWait > 0 {
		return s.completionWait
	}
	return abandonedCompletionWait
}
