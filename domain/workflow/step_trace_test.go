package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/tracepayload"
)

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

var _ net.Error = timeoutNetError{}

func TestClassifyErrorNamesTheCauseSoTheStepSaysWhatToFix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"no error", nil, ""},
		{"executor declared kind wins over the wrapped cause", Fail(ErrorKindScript, fmt.Errorf("action_code: %w", context.DeadlineExceeded)), ErrorKindScript},
		{"declared kind survives wrapping", fmt.Errorf("outer: %w", Fail(ErrorKindHTTPStatus, errors.New("HTTP 500"))), ErrorKindHTTPStatus},
		{"deadline is a timeout", fmt.Errorf("send: %w", context.DeadlineExceeded), ErrorKindTimeout},
		{"network timeout is a timeout", fmt.Errorf("dial: %w", timeoutNetError{}), ErrorKindTimeout},
		{"cancelled context", fmt.Errorf("x: %w", context.Canceled), ErrorKindCancelled},
		{"missing config", fmt.Errorf("node: %w", ErrNodeConfigMissing), ErrorKindConfig},
		{"unreadable gate", fmt.Errorf("%w: entry", ErrAutomationStateUnreadable), ErrorKindGate},
		{"circuit breaker", ErrMaxExecutionsReached, ErrorKindGuard},
		{"lifetime breaker", ErrDurableExecutionsReached, ErrorKindGuard},
		{"no executor", ErrNodeExecutorNotFound, ErrorKindGuard},
		{"egress refusal is a transport failure", fmt.Errorf("post: %w", shared.ErrEgressBlocked), ErrorKindTransport},
		{"a closed service window says a template is needed", fmt.Errorf("send text: %w", conversation.ErrWindowClosed), ErrorKindWindowClosed},
		{"anything else is internal", errors.New("boom"), ErrorKindInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Fatalf("ClassifyError = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNodeFailureKeepsTheCauseForErrorsIs(t *testing.T) {
	err := Fail(ErrorKindTimeout, fmt.Errorf("fetch: %w", context.DeadlineExceeded))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("the declared kind must not hide the cause from errors.Is")
	}
	if err.Error() != "fetch: context deadline exceeded" {
		t.Fatalf("message = %q", err.Error())
	}
	if Fail(ErrorKindConfig, nil) != nil {
		t.Fatal("no error stays no error")
	}
}

func TestErrorChainListsWrappedCausesOutermostFirstWithoutRepeats(t *testing.T) {
	root := errors.New("connection refused")
	mid := fmt.Errorf("login: %w", root)
	outer := Fail(ErrorKindTransport, fmt.Errorf("n55: %w", mid))

	got := ErrorChain(outer)
	want := []string{"n55: login: connection refused", "login: connection refused", "connection refused"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chain = %q, want %q", got, want)
	}
	if ErrorChain(nil) != nil {
		t.Fatal("no error has no chain")
	}
}

func TestErrorChainFollowsJoinedErrorsAndStopsAtItsDepth(t *testing.T) {
	joined := errors.Join(errors.New("first"), errors.New("second"))
	got := ErrorChain(fmt.Errorf("both: %w", joined))
	if len(got) != 4 || got[2] != "first" || got[3] != "second" {
		t.Fatalf("chain = %q", got)
	}

	var deep error = errors.New("root")
	for i := 0; i < 40; i++ {
		deep = fmt.Errorf("level %d: %w", i, deep)
	}
	if n := len(ErrorChain(deep)); n != MaxErrorChain {
		t.Fatalf("chain length = %d, want the %d cap", n, MaxErrorChain)
	}
}

func TestBranchLabelIsTheLabelOfTheEdgeTaken(t *testing.T) {
	g := Graph{Edges: []Edge{
		{Source: "n1", Target: "ok", Label: "sucesso"},
		{Source: "n1", Target: "bad", Label: "falha"},
		{Source: "n1", Target: "bad", Label: "timeout"},
		{Source: "n2", Target: "next"},
	}}
	cases := []struct {
		name, source, target, hint, want string
	}{
		{"single edge to the target", "n1", "ok", "", "sucesso"},
		{"two edges to the same target follow the executor hint", "n1", "bad", "timeout", "timeout"},
		{"hint that is not an edge falls back to the first edge", "n1", "bad", "other", "falha"},
		{"unlabelled default edge", "n2", "next", "", ""},
		{"no edge", "n1", "nowhere", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.BranchLabel(tc.source, tc.target, tc.hint); got != tc.want {
				t.Fatalf("BranchLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStepTraceIsSafeWhenNoTraceIsAttached(t *testing.T) {
	var trace *StepTrace
	trace.RecordHTTP(HTTPExchange{Method: "GET"})
	trace.RecordScriptLogs([]ScriptLog{{Level: "info", Message: "x"}})
	trace.RecordStack("stack")
	if trace.Exchanges() != nil || trace.Logs() != nil || trace.StackText() != "" {
		t.Fatal("a nil trace records nothing")
	}
}

func TestStepTraceCollectsWhatTheExecutorSaw(t *testing.T) {
	trace := &StepTrace{}
	trace.RecordHTTP(HTTPExchange{Method: "POST", URL: "https://a"})
	trace.RecordHTTP(HTTPExchange{Method: "GET", URL: "https://b"})
	trace.RecordScriptLogs([]ScriptLog{{Level: "info", Message: "etapa 1"}})
	trace.RecordStack("Error: x\n\tat (3:5)")
	if len(trace.Exchanges()) != 2 || trace.Logs()[0].Message != "etapa 1" || trace.StackText() == "" {
		t.Fatalf("trace = %+v", trace)
	}
}

func TestFitHTTPExchangesKeepsTheFieldUnderTheCap(t *testing.T) {
	big := strings.Repeat("x", tracepayload.MaxBytes)
	exchanges := []HTTPExchange{
		{Method: "POST", URL: "https://a", RequestBody: big, ResponseBody: big, Status: 200},
		{Method: "GET", URL: "https://b", ResponseBody: big, Status: 500},
	}
	fitted := FitHTTPExchanges(exchanges)
	encoded, _ := json.Marshal(fitted)
	if len(encoded) > tracepayload.MaxBytes+1024 {
		t.Fatalf("fitted exchanges are %d bytes", len(encoded))
	}
	for i, x := range fitted {
		if !x.Truncated {
			t.Fatalf("exchange %d must say its body was cut", i)
		}
		if x.Status != exchanges[i].Status || x.URL != exchanges[i].URL {
			t.Fatalf("exchange %d lost its identity: %+v", i, x)
		}
	}
	if len(exchanges[0].RequestBody) != tracepayload.MaxBytes {
		t.Fatal("fitting must not mutate the caller's exchanges")
	}
	small := []HTTPExchange{{Method: "GET", URL: "https://c", ResponseBody: "ok"}}
	if got := FitHTTPExchanges(small); got[0].Truncated || got[0].ResponseBody != "ok" {
		t.Fatalf("small exchange changed: %+v", got[0])
	}
}

func TestFitScriptLogsStopsAtTheCapAndSaysHowManyWereLeftOut(t *testing.T) {
	logs := make([]ScriptLog, 0, 100)
	for i := 0; i < 100; i++ {
		logs = append(logs, ScriptLog{Level: "info", Message: strings.Repeat("m", 2048)})
	}
	fitted := FitScriptLogs(logs)
	encoded, _ := json.Marshal(fitted)
	if len(encoded) > tracepayload.MaxBytes+1024 {
		t.Fatalf("fitted logs are %d bytes", len(encoded))
	}
	last := fitted[len(fitted)-1]
	if last.Level != ScriptLogLevelTruncated || !strings.Contains(last.Message, "omitid") {
		t.Fatalf("last entry = %+v, want the truncation note", last)
	}
	if got := FitScriptLogs(logs[:2]); len(got) != 2 {
		t.Fatalf("small log list changed: %d entries", len(got))
	}
}

func TestStartedByNamesWhoStartedTheRun(t *testing.T) {
	cases := []struct{ got, want string }{
		{StartedByAgent("a1"), "agent:a1"},
		{StartedByWebhook("d1"), "webhook:d1"},
		{StartedByExit("m1"), "exit:m1"},
		{StartedByManual("u1"), "manual:u1"},
		{StartedByTrigger(TriggerMessageReceived), "trigger:trigger_message_received"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestNewStatusesAreDistinctFromExecutedAndFailed(t *testing.T) {
	seen := map[LogStatus]bool{}
	for _, s := range []LogStatus{LogStatusExecuted, LogStatusFailed, LogStatusSkipped, LogStatusCancelled, LogStatusWaiting, LogStatusGate} {
		if seen[s] || s == "" {
			t.Fatalf("status %q repeats or is empty", s)
		}
		seen[s] = true
	}
}

func TestStepErrorMessageShowsTheCodedMessageOfAKindAndTheTextOtherwise(t *testing.T) {
	cases := []struct {
		name      string
		kind      ErrorKind
		technical string
		want      string
	}{
		{"a closed window reads as the template advice", ErrorKindWindowClosed, "conversation: 24-hour messaging window is closed", WindowClosedFix},
		{"a kind with no coded message keeps the text the builder needs", ErrorKindHTTPStatus, "status 502", "status 502"},
		{"no kind keeps the text", "", "boom", "boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StepErrorMessage(c.kind, c.technical); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
