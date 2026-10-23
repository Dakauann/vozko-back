package workflow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/redact"
	"vozko/domain/shared"
	"vozko/domain/tracepayload"
)

type ErrorKind string

const (
	ErrorKindConfig     ErrorKind = "config"
	ErrorKindTimeout    ErrorKind = "timeout"
	ErrorKindHTTPStatus ErrorKind = "http_status"
	ErrorKindTransport  ErrorKind = "transport"
	ErrorKindScript     ErrorKind = "script"
	ErrorKindPanic      ErrorKind = "panic"
	ErrorKindGuard      ErrorKind = "guard"
	ErrorKindGate       ErrorKind = "gate"
	ErrorKindValidation ErrorKind = "validation"
	ErrorKindCancelled  ErrorKind = "cancelled"
	ErrorKindInternal   ErrorKind = "internal"
	ErrorKindHandled    ErrorKind = "handled"

	ErrorKindRealCallNotConfirmed ErrorKind = "real_call_not_confirmed"
	ErrorKindWrongWorkflowType    ErrorKind = "wrong_workflow_type"
	ErrorKindMissingMocks         ErrorKind = "missing_mocks"
	ErrorKindTargetNotReached     ErrorKind = "target_not_reached"
	ErrorKindWindowClosed         ErrorKind = "window_closed"
)

type NodeFailure struct {
	Kind ErrorKind
	Err  error
}

func (f *NodeFailure) Error() string { return f.Err.Error() }

func (f *NodeFailure) Unwrap() error { return f.Err }

func Fail(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &NodeFailure{Kind: kind, Err: err}
}

var guardErrors = []error{
	ErrMaxExecutionsReached,
	ErrDurableExecutionsReached,
	ErrNodeExecutorNotFound,
	ErrWaitDuringCall,
}

func ClassifyError(err error) ErrorKind {
	if err == nil {
		return ""
	}
	var failure *NodeFailure
	if errors.As(err, &failure) && failure.Kind != "" {
		return failure.Kind
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorKindTimeout
	case errors.Is(err, context.Canceled):
		return ErrorKindCancelled
	case errors.Is(err, ErrNodeConfigMissing):
		return ErrorKindConfig
	case errors.Is(err, ErrAutomationStateUnreadable):
		return ErrorKindGate
	case errors.Is(err, shared.ErrEgressBlocked):
		return ErrorKindTransport
	case errors.Is(err, conversation.ErrWindowClosed):
		return ErrorKindWindowClosed
	}
	for _, guard := range guardErrors {
		if errors.Is(err, guard) {
			return ErrorKindGuard
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrorKindTimeout
	}
	return ErrorKindInternal
}

var stepErrorMessages = map[ErrorKind]string{
	ErrorKindWindowClosed: WindowClosedFix,
}

func StepErrorMessage(kind ErrorKind, technical string) string {
	if message, coded := stepErrorMessages[kind]; coded {
		return message
	}
	return technical
}

const MaxErrorChain = 16

func ErrorChain(err error) []string {
	if err == nil {
		return nil
	}
	chain := make([]string, 0, 4)
	var walk func(error)
	walk = func(e error) {
		for e != nil && len(chain) < MaxErrorChain {
			if msg := e.Error(); len(chain) == 0 || chain[len(chain)-1] != msg {
				chain = append(chain, msg)
			}
			if joined, ok := e.(interface{ Unwrap() []error }); ok {
				for _, inner := range joined.Unwrap() {
					walk(inner)
				}
				return
			}
			e = errors.Unwrap(e)
		}
	}
	walk(err)
	return chain
}

func ResolvedConfig(node *Node, state *RunState) map[string]interface{} {
	if node == nil {
		return nil
	}
	if node.Type.Canonical() == NodeTypeActionCode {
		return node.Config
	}
	if node.Type.SendsToCustomer() {
		return renderConfig(node.Config, func(text string) string {
			rendered, _ := RenderCustomerText(text, state)
			return rendered
		})
	}
	return InterpolateMap(node.Config, state, nil)
}

func (g *Graph) BranchLabel(source, target, hint string) string {
	label, found := "", false
	for _, e := range g.Edges {
		if e.Source != source || e.Target != target {
			continue
		}
		if hint != "" && e.Label == hint {
			return e.Label
		}
		if !found {
			label, found = e.Label, true
		}
	}
	return label
}

type HTTPExchange struct {
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	RequestHeaders  map[string]string `json:"requestHeaders,omitempty"`
	RequestBody     string            `json:"requestBody,omitempty"`
	Status          int               `json:"status,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    string            `json:"responseBody,omitempty"`
	LatencyMs       int64             `json:"latencyMs"`
	TLSFallback     bool              `json:"tlsFallback,omitempty"`
	Redirects       int               `json:"redirects,omitempty"`
	EgressRefusal   string            `json:"egressRefusal,omitempty"`
	Error           string            `json:"error,omitempty"`
	Truncated       bool              `json:"truncated,omitempty"`
}

func (x HTTPExchange) Redact(r redact.Redactor) HTTPExchange {
	x.URL = r.String(x.URL)
	x.RequestHeaders = redactHeaders(r, x.RequestHeaders)
	x.RequestBody = r.String(x.RequestBody)
	x.ResponseHeaders = redactHeaders(r, x.ResponseHeaders)
	x.ResponseBody = r.String(x.ResponseBody)
	x.EgressRefusal = r.String(x.EgressRefusal)
	x.Error = r.String(x.Error)
	return x
}

func redactHeaders(r redact.Redactor, headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	masked, _ := r.Value(headers).(map[string]string)
	return masked
}

const minExchangeBodyBytes = 1024

func FitHTTPExchanges(exchanges []HTTPExchange) []HTTPExchange {
	if len(exchanges) == 0 {
		return nil
	}
	bodyBudget := max(tracepayload.MaxBytes/(4*len(exchanges)), minExchangeBodyBytes)
	out := make([]HTTPExchange, len(exchanges))
	for i, x := range exchanges {
		var cutRequest, cutResponse bool
		x.RequestBody, cutRequest = tracepayload.Text(x.RequestBody, bodyBudget)
		x.ResponseBody, cutResponse = tracepayload.Text(x.ResponseBody, bodyBudget)
		x.Truncated = x.Truncated || cutRequest || cutResponse
		out[i] = x
	}
	return out
}

const ScriptLogLevelTruncated = "truncated"

type ScriptLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

const scriptLogEntryOverhead = 32

func FitScriptLogs(logs []ScriptLog) []ScriptLog {
	if len(logs) == 0 {
		return nil
	}
	budget := tracepayload.MaxBytes - 256
	out := make([]ScriptLog, 0, len(logs))
	for i, entry := range logs {
		cost := len(entry.Level) + len(entry.Message) + scriptLogEntryOverhead
		if cost > budget {
			out = append(out, ScriptLog{
				Level:   ScriptLogLevelTruncated,
				Message: fmt.Sprintf("%d entradas de log omitidas, o limite de %d KB foi atingido", len(logs)-i, tracepayload.MaxBytes/1024),
			})
			return out
		}
		budget -= cost
		out = append(out, entry)
	}
	return out
}

type StepTrace struct {
	exchanges  []HTTPExchange
	logs       []ScriptLog
	stack      string
	scrubbers  []redact.Redactor
	unresolved []string
}

const (
	MaxUnresolvedRefs     = 50
	maxUnresolvedRefBytes = 200
)

func (t *StepTrace) RecordUnresolved(refs ...string) {
	if t == nil {
		return
	}
	for _, ref := range refs {
		ref, _ = tracepayload.Text(ref, maxUnresolvedRefBytes)
		if len(t.unresolved) >= MaxUnresolvedRefs || slices.Contains(t.unresolved, ref) {
			continue
		}
		t.unresolved = append(t.unresolved, ref)
	}
}

func (t *StepTrace) Unresolved() []string {
	if t == nil {
		return nil
	}
	return t.unresolved
}

func (t *StepTrace) RecordHTTP(x HTTPExchange) {
	if t == nil {
		return
	}
	t.exchanges = append(t.exchanges, x)
}

func (t *StepTrace) RecordScriptLogs(logs []ScriptLog) {
	if t == nil {
		return
	}
	t.logs = append(t.logs, logs...)
}

func (t *StepTrace) RecordStack(stack string) {
	if t == nil {
		return
	}
	t.stack = stack
}

func (t *StepTrace) ScrubWith(r redact.Redactor) {
	if t == nil {
		return
	}
	t.scrubbers = append(t.scrubbers, r)
}

func (t *StepTrace) Exchanges() []HTTPExchange {
	if t == nil {
		return nil
	}
	return t.exchanges
}

func (t *StepTrace) Logs() []ScriptLog {
	if t == nil {
		return nil
	}
	return t.logs
}

func (t *StepTrace) StackText() string {
	if t == nil {
		return ""
	}
	return t.stack
}

func (t *StepTrace) Scrubbers() []redact.Redactor {
	if t == nil {
		return nil
	}
	return t.scrubbers
}

const (
	startedByAgent   = "agent:"
	startedByWebhook = "webhook:"
	startedByExit    = "exit:"
	startedByManual  = "manual:"
	startedByTrigger = "trigger:"
)

func StartedByAgent(agentID string) string { return startedByAgent + agentID }

func StartedByWebhook(deliveryID string) string { return startedByWebhook + deliveryID }

func StartedByExit(ref string) string { return startedByExit + ref }

func StartedByManual(userID string) string { return startedByManual + userID }

func StartedByTrigger(t TriggerType) string { return startedByTrigger + string(t) }

func StepDuration(started, finished time.Time) int64 {
	if finished.Before(started) {
		return 0
	}
	return finished.Sub(started).Milliseconds()
}
