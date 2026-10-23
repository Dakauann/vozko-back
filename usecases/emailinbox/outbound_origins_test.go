package emailinbox

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
	balance_usecase "vozko/usecases/balance"
	"vozko/usecases/transactionalemail"
)

type roundBilling struct {
	charges int
	refunds int
}

func (b *roundBilling) ChargeMessageRound(_ context.Context, _, key string) (balance_usecase.EmailMessageCharge, error) {
	b.charges++
	return balance_usecase.EmailMessageCharge{Key: key, Quote: balance_usecase.EmailQuote{Units: 1, UnitMicros: 583, TotalMicros: 583}, Recorded: true}, nil
}

func (b *roundBilling) RefundMessage(context.Context, string, string, balance_usecase.EmailQuote) error {
	b.refunds++
	return nil
}

type sentOutbox struct {
	w     *world
	sends int
	rows  []emailsending.Message
}

func (o *sentOutbox) Send(_ context.Context, req emailsending.SendRequest, compose emailsending.ComposeFunc) (*emailsending.Message, error) {
	o.sends++
	sender, _ := o.w.senders.GetDefault(context.Background(), req.WorkspaceID)
	if _, err := compose(emailsending.Envelope{MessageID: triggeredMsg, Sender: *sender}); err != nil {
		return nil, err
	}
	m := emailsending.Message{ID: triggeredMsg, WorkspaceID: req.WorkspaceID, DomainID: sender.DomainID, ProviderMessageID: "ses-tx-1",
		RunKind: req.RunKind, RunID: req.RunID, Address: req.To, Status: emailsending.MessageSent}
	o.rows = append(o.rows, m)
	o.w.sent.byID[m.ID] = &m
	return &m, nil
}

func (o *sentOutbox) ListByRun(_ context.Context, _ string, _ emailsending.RunKind, runID string) ([]emailsending.Message, error) {
	var out []emailsending.Message
	for _, m := range o.rows {
		if m.RunID == runID {
			out = append(out, m)
		}
	}
	return out, nil
}

type noDesigns struct{}

func (noDesigns) Get(context.Context, string, string) (*emailcampaign.Design, error) {
	return nil, emailcampaign.ErrDesignNotFound
}

type anaFacts struct{}

func (anaFacts) EmailFacts(_ context.Context, _ string, ids []string) (map[string]lead.EmailFacts, error) {
	out := map[string]lead.EmailFacts{}
	for _, id := range ids {
		if id == leadAna {
			out[id] = lead.EmailFacts{ID: leadAna, Name: "Ana Souza", Email: string(anaAddress)}
		}
	}
	return out, nil
}

func TestEveryOneToOneOriginRecordsOneRowThatTheReplyThreadsInto(t *testing.T) {
	cases := []struct {
		name   string
		origin transactionalemail.Origin
		by     conversation.SentBy
	}{
		{"workflow node", transactionalemail.OriginWorkflow, conversation.SentByWorkflow("wf-1")},
		{"agent tool", transactionalemail.OriginAgent, conversation.SentByAI("agent-1")},
		{"assistant tool", transactionalemail.OriginCopilot, conversation.SentByAI("")},
		{"operator outreach", transactionalemail.OriginOutreach, conversation.SentByPerson("8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			outbox := &sentOutbox{w: w}
			billing := &roundBilling{}
			service, err := transactionalemail.New(transactionalemail.Deps{
				Gateway: outbox, Billing: billing, Runs: outbox, Designs: noDesigns{}, LeadFacts: anaFacts{},
				Leads: w.leads, Transcript: w.transcript(),
			})
			if err != nil {
				t.Fatal(err)
			}
			req := transactionalemail.Request{
				WorkspaceID: wsA, LeadID: leadAna, Subject: "Proposta para {{first_name}}",
				Body: transactionalemail.Body{Text: "Oi {{first_name}}, segue a proposta."}, IdempotencyKey: "key-" + tc.name,
				Origin: tc.origin, SentBy: tc.by,
			}
			first, err := service.Send(context.Background(), req)
			if err != nil || first.ConversationID == "" {
				t.Fatalf("send = %+v, %v", first, err)
			}
			again, err := service.Send(context.Background(), req)
			if err != nil || !again.Repeated || again.ConversationID != first.ConversationID || again.MessageID != first.MessageID {
				t.Fatalf("repeat = %+v, %v", again, err)
			}
			if outbox.sends != 1 || billing.charges != 1 || billing.refunds != 0 || len(w.history.records) != 1 {
				t.Fatalf("sends %d charges %d refunds %d records %d", outbox.sends, billing.charges, billing.refunds, len(w.history.records))
			}
			rec := w.history.records[0]
			if rec.SentBy != tc.by || rec.EntryID != first.ConversationID || rec.ProviderMessageID != "ses-tx-1" || rec.Text != "Oi Ana, segue a proposta." {
				t.Fatalf("record = %+v", rec)
			}
			if conv := w.onlyConversation(); conv.Subject != "Proposta para Ana" {
				t.Fatalf("conversation = %+v", conv)
			}

			if err := w.receive(reply(anaAddress, "r1@cliente.com"), w.tagged(emailtoken.PurposeReply, triggeredMsg, replyA)); err != nil {
				t.Fatal(err)
			}
			if w.onlyConversation().ID != first.ConversationID || len(w.history.records) != 2 || w.history.records[1].EntryID != first.ConversationID {
				t.Fatalf("the reply did not thread into %s: %+v", first.ConversationID, w.history.records)
			}
		})
	}
}
