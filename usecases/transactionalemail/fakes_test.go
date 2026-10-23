package transactionalemail

import (
	"context"
	"errors"
	"sync"

	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	em "vozko/domain/emailinbox"
	es "vozko/domain/emailsending"
	"vozko/domain/lead"
	balance_usecase "vozko/usecases/balance"
)

type event string

type recorder struct {
	events []event
}

func (r *recorder) add(e event) { r.events = append(r.events, e) }

type gatewayFake struct {
	log      *recorder
	err      error
	requests []es.SendRequest
	contents []es.Content
	sent     *es.Message
	envelope es.Envelope
}

func (g *gatewayFake) Send(_ context.Context, req es.SendRequest, compose es.ComposeFunc) (*es.Message, error) {
	g.log.add("send")
	g.requests = append(g.requests, req)
	envelope := g.envelope
	if envelope.MessageID == "" {
		envelope = es.Envelope{MessageID: "env-1"}
	}
	content, err := compose(envelope)
	if err != nil {
		return nil, err
	}
	g.contents = append(g.contents, content)
	if g.err != nil {
		return nil, g.err
	}
	if g.sent != nil {
		return g.sent, nil
	}
	return &es.Message{ID: "msg-1", ProviderMessageID: "prov-1", RunKind: req.RunKind, RunID: req.RunID, Status: es.MessageSent}, nil
}

type billingFake struct {
	log         *recorder
	heldByOther bool
	chargeErr   error
	charged     []string
	refunded    []string
	refundErr   error
	quote       balance_usecase.EmailQuote
}

func (b *billingFake) ChargeMessageRound(_ context.Context, _ string, messageKey string) (balance_usecase.EmailMessageCharge, error) {
	b.log.add("charge")
	if b.chargeErr != nil {
		return balance_usecase.EmailMessageCharge{}, b.chargeErr
	}
	b.charged = append(b.charged, messageKey)
	return balance_usecase.EmailMessageCharge{Key: messageKey + "#round", Quote: b.quote, Recorded: !b.heldByOther}, nil
}

func (b *billingFake) RefundMessage(_ context.Context, _ string, messageKey string, quote balance_usecase.EmailQuote) error {
	b.log.add("refund")
	if quote != b.quote {
		return errors.New("refund at another price")
	}
	b.refunded = append(b.refunded, messageKey)
	return b.refundErr
}

type runsFake struct {
	rows  []es.Message
	later []es.Message
	err   error
	asked []string
}

func (r *runsFake) ListByRun(_ context.Context, workspaceID string, kind es.RunKind, runID string) ([]es.Message, error) {
	r.asked = append(r.asked, workspaceID+"|"+string(kind)+"|"+runID)
	if len(r.asked) > 1 && r.later != nil {
		return r.later, r.err
	}
	return r.rows, r.err
}

type designsFake struct {
	designs map[string]emaildesign.Document
}

func (d designsFake) Get(_ context.Context, workspaceID, id string) (*emailcampaign.Design, error) {
	doc, ok := d.designs[workspaceID+"/"+id]
	if !ok {
		return nil, emailcampaign.ErrDesignNotFound
	}
	return &emailcampaign.Design{ID: id, WorkspaceID: workspaceID, Document: doc}, nil
}

type factsFake struct {
	facts map[string]lead.EmailFacts
	err   error
}

func (f factsFake) EmailFacts(_ context.Context, _ string, leadIDs []string) (map[string]lead.EmailFacts, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]lead.EmailFacts{}
	for _, id := range leadIDs {
		if found, ok := f.facts[id]; ok {
			out[id] = found
		}
	}
	return out, nil
}

type leadsFake struct {
	byEmail map[string]string
	err     error
}

func (l leadsFake) FindByEmail(_ string, email string) (*lead.Lead, error) {
	if l.err != nil {
		return nil, l.err
	}
	id, ok := l.byEmail[email]
	if !ok {
		return nil, lead.ErrLeadNotFound
	}
	return &lead.Lead{ID: id, Email: email}, nil
}

type transcriptFake struct {
	mu     sync.Mutex
	log    *recorder
	err    error
	emails []em.OutboundEmail
}

func (f *transcriptFake) Transcribe(_ context.Context, email em.OutboundEmail) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.log != nil {
		f.log.add("transcribe")
	}
	f.emails = append(f.emails, email)
	if f.err != nil {
		return "", f.err
	}
	return "conv-of-" + email.ExternalID, nil
}
