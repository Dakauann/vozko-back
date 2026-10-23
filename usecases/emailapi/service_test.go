package emailapi_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/emailapi"
	"vozko/domain/emaildesign"
	es "vozko/domain/emailsending"
	"vozko/domain/media"
	balance_usecase "vozko/usecases/balance"
	tx "vozko/usecases/transactionalemail"
)

func TestSendChargesOnceForEveryRecipientAndSendsEachOnItsOwn(t *testing.T) {
	h := newHarness()
	req := request("ana@example.com")
	req.Cc = []string{"bia@example.com"}
	req.Bcc = []string{"caio@example.com"}
	req.ReplyTo = []string{"suporte@acme.com"}
	req.Headers = map[string]string{"X-Entity-Ref-Id": "42"}
	res, err := h.service(t).Send(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Repeated || len(res.Messages) != 3 || len(h.gateway.requests) != 3 {
		t.Fatalf("result = %+v, gateway %d", res, len(h.gateway.requests))
	}
	if len(h.billing.charges) != 1 || h.billing.charges[emailapi.RoundReference(res.Send.ID, 1)].Units != 3 || len(h.billing.refunds) != 0 {
		t.Fatalf("charges = %+v refunds %+v, want one debit of three", h.billing.charges, h.billing.refunds)
	}
	for i, r := range h.gateway.requests {
		if r.RunKind != es.RunAPI || r.RunID != res.Send.ID || r.RecipientID != emailapi.RecipientID(i) || r.SenderID != senderID || r.Kind != es.KindTransactional {
			t.Fatalf("request %d = %+v", i, r)
		}
		if len(r.ReplyTo) != 1 || r.ReplyTo[0] != "suporte@acme.com" || len(r.Headers) != 1 {
			t.Fatalf("request %d reply-to %v headers %v", i, r.ReplyTo, r.Headers)
		}
	}
	_, stored := h.sends.only(t)
	for _, m := range stored {
		if !m.WentOut() || m.Round != 1 {
			t.Fatalf("stored = %+v", stored)
		}
	}
	if res.Send.From != "no-reply@acme.com" || res.Send.APIKeyID != "key-1" {
		t.Fatalf("send = %+v", res.Send)
	}
}

func TestTheSameKeyReturnsTheSameResultWithoutSendingOrChargingAgain(t *testing.T) {
	h := newHarness()
	svc := h.service(t)
	first, err := svc.Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Send(context.Background(), request("bia@example.com", "ana@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Repeated || again.Send.ID != first.Send.ID || len(h.gateway.requests) != 2 || len(h.billing.charges) != 1 {
		t.Fatalf("repeat = %+v, sends %d charges %d", again, len(h.gateway.requests), len(h.billing.charges))
	}
	other := request("ana@example.com")
	other.Content.Subject = "Outro"
	if _, err := svc.Send(context.Background(), other); tx.CodeOf(err) != CodeIdempotencyKeyReused {
		t.Fatalf("other request with the same key = %v", err)
	}
}

func TestAHeldLeaseAnswersInProgress(t *testing.T) {
	h := newHarness()
	svc := h.service(t)
	if _, err := svc.Send(context.Background(), request("ana@example.com")); err != nil {
		t.Fatal(err)
	}
	send, _ := h.sends.only(t)
	until := testNow.Add(time.Minute)
	h.sends.sends[send.ID].LeaseUntil = &until
	if _, err := svc.Send(context.Background(), request("ana@example.com")); tx.CodeOf(err) != tx.CodeSendInProgress || !tx.Retryable(err) {
		t.Fatalf("held lease = %v", err)
	}
}

func TestSuppressedRecipientsAreRefusedBeforeAnyCharge(t *testing.T) {
	h := newHarness()
	h.suppressions["bia@example.com"] = es.SuppressHardBounce
	res, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Messages[1].Status != emailapi.StatusRefused || res.Messages[1].ErrorCode != tx.CodeRecipientSuppressed {
		t.Fatalf("messages = %+v", res.Messages)
	}
	if q := h.billing.charges[emailapi.RoundReference(res.Send.ID, 1)]; q.Units != 1 || len(h.gateway.requests) != 1 {
		t.Fatalf("charged %+v, sent %d, want only the deliverable recipient", q, len(h.gateway.requests))
	}
	h2 := newHarness()
	h2.suppressions["ana@example.com"] = es.SuppressComplaint
	if _, err := h2.service(t).Send(context.Background(), request("ana@example.com")); tx.CodeOf(err) != tx.CodeRecipientSuppressed || len(h2.billing.charges) != 0 {
		t.Fatalf("all suppressed = %v, charges %d", err, len(h2.billing.charges))
	}
}

func TestAShortWalletSendsNothingAndSaysWhatIsMissing(t *testing.T) {
	h := newHarness()
	h.billing.wallet = 1000
	_, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com"))
	var short *balance_usecase.InsufficientEmailBalanceError
	if tx.CodeOf(err) != tx.CodeBalanceInsufficient || !errors.As(err, &short) || short.MissingMicros != 2*583-1000 || !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("err = %v", err)
	}
	send, stored := h.sends.only(t)
	if len(h.gateway.requests) != 0 || send.RoundOpen() || stored[0].ErrorCode != tx.CodeBalanceInsufficient {
		t.Fatalf("send %+v stored %+v", send, stored)
	}
	h.billing.wallet = 1_000_000
	res, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if err != nil || len(h.gateway.requests) != 2 || !res.Repeated {
		t.Fatalf("after the top up = %+v %v", res, err)
	}
	if _, ok := h.billing.charges[emailapi.RoundReference(res.Send.ID, 2)]; !ok {
		t.Fatalf("the retry is a new round: %+v", h.billing.charges)
	}
}

func TestAThrottleOnTheFirstRecipientRefundsAndAnswersRetryAfter(t *testing.T) {
	h := newHarness()
	h.gateway.refuse["ana@example.com"] = &es.SendError{Kind: es.SendErrorTransient, Detail: "rate", RetryAfter: 1500 * time.Millisecond}
	_, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if tx.CodeOf(err) != tx.CodeSendThrottled {
		t.Fatalf("err = %v", err)
	}
	if seconds, ok := tx.RetryAfterSeconds(err); !ok || seconds != 2 {
		t.Fatalf("retry after = %d %v", seconds, ok)
	}
	send, stored := h.sends.only(t)
	ref := emailapi.RoundReference(send.ID, 1)
	if h.billing.refunds[ref] != 2 || send.RoundOpen() || stored[1].ErrorCode != tx.CodeSendThrottled || len(h.gateway.requests) != 0 {
		t.Fatalf("refunds %+v send %+v stored %+v", h.billing.refunds, send, stored)
	}
}

func TestAThrottleMidwayKeepsWhatWentOutAndRefundsTheRest(t *testing.T) {
	h := newHarness()
	h.gateway.refuse["bia@example.com"] = &es.SendError{Kind: es.SendErrorTransient, Detail: "rate", RetryAfter: 3 * time.Second}
	res, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com", "caio@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if res.RetryAfterSeconds != 3 || !res.Messages[0].WentOut() || res.Messages[1].ErrorCode != tx.CodeSendThrottled || res.Messages[2].ErrorCode != tx.CodeSendThrottled {
		t.Fatalf("result = %+v", res)
	}
	if h.billing.refunds[emailapi.RoundReference(res.Send.ID, 1)] != 2 {
		t.Fatalf("refunds = %+v", h.billing.refunds)
	}
	delete(h.gateway.refuse, "bia@example.com")
	again, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com", "caio@example.com"))
	if err != nil || len(h.gateway.requests) != 3 || h.billing.charges[emailapi.RoundReference(res.Send.ID, 2)].Units != 2 {
		t.Fatalf("resume = %+v %v, sends %d", again, err, len(h.gateway.requests))
	}
}

func TestASuppressionFoundByTheGatewayRefusesOnlyThatRecipient(t *testing.T) {
	h := newHarness()
	h.gateway.refuse["ana@example.com"] = es.ErrRecipientSuppressed
	res, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if err != nil || res.Messages[0].ErrorCode != tx.CodeRecipientSuppressed || !res.Messages[1].WentOut() {
		t.Fatalf("result = %+v %v", res, err)
	}
}

func TestARequestScopedRefusalStopsTheRound(t *testing.T) {
	h := newHarness()
	h.gateway.refuse["ana@example.com"] = es.ErrAccountPaused
	_, err := h.service(t).Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if tx.CodeOf(err) != tx.CodeAccountPaused || len(h.gateway.requests) != 0 {
		t.Fatalf("err = %v sends %d", err, len(h.gateway.requests))
	}
	send, _ := h.sends.only(t)
	if h.billing.refunds[emailapi.RoundReference(send.ID, 1)] != 2 {
		t.Fatalf("refunds = %+v", h.billing.refunds)
	}
}

func TestACrashBetweenChargeAndSettlementIsSettledByTheNextCall(t *testing.T) {
	h := newHarness()
	svc := h.service(t)
	if _, err := svc.Send(context.Background(), request("ana@example.com", "bia@example.com")); err != nil {
		t.Fatal(err)
	}
	send, _ := h.sends.only(t)
	s := h.sends.sends[send.ID]
	s.Round, s.RoundUnits, s.RoundUnitMicros, s.RoundSettled = 2, 2, 583, false
	h.billing.charges[emailapi.RoundReference(send.ID, 2)] = quote{Units: 2, UnitMicros: 583, TotalMicros: 1166}
	res, err := svc.Send(context.Background(), request("ana@example.com", "bia@example.com"))
	if err != nil || !res.Repeated {
		t.Fatal(err)
	}
	if h.billing.refunds[emailapi.RoundReference(send.ID, 2)] != 2 || h.sends.sends[send.ID].RoundOpen() {
		t.Fatalf("refunds = %+v", h.billing.refunds)
	}
}

func TestAnOpenRoundThatWasNeverChargedIsClosedWithoutARefund(t *testing.T) {
	h := newHarness()
	h.gateway.refuse["ana@example.com"] = es.ErrAccountPaused
	svc := h.service(t)
	_, _ = svc.Send(context.Background(), request("ana@example.com"))
	send, _ := h.sends.only(t)
	s := h.sends.sends[send.ID]
	s.Round, s.RoundUnits, s.RoundUnitMicros, s.RoundSettled = 2, 1, 583, false
	delete(h.gateway.refuse, "ana@example.com")
	if _, err := svc.Send(context.Background(), request("ana@example.com")); err != nil {
		t.Fatal(err)
	}
	if _, refunded := h.billing.refunds[emailapi.RoundReference(send.ID, 2)]; refunded {
		t.Fatal("a round that was never charged must not be refunded")
	}
	if _, ok := h.billing.charges[emailapi.RoundReference(send.ID, 3)]; !ok {
		t.Fatalf("charges = %+v", h.billing.charges)
	}
}

func TestAMessageThatWentOutButWasNotRecordedIsNeverSentTwice(t *testing.T) {
	h := newHarness()
	h.sends.failMark = errors.New("db blip")
	svc := h.service(t)
	if _, err := svc.Send(context.Background(), request("ana@example.com")); err != nil {
		t.Fatal(err)
	}
	h.sends.failMark = nil
	res, err := svc.Send(context.Background(), request("ana@example.com"))
	if err != nil || len(h.gateway.requests) != 1 || !res.Messages[0].WentOut() {
		t.Fatalf("repeat = %+v %v, sends %d", res, err, len(h.gateway.requests))
	}
	if !strings.Contains(joinLogs(h), "CRITICAL") {
		t.Fatal("the missing log row must be reported")
	}
}

func TestSenderResolution(t *testing.T) {
	h := newHarness()
	byID := request("ana@example.com")
	byID.From = senderID
	if _, err := h.service(t).Send(context.Background(), byID); err != nil {
		t.Fatalf("by id = %v", err)
	}
	for name, from := range map[string]string{"unknown address": "other@acme.com", "unknown id": "1b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b", "not an address": "acme"} {
		r := request("ana@example.com")
		r.From, r.IdempotencyKey = from, name
		if _, err := newHarness().service(t).Send(context.Background(), r); tx.CodeOf(err) != CodeSenderUnknown {
			t.Fatalf("%s = %v", name, err)
		}
	}
	h2 := newHarness()
	h2.domains.domains[0].Status = es.DomainPending
	if _, err := h2.service(t).Send(context.Background(), request("ana@example.com")); tx.CodeOf(err) != tx.CodeSenderNotVerified || len(h2.billing.charges) != 0 {
		t.Fatalf("unverified = %v", err)
	}
}

func TestValidationRefusalsAreCodedAndChargeNothing(t *testing.T) {
	cases := map[string]struct {
		mutate func(*emailapi.Request)
		code   string
	}{
		"no key":      {func(r *emailapi.Request) { r.IdempotencyKey = "" }, tx.CodeIdempotencyKeyRequired},
		"no from":     {func(r *emailapi.Request) { r.From = "" }, CodeFromRequired},
		"no to":       {func(r *emailapi.Request) { r.To = nil }, CodeRecipientsInvalid},
		"bad address": {func(r *emailapi.Request) { r.To = []string{"x"} }, tx.CodeAddressInvalid},
		"bad header":  {func(r *emailapi.Request) { r.Headers = map[string]string{"From": "x"} }, CodeHeaderNotAllowed},
		"bad tag":     {func(r *emailapi.Request) { r.Tags = []emailapi.Tag{{Name: "a b", Value: "c"}} }, CodeTagsInvalid},
		"bad reply":   {func(r *emailapi.Request) { r.ReplyTo = []string{"x"} }, CodeReplyToInvalid},
		"no body":     {func(r *emailapi.Request) { r.Content.HTML, r.Content.Text = "", "" }, tx.CodeMessageInvalid},
		"no design": {func(r *emailapi.Request) {
			r.Content.HTML, r.Content.Text, r.Content.DesignID = "", "", "missing"
		}, tx.CodeDesignNotFound},
		"blocked file": {func(r *emailapi.Request) {
			r.Attachments = []emailapi.AttachmentSource{{FileName: "setup.exe", Data: []byte("MZ")}}
		}, CodeAttachmentBlocked},
		"huge file": {func(r *emailapi.Request) {
			r.Attachments = []emailapi.AttachmentSource{{FileName: "a.bin", ContentType: "application/pdf", Data: make([]byte, es.MaxAttachmentBytes+1)}}
		}, CodeAttachmentTooLarge},
		"unreachable file": {func(r *emailapi.Request) {
			r.Attachments = []emailapi.AttachmentSource{{FileName: "a.pdf", URL: "https://cdn.example.com/missing.pdf"}}
		}, CodeAttachmentUnavailable},
	}
	for name, c := range cases {
		h := newHarness()
		r := request("ana@example.com")
		c.mutate(&r)
		_, err := h.service(t).Send(context.Background(), r)
		var refused *tx.Refusal
		if !errors.As(err, &refused) || refused.Code != c.code {
			t.Fatalf("%s: err = %v, want %s", name, err, c.code)
		}
		if len(h.billing.charges) != 0 || len(h.sends.sends) != 0 {
			t.Fatalf("%s: a refused request recorded or charged something", name)
		}
	}
}

func TestAttachmentsAreChargedByChunkAndCarriedToEveryRecipient(t *testing.T) {
	h := newHarness()
	h.files.files["https://cdn.example.com/nota.pdf"] = media.RemoteFile{Data: []byte("%PDF-1.4"), ContentType: "application/pdf"}
	r := request("ana@example.com", "bia@example.com")
	r.Attachments = []emailapi.AttachmentSource{{FileName: "nota", URL: "https://cdn.example.com/nota.pdf"}, {FileName: "a.txt", Data: []byte("oi")}}
	res, err := h.service(t).Send(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	q := h.billing.charges[emailapi.RoundReference(res.Send.ID, 1)]
	if q.UnitMicros != 583+102 || q.Units != 2 {
		t.Fatalf("quote = %+v", q)
	}
	for _, c := range h.gateway.contents {
		if len(c.Attachments) != 2 || c.Attachments[0].FileName != "nota.pdf" || c.Attachments[0].ContentType != "application/pdf" || c.Attachments[1].ContentType != "text/plain; charset=utf-8" {
			t.Fatalf("attachments = %+v", c.Attachments)
		}
	}
}

func TestDesignsRenderPerRecipientWithTheVariables(t *testing.T) {
	h := newHarness()
	h.designs[wsID+"/d-1"] = emaildesign.Document{Version: emaildesign.CurrentVersion, Blocks: []emaildesign.Block{
		{ID: "h1", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Código {{field.code}} para {{email}}", Level: 1}},
	}}
	r := request("ana@example.com", "bia@example.com")
	r.Content = emailapi.Content{Subject: "Código {{field.code}}", DesignID: "d-1", Variables: map[string]string{"code": "991"}}
	if _, err := h.service(t).Send(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if c := h.gateway.contents[1]; c.Subject != "Código 991" || !strings.Contains(c.HTML, "Código 991 para bia@example.com") {
		t.Fatalf("content = %+v", c)
	}
}
