package emailsending

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

func apiRequest() es.SendRequest {
	return es.SendRequest{
		WorkspaceID: wsID,
		To:          "cliente@example.com",
		Kind:        es.KindTransactional,
		RunKind:     es.RunAPI,
		RunID:       "send-1",
		RecipientID: "r0",
		ReplyTo:     []emailaddress.Address{"suporte@acme.com"},
		Headers:     []es.Header{{Name: "X-Entity-Ref-Id", Value: "42"}},
	}
}

func transactionalText(es.Envelope) (es.Content, error) {
	return es.Content{Subject: "Código", Text: "123"}, nil
}

func TestGatewayGivesAPISendsTheCallersReplyToAndHeaders(t *testing.T) {
	h, _ := readyHarness(t)
	if _, err := h.m.Gateway.Send(context.Background(), apiRequest(), transactionalText); err != nil {
		t.Fatal(err)
	}
	out := h.provider.sent[0]
	if !reflect.DeepEqual(out.ReplyTo, []emailaddress.Address{"suporte@acme.com"}) {
		t.Fatalf("reply-to = %v, want only the caller's address", out.ReplyTo)
	}
	if !reflect.DeepEqual(out.Headers, []es.Header{{Name: "X-Entity-Ref-Id", Value: "42"}}) {
		t.Fatalf("headers = %v", out.Headers)
	}
	if out.Tags["run_kind"] != "api" {
		t.Fatalf("tags = %v", out.Tags)
	}
}

func TestGatewayLeavesAnAPISendWithoutReplyToWithoutTheInboxAddress(t *testing.T) {
	h, _ := readyHarness(t)
	req := apiRequest()
	req.ReplyTo = nil
	if _, err := h.m.Gateway.Send(context.Background(), req, transactionalText); err != nil {
		t.Fatal(err)
	}
	if got := h.provider.sent[0].ReplyTo; len(got) != 0 {
		t.Fatalf("reply-to = %v, want none so replies reach the From address", got)
	}
}

func TestGatewayIgnoresReplyToAndHeadersOnEveryOtherRunKind(t *testing.T) {
	for _, kind := range []es.RunKind{es.RunTriggered, es.RunReply, es.RunTest, es.RunCampaign} {
		h, _ := readyHarness(t)
		req := apiRequest()
		req.RunKind = kind
		if kind == es.RunCampaign {
			req.Kind = es.KindMarketing
		}
		msg, err := h.m.Gateway.Send(context.Background(), req, transactionalText)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		out := h.provider.sent[0]
		if len(out.ReplyTo) != 1 || out.ReplyTo[0] == "suporte@acme.com" {
			t.Fatalf("%s reply-to = %v, want the tagged inbox address", kind, out.ReplyTo)
		}
		if _, id, err := h.codec.OpenAddress(out.ReplyTo[0]); err != nil || id != msg.ID {
			t.Fatalf("%s reply-to is not the inbox address of the message: %v", kind, err)
		}
		for _, hdr := range out.Headers {
			if hdr.Name == "X-Entity-Ref-Id" {
				t.Fatalf("%s carried a caller header", kind)
			}
		}
	}
}
