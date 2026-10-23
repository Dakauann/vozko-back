package emailsending

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
)

func compose(subject string) es.ComposeFunc {
	return func(env es.Envelope) (es.Content, error) {
		return es.Content{Subject: subject, HTML: "<p>oi</p><a href=\"" + env.UnsubscribeURL + "\">sair</a>", Text: "oi"}, nil
	}
}

func marketingRequest() es.SendRequest {
	return es.SendRequest{
		WorkspaceID: wsID,
		To:          "cliente@example.com",
		Kind:        es.KindMarketing,
		RunKind:     es.RunCampaign,
		RunID:       "camp-1",
		RecipientID: "rec-1",
		Tags:        map[string]string{"campaign": "camp-1", es.MessageIDTag: "spoofed"},
	}
}

func readyHarness(t *testing.T) (*harness, *es.SendingDomain) {
	t.Helper()
	h := newHarness(t)
	d := h.provisionVerified(t, wsID)
	return h, d
}

func TestGatewaySendsMarketing(t *testing.T) {
	h, d := readyHarness(t)
	ctx := context.Background()
	msg, err := h.m.Gateway.Send(ctx, marketingRequest(), compose("Promoção"))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != es.MessageSent || msg.ProviderMessageID != "ses-1" || msg.DomainID != d.ID || msg.RunID != "camp-1" || !msg.SentAt.Equal(testNow) {
		t.Fatalf("message = %+v", msg)
	}
	stored, _ := h.messages.Get(ctx, msg.ID)
	if stored.Status != es.MessageSent || stored.ProviderMessageID != "ses-1" {
		t.Fatalf("stored = %+v", stored)
	}
	out := h.provider.sent[0]
	if out.TenantName != "ws-"+wsID || out.From.Address != emailaddress.Address("contato@"+d.Domain) || out.From.Name != wsName {
		t.Fatalf("outbound = %+v", out)
	}
	purpose, id, err := h.codec.OpenAddress(out.ReplyTo[0])
	if err != nil || purpose != emailtoken.PurposeReply || id != msg.ID || out.ReplyTo[0].Domain() != d.ReplyDomain {
		t.Fatalf("reply-to = %v (%v %v)", out.ReplyTo, purpose, err)
	}
	var unsubscribe string
	for _, hdr := range out.Headers {
		if hdr.Name == "List-Unsubscribe" {
			unsubscribe = hdr.Value
		}
	}
	if !strings.HasPrefix(unsubscribe, "<https://api.test/public/email/unsubscribe/") || !strings.Contains(unsubscribe, "mailto:unsubscribe+") {
		t.Fatalf("list-unsubscribe = %q", unsubscribe)
	}
	if !strings.Contains(out.HTML, "https://api.test/public/email/unsubscribe/") {
		t.Fatal("the composer must receive the unsubscribe url")
	}
	if out.Tags[es.MessageIDTag] != msg.ID || out.Tags["workspace"] != wsID || out.Tags["run_kind"] != "campaign" || out.Tags["campaign"] != "camp-1" {
		t.Fatalf("tags = %v", out.Tags)
	}
	if used, _ := h.usage.Used(ctx, d.ID, es.DayKey(testNow)); used != 1 {
		t.Fatalf("used = %d", used)
	}
}

func TestGatewayTransactionalThreading(t *testing.T) {
	h, _ := readyHarness(t)
	req := marketingRequest()
	req.Kind = es.KindTransactional
	req.InReplyTo = "orig@example.com"
	req.References = []string{"<a@x>", "b@x", "bad id"}
	if _, err := h.m.Gateway.Send(context.Background(), req, compose("Pedido")); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{}
	for _, hdr := range h.provider.sent[0].Headers {
		headers[hdr.Name] = hdr.Value
	}
	if headers["In-Reply-To"] != "<orig@example.com>" || headers["References"] != "<a@x> <b@x>" || headers["List-Unsubscribe"] != "" {
		t.Fatalf("headers = %v", headers)
	}
}

func TestGatewayRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*harness, *es.SendingDomain, *es.SendRequest)
		check   func(t *testing.T, err error)
	}{
		{"suppressed recipient", func(h *harness, _ *es.SendingDomain, r *es.SendRequest) {
			_ = h.suppressions.Suppress(context.Background(), es.Suppression{WorkspaceID: wsID, Address: r.To, Reason: es.SuppressComplaint})
		}, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrRecipientSuppressed) {
				t.Fatalf("err = %v", err)
			}
		}},
		{"paused account", func(h *harness, _ *es.SendingDomain, _ *es.SendRequest) {
			acct, _ := h.accounts.GetByWorkspace(context.Background(), wsID)
			acct.Status = es.AccountPausedByProvider
			_ = h.accounts.Update(context.Background(), acct)
		}, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrAccountPaused) {
				t.Fatalf("err = %v", err)
			}
		}},
		{"unverified domain", func(h *harness, d *es.SendingDomain, _ *es.SendRequest) {
			d.Status = es.DomainVerifying
			_ = h.domains.Update(context.Background(), d)
		}, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrDomainNotVerified) {
				t.Fatalf("err = %v", err)
			}
		}},
		{"no default sender", func(h *harness, _ *es.SendingDomain, _ *es.SendRequest) {
			for id := range h.senders.rows {
				delete(h.senders.rows, id)
			}
		}, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrNoDefaultSender) {
				t.Fatalf("err = %v", err)
			}
		}},
		{"unknown sender", func(_ *harness, _ *es.SendingDomain, r *es.SendRequest) { r.SenderID = "nope" }, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrSenderNotFound) {
				t.Fatalf("err = %v", err)
			}
		}},
		{"unnormalized recipient", func(_ *harness, _ *es.SendingDomain, r *es.SendRequest) { r.To = "Cliente@Example.com" }, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrInvalidMessage) {
				t.Fatalf("err = %v", err)
			}
		}},
		{"not provisioned", func(_ *harness, _ *es.SendingDomain, r *es.SendRequest) {
			r.WorkspaceID = "22222222-2222-2222-2222-222222222222"
		}, func(t *testing.T, err error) {
			if !errors.Is(err, es.ErrAccountNotProvisioned) {
				t.Fatalf("err = %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, d := readyHarness(t)
			req := marketingRequest()
			tc.prepare(h, d, &req)
			_, err := h.m.Gateway.Send(context.Background(), req, compose("Oi"))
			tc.check(t, err)
			if len(h.provider.sent) != 0 {
				t.Fatal("nothing may reach the provider")
			}
			if used, _ := h.usage.Used(context.Background(), d.ID, es.DayKey(testNow)); used != 0 {
				t.Fatalf("no slot may stay taken, used = %d", used)
			}
		})
	}
}

func TestGatewayDailyCap(t *testing.T) {
	h, d := readyHarness(t)
	h.usage.used[h.usage.key(d.ID, es.DayKey(testNow))] = es.ProbationDailyCap
	_, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
	var capErr *es.DailyCapError
	if !errors.As(err, &capErr) || capErr.Cap != es.ProbationDailyCap || !capErr.ResumeAt.Equal(es.NextDayStart(testNow)) {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, es.ErrDailyCapReached) || len(h.provider.sent) != 0 {
		t.Fatal("cap refusal must not send")
	}
}

func TestGatewayGivesTheSlotBackOnFailures(t *testing.T) {
	t.Run("provider failure", func(t *testing.T) {
		h, d := readyHarness(t)
		h.provider.sendErr = &es.SendError{Kind: es.SendErrorTransient, Detail: "throttled"}
		_, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
		if kind, ok := es.SendErrorKindOf(err); !ok || kind != es.SendErrorTransient {
			t.Fatalf("err = %v", err)
		}
		if used, _ := h.usage.Used(context.Background(), d.ID, es.DayKey(testNow)); used != 0 {
			t.Fatalf("used = %d", used)
		}
		for _, m := range h.messages.rows {
			if m.Status != es.MessageFailed || m.ProviderMessageID != "" {
				t.Fatalf("message = %+v", m)
			}
		}
	})
	t.Run("compose failure", func(t *testing.T) {
		h, d := readyHarness(t)
		_, err := h.m.Gateway.Send(context.Background(), marketingRequest(), func(es.Envelope) (es.Content, error) { return es.Content{}, errors.New("render") })
		if err == nil || len(h.messages.rows) != 0 {
			t.Fatalf("err = %v rows = %d", err, len(h.messages.rows))
		}
		if used, _ := h.usage.Used(context.Background(), d.ID, es.DayKey(testNow)); used != 0 {
			t.Fatalf("used = %d", used)
		}
	})
	t.Run("invalid content", func(t *testing.T) {
		h, _ := readyHarness(t)
		if _, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi\r\nBcc: x@y.com")); !errors.Is(err, es.ErrInvalidMessage) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGatewayNeverResendsWhenRecordingFailsAfterAccept(t *testing.T) {
	h, _ := readyHarness(t)
	h.messages.updateErr = errors.New("db down")
	msg, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
	if err != nil || msg.ProviderMessageID != "ses-1" {
		t.Fatalf("an accepted send must be reported as sent: %+v %v", msg, err)
	}
	stored, _ := h.messages.Get(context.Background(), msg.ID)
	if stored.Status != es.MessageQueued {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestGatewayRebuildsTheEnvelopeASentMessageWasComposedWith(t *testing.T) {
	for _, kind := range []es.MessageKind{es.KindMarketing, es.KindTransactional} {
		t.Run(string(kind), func(t *testing.T) {
			h, _ := readyHarness(t)
			ctx := context.Background()
			req := marketingRequest()
			req.Kind = kind
			var composed es.Envelope
			msg, err := h.m.Gateway.Send(ctx, req, func(env es.Envelope) (es.Content, error) {
				composed = env
				return compose("Promoção")(env)
			})
			if err != nil {
				t.Fatal(err)
			}
			rebuilt, err := h.m.Gateway.Envelope(ctx, req, *msg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rebuilt, composed) {
				t.Fatalf("rebuilt %+v, the send composed with %+v", rebuilt, composed)
			}
			if (kind == es.KindMarketing) != (rebuilt.UnsubscribeURL != "") {
				t.Fatalf("unsubscribe url = %q for %s", rebuilt.UnsubscribeURL, kind)
			}
		})
	}
}

func TestGatewayRefusesToRebuildAnotherWorkspacesMessage(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	msg, err := h.m.Gateway.Send(ctx, marketingRequest(), compose("Promoção"))
	if err != nil {
		t.Fatal(err)
	}
	other := marketingRequest()
	other.WorkspaceID = "11111111-2222-3333-4444-555555555555"
	if _, err := h.m.Gateway.Envelope(ctx, other, *msg); !errors.Is(err, es.ErrMessageNotFound) {
		t.Fatalf("err = %v, want message not found", err)
	}
}

func TestGatewayRefusesTheSharedListAndDeadDomainsForEveryRunKind(t *testing.T) {
	for _, reason := range []es.SuppressReason{es.SuppressBadAddress, es.SuppressNoMailDomain} {
		for _, kind := range []es.RunKind{es.RunCampaign, es.RunTriggered, es.RunReply, es.RunTest, es.RunAPI} {
			t.Run(string(reason)+"/"+string(kind), func(t *testing.T) {
				h, _ := readyHarness(t)
				req := marketingRequest()
				req.RunKind = kind
				if kind != es.RunCampaign && kind != es.RunTest {
					req.Kind = es.KindTransactional
				}
				_ = h.suppressions.Suppress(context.Background(), es.Suppression{WorkspaceID: wsID, Address: req.To, Reason: reason})
				_, err := h.m.Gateway.Send(context.Background(), req, compose("Oi"))
				if !errors.Is(err, es.ErrRecipientSuppressed) || es.SuppressReasonOf(err) != reason {
					t.Fatalf("err = %v, want a refusal carrying %s", err, reason)
				}
				if len(h.provider.sent) != 0 {
					t.Fatal("nothing reaches the provider")
				}
			})
		}
	}
}

func ownDomainHarness(t *testing.T) (*harness, *es.SendingDomain, string) {
	t.Helper()
	h, _ := readyHarness(t)
	ctx := context.Background()
	d := h.connect(t, wsID, "loja.com.br")
	h.provider.states["loja.com.br"] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	d, err := h.m.Domains.Recheck(ctx, wsID, d.ID)
	if err != nil || d.Status != es.DomainVerified {
		t.Fatalf("own domain = %+v %v", d, err)
	}
	sender, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: d.ID, LocalPart: "vendas", DisplayName: "Loja"})
	if err != nil {
		t.Fatal(err)
	}
	return h, d, sender.Sender.ID
}

func TestGatewayOwnDomainHasNoDailyCap(t *testing.T) {
	h, d, senderID := ownDomainHarness(t)
	day := es.DayKey(testNow)
	h.usage.used[h.usage.key(d.ID, day)] = 1_000_000
	req := marketingRequest()
	req.SenderID = senderID
	msg, err := h.m.Gateway.Send(context.Background(), req, compose("Oi"))
	if err != nil || msg.DomainID != d.ID {
		t.Fatalf("send = %+v %v", msg, err)
	}
	if used, _ := h.usage.Used(context.Background(), d.ID, day); used != 1_000_001 {
		t.Fatalf("used = %d, the uncapped domain still counts its sends", used)
	}
	if len(h.pacer.tickets) != 1 || h.pacer.tickets[0].WorkspaceID != wsID {
		t.Fatalf("pacer tickets = %+v, the own domain must still pass the provider quota pacing", h.pacer.tickets)
	}
}

func TestGatewayOwnDomainStillWaitsForTheProviderQuota(t *testing.T) {
	h, d, senderID := ownDomainHarness(t)
	h.pacer.err = &es.SendError{Kind: es.SendErrorTransient, Detail: "ses pacing"}
	req := marketingRequest()
	req.SenderID = senderID
	_, err := h.m.Gateway.Send(context.Background(), req, compose("Oi"))
	if kind, ok := es.SendErrorKindOf(err); !ok || kind != es.SendErrorTransient || len(h.provider.sent) != 0 {
		t.Fatalf("err = %v sent = %d, the pacer refusal must hold the own domain too", err, len(h.provider.sent))
	}
	if used, _ := h.usage.Used(context.Background(), d.ID, es.DayKey(testNow)); used != 0 {
		t.Fatalf("used = %d, a paced send gives its slot back", used)
	}
}

func TestStatusShowsTheOwnDomainWithoutADailyCap(t *testing.T) {
	h, d, _ := ownDomainHarness(t)
	view, err := h.m.Setup.Status(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	caps := map[es.DomainKind]int64{}
	for _, v := range view.Domains {
		caps[v.Domain.Kind] = v.CapToday
	}
	if caps[es.DomainOwn] != es.Uncapped || caps[es.DomainSharedProbation] != es.ProbationDailyCap {
		t.Fatalf("caps = %v for own domain %s", caps, d.ID)
	}
}

func TestGatewayAsksTheHygieneRuleBeforeAnythingIsSpentOrSent(t *testing.T) {
	h, _ := readyHarness(t)
	h.hygiene.refusal = &emailhygiene.RecipientRefusal{Code: emailhygiene.CodeDisposableAddress}
	req := marketingRequest()
	req.Kind, req.RunKind = es.KindTransactional, es.RunReply

	_, err := h.m.Gateway.Send(context.Background(), req, compose("Oi"))

	if code, ok := emailhygiene.RecipientRefusalCode(err); !ok || code != emailhygiene.CodeDisposableAddress {
		t.Fatalf("err = %v, want the hygiene refusal", err)
	}
	if len(h.provider.sent) != 0 || len(h.usage.used) != 0 || h.usage.givebacks != 0 {
		t.Fatal("a refused recipient reaches neither the provider nor the daily cap")
	}
	if len(h.hygiene.kinds) != 1 || h.hygiene.kinds[0] != es.RunReply {
		t.Fatalf("hygiene asked with %v, want the run kind of the send", h.hygiene.kinds)
	}
}
