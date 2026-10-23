package transactionalemail

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	es "vozko/domain/emailsending"
	"vozko/domain/lead"
)

func TestSendRecordsTheSentEmailInTheRecipientConversation(t *testing.T) {
	cases := []struct {
		name   string
		origin Origin
		by     conversation.SentBy
		opens  bool
	}{
		{"workflow node", OriginWorkflow, conversation.SentByWorkflow("wf-1"), false},
		{"agent tool", OriginAgent, conversation.SentByAI("agent-1"), false},
		{"assistant tool", OriginCopilot, conversation.SentByAI(""), false},
		{"operator outreach", OriginOutreach, conversation.SentByPerson("8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			req := textRequest()
			req.Origin, req.SentBy, req.SenderID = tc.origin, tc.by, "sender-9"
			res, err := h.service(t).Send(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.transcript.emails) != 1 {
				t.Fatalf("transcripts = %d, want 1", len(h.transcript.emails))
			}
			got := h.transcript.emails[0]
			want := em.OutboundEmail{
				WorkspaceID: "ws-1", LeadID: "lead-1", LeadName: "Ana Souza", To: "ana@acme.com.br", SenderID: "sender-9",
				Subject: "Pedido de Ana", Text: "Oi Ana,\n\nplano ouro & pedido A-17.", ExternalID: "prov-1",
				SentBy: tc.by, OpensContact: tc.opens, HTML: got.HTML,
			}
			if !strings.Contains(got.HTML, "pedido A-17") {
				t.Fatalf("transcript html = %q, want the composed html", got.HTML)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("transcript = %+v, want %+v", got, want)
			}
			if res.ConversationID != "conv-of-prov-1" || res.MessageID != "msg-1" {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

func TestSendRecordsTheSenderTheGatewayChose(t *testing.T) {
	h := newHarness()
	h.gateway.envelope = es.Envelope{MessageID: "env-1", Sender: es.Sender{ID: "default-sender"}}
	if _, err := h.service(t).Send(context.Background(), textRequest()); err != nil {
		t.Fatal(err)
	}
	if h.transcript.emails[0].SenderID != "default-sender" {
		t.Fatalf("sender = %q", h.transcript.emails[0].SenderID)
	}
}

func TestSendFindsTheConversationAgainForARepeatedKey(t *testing.T) {
	h := newHarness()
	h.runs.rows = []es.Message{{ID: "kept", ProviderMessageID: "prov-kept", Status: es.MessageSent, Address: "ana@acme.com.br"}}
	res, err := h.service(t).Send(context.Background(), textRequest())
	if err != nil || !res.Repeated || res.ConversationID != "conv-of-prov-kept" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if len(h.billing.charged) != 0 || len(h.gateway.requests) != 0 || len(h.transcript.emails) != 1 {
		t.Fatalf("a repeat neither charges nor sends: charged %v sent %v", h.billing.charged, h.gateway.requests)
	}

	inFlight := newHarness()
	inFlight.runs.rows = []es.Message{{ID: "kept", Status: es.MessageQueued, Address: "ana@acme.com.br"}}
	res, err = inFlight.service(t).Send(context.Background(), textRequest())
	if err != nil || !res.Repeated || res.ConversationID != "" || len(inFlight.transcript.emails) != 0 {
		t.Fatalf("a send still in flight is recorded by its own call: %+v, %v, %v", res, err, inFlight.transcript.emails)
	}
}

func TestSendKeepsASentEmailWhenItsTranscriptFails(t *testing.T) {
	h := newHarness()
	h.transcript.err = errors.New("db down")
	var logged []string
	s, err := New(Deps{Gateway: h.gateway, Billing: h.billing, Runs: h.runs, Designs: h.designs, LeadFacts: h.facts, Leads: h.leads, Transcript: h.transcript,
		Logf: func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Send(context.Background(), textRequest())
	if err != nil || res.MessageID != "msg-1" || res.ConversationID != "" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if want := []event{"charge", "send", "transcribe"}; !reflect.DeepEqual(h.log.events, want) {
		t.Fatalf("the email went out, so it is neither refunded nor resent: %v", h.log.events)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "CRITICAL") || !strings.Contains(logged[0], "msg-1") {
		t.Fatalf("logged = %v", logged)
	}
}

func TestSendNeverRecordsARefusedEmail(t *testing.T) {
	h := newHarness()
	h.gateway.err = es.ErrRecipientSuppressed
	if _, err := h.service(t).Send(context.Background(), textRequest()); err == nil || len(h.transcript.emails) != 0 {
		t.Fatalf("err = %v, transcripts %v", err, h.transcript.emails)
	}
}

func TestOutreachRefusesWhatTheSharedLeadRulesRefuse(t *testing.T) {
	cases := []struct {
		name   string
		origin Origin
		leadID string
		code   string
	}{
		{"an opted-out lead is not reached by an operator", OriginOutreach, "opted", CodeLeadOptedOut},
		{"a blocked lead is not reached by an operator", OriginOutreach, "blocked", CodeLeadBlocked},
		{"a lead without an email", OriginOutreach, "no-email", CodeLeadNoEmail},
		{"a lead without an email from a workflow", OriginWorkflow, "no-email", CodeLeadNoEmail},
	}
	for _, tc := range cases {
		h := newHarness()
		h.facts.facts["no-email"] = lead.EmailFacts{ID: "no-email", Name: "Dani"}
		req := textRequest()
		req.Origin, req.LeadID, req.To = tc.origin, tc.leadID, ""
		req.SentBy = conversation.SentByPerson("8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b")
		_, err := h.service(t).Send(context.Background(), req)
		if code := codeOf(t, err); code != tc.code {
			t.Errorf("%s: code = %s (%v), want %s", tc.name, code, err, tc.code)
		}
		if len(h.log.events) != 0 {
			t.Errorf("%s: refused before any charge: %v", tc.name, h.log.events)
		}
	}
}

func TestSendRefusesASenderThatIsNotTheBusiness(t *testing.T) {
	h := newHarness()
	for _, by := range []conversation.SentBy{{}, conversation.SentByContact("ana@acme.com.br"), conversation.SentByPerson("")} {
		req := textRequest()
		req.SentBy = by
		if _, err := h.service(t).Send(context.Background(), req); !errors.Is(err, ErrSentByInvalid) || CodeOf(err) != CodeRequestInvalid {
			t.Errorf("%+v: err = %v", by, err)
		}
	}
	if len(h.log.events) != 0 {
		t.Fatalf("events = %v", h.log.events)
	}
}

func TestAThrottledSendIsRefundedAndRetryableWithItsHint(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      string
		retryable bool
		seconds   int64
	}{
		{"pacer out of priority tokens", &es.SendError{Kind: es.SendErrorTransient, Detail: "pacing: rate", RetryAfter: 1500 * time.Millisecond}, CodeSendThrottled, true, 2},
		{"daily quota used", &es.SendError{Kind: es.SendErrorTransient, Detail: "pacing: daily", RetryAfter: es.DailyQuotaRetryAfter}, CodeSendThrottled, true, 900},
		{"a sub second hint still waits a second", &es.SendError{Kind: es.SendErrorTransient, RetryAfter: 10 * time.Millisecond}, CodeSendThrottled, true, 1},
		{"a transient failure without a hint", &es.SendError{Kind: es.SendErrorTransient, Detail: "timeout"}, CodeSendFailed, false, 0},
		{"a rejection", &es.SendError{Kind: es.SendErrorRejected, Detail: "bad", RetryAfter: time.Second}, CodeSendFailed, false, 0},
	}
	for _, tc := range cases {
		h := newHarness()
		h.gateway.err = fmt.Errorf("gateway: %w", tc.err)
		_, err := h.service(t).Send(context.Background(), textRequest())
		if code := codeOf(t, err); code != tc.code {
			t.Errorf("%s: code = %s, want %s", tc.name, code, tc.code)
		}
		if Retryable(err) != tc.retryable {
			t.Errorf("%s: retryable = %v", tc.name, Retryable(err))
		}
		seconds, ok := RetryAfterSeconds(err)
		if seconds != tc.seconds || ok != (tc.seconds > 0) {
			t.Errorf("%s: retry after = %d %v, want %d", tc.name, seconds, ok, tc.seconds)
		}
		if want := []event{"charge", "send", "refund"}; !reflect.DeepEqual(h.log.events, want) {
			t.Errorf("%s: events = %v", tc.name, h.log.events)
		}
	}
}

func TestRetryableCodes(t *testing.T) {
	for code, want := range map[string]bool{
		CodeSendThrottled: true, CodeSendInProgress: true, CodeSendFailed: false, CodeBalanceInsufficient: false, CodeLeadBlocked: false,
	} {
		if got := Retryable(&Refusal{Code: code, Err: errors.New(code)}); got != want {
			t.Errorf("%s: retryable = %v, want %v", code, got, want)
		}
	}
	if Retryable(nil) || Retryable(errors.New("boom")) {
		t.Fatal("only coded refusals are retryable")
	}
}

func TestRetryHint(t *testing.T) {
	throttled := &Refusal{Code: CodeSendThrottled, Err: &es.SendError{Kind: es.SendErrorTransient, RetryAfter: 3 * time.Second}}
	if hint := RetryHint(throttled); !strings.Contains(hint, "temporário") || !strings.Contains(hint, "3 segundos") || !strings.Contains(hint, "nada foi cobrado") {
		t.Fatalf("hint = %q", hint)
	}
	if hint := RetryHint(&Refusal{Code: CodeSendInProgress, Err: ErrSendInProgress}); !strings.Contains(hint, "temporário") || strings.Contains(hint, "segundos") {
		t.Fatalf("in progress hint = %q", hint)
	}
	if hint := RetryHint(&Refusal{Code: CodeLeadBlocked, Err: ErrLeadBlocked}); hint != "" {
		t.Fatalf("permanent refusal hint = %q", hint)
	}
}
