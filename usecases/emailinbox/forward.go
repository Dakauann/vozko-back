package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	conv_event "vozko/domain/conversation_event"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
)

const (
	ForwardSkippedBalance = "balance_insufficient"
	ForwardSkippedSend    = "send_failed"
	forwardSubjectPrefix  = "Fwd: "
)

func forwardKey(conversationID, externalID string) string {
	return "email_forward:" + conversationID + ":" + externalID
}

type ForwardInput struct {
	Conversation *em.Conversation
	Contact      *em.Contact
	Message      em.InboundMessage
	ExternalID   string
	ReceivedAt   time.Time
}

type Forwarder struct {
	senders Senders
	billing MessageBilling
	gateway Gateway
	events  conv_event.Logger
}

func NewForwarder(senders Senders, billing MessageBilling, gateway Gateway, events conv_event.Logger) *Forwarder {
	if senders == nil || billing == nil || gateway == nil || events == nil {
		panic("emailinbox: the reply forwarder is missing a dependency")
	}
	return &Forwarder{senders: senders, billing: billing, gateway: gateway, events: events}
}

func (f *Forwarder) Forward(ctx context.Context, in ForwardInput) {
	conv := in.Conversation
	sender, err := f.senders.Get(ctx, conv.WorkspaceID, conv.SenderID)
	if err != nil {
		if !errors.Is(err, emailsending.ErrSenderNotFound) {
			log.Printf("%s forward of %s skipped, sender %s unreadable: %v", logPrefix, in.ExternalID, conv.SenderID, err)
		}
		return
	}
	if sender.ReplyForwardTo == "" {
		return
	}
	key := forwardKey(conv.ID, in.ExternalID)
	charge, err := f.billing.ChargeMessage(ctx, conv.WorkspaceID, key)
	if err != nil {
		reason := ForwardSkippedSend
		if errors.Is(err, balance.ErrInsufficientBalance) {
			reason = ForwardSkippedBalance
		}
		log.Printf("%s forward of %s to the sender mailbox skipped (%s): %v", logPrefix, in.ExternalID, reason, err)
		f.skipped(conv, reason)
		return
	}
	_, err = f.gateway.Send(ctx, emailsending.SendRequest{
		WorkspaceID: conv.WorkspaceID,
		SenderID:    conv.SenderID,
		To:          sender.ReplyForwardTo,
		Kind:        emailsending.KindTransactional,
		RunKind:     emailsending.RunReply,
		RunID:       conv.ID,
		InReplyTo:   in.Message.MessageID,
	}, func(emailsending.Envelope) (emailsending.Content, error) {
		text := forwardText(in)
		return emailsending.Content{Subject: forwardSubject(in.Message.Subject), Text: text, HTML: em.ReplyHTML(text)}, nil
	})
	if err != nil {
		if refundErr := f.billing.RefundMessage(ctx, conv.WorkspaceID, key, charge); refundErr != nil {
			log.Printf("%s CRITICAL forward of %s failed and its charge was not refunded: %v", logPrefix, in.ExternalID, refundErr)
		}
		log.Printf("%s forward of %s to the sender mailbox failed: %v", logPrefix, in.ExternalID, err)
		f.skipped(conv, ForwardSkippedSend)
	}
}

func (f *Forwarder) skipped(conv *em.Conversation, reason string) {
	f.events.Log(conv_event.New(conv.WorkspaceID, conv.ID, string(shared.EntryTypeEmail), conv_event.EventEmailForwardSkipped).
		WithChannel(string(shared.EntryTypeEmail)).
		WithActorSystem().
		WithDetails(map[string]string{"reason": reason}).
		Build())
}

func forwardSubject(subject string) string {
	normalized := em.NormalizeSubject(subject)
	if normalized == "" {
		return strings.TrimSpace(forwardSubjectPrefix)
	}
	return forwardSubjectPrefix + normalized
}

func forwardText(in ForwardInput) string {
	var b strings.Builder
	from := string(in.Contact.Address)
	if name := strings.TrimSpace(in.Message.From.Name); name != "" {
		from = name + " <" + from + ">"
	}
	fmt.Fprintf(&b, "De: %s\n", from)
	if subject := strings.TrimSpace(in.Message.Subject); subject != "" {
		fmt.Fprintf(&b, "Assunto: %s\n", subject)
	}
	fmt.Fprintf(&b, "Data: %s\n\n", in.ReceivedAt.UTC().Format("02/01/2006 15:04 UTC"))
	body, _ := em.TruncateText(in.Message.Text, em.MaxTranscriptTextBytes)
	b.WriteString(body)
	if quoted, _ := em.TruncateText(in.Message.QuotedText, em.MaxTranscriptTextBytes); quoted != "" {
		b.WriteString("\n\n")
		b.WriteString(quoted)
	}
	return b.String()
}
