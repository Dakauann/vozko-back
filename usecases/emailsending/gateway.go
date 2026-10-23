package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
)

const UnsubscribePath = "/public/email/unsubscribe/"

type Gateway struct {
	c *core
}

var (
	_ es.Gateway        = (*Gateway)(nil)
	_ es.EnvelopeReader = (*Gateway)(nil)
)

func (g *Gateway) Send(ctx context.Context, req es.SendRequest, compose es.ComposeFunc) (*es.Message, error) {
	c := g.c
	if compose == nil {
		return nil, fmt.Errorf("%w: compose", es.ErrInvalidMessage)
	}
	to, err := emailaddress.Normalize(string(req.To))
	if err != nil || to != req.To {
		return nil, fmt.Errorf("%w: to", es.ErrInvalidMessage)
	}
	if req.Kind != es.KindMarketing && req.Kind != es.KindTransactional {
		return nil, fmt.Errorf("%w: kind", es.ErrInvalidMessage)
	}
	acct, err := c.liveAccount(ctx, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := acct.CanSend(); err != nil {
		return nil, err
	}
	sender, err := g.sender(ctx, req)
	if err != nil {
		return nil, err
	}
	domain, err := c.Domains.Get(ctx, req.WorkspaceID, sender.DomainID)
	if err != nil {
		return nil, err
	}
	if err := sender.CanSendFrom(*domain); err != nil {
		return nil, err
	}
	if err := c.Hygiene.SendRefusal(ctx, req.WorkspaceID, req.RunKind, to); err != nil {
		return nil, err
	}
	suppressed, err := c.Suppressions.Suppressed(ctx, req.WorkspaceID, []emailaddress.Address{to})
	if err != nil {
		return nil, err
	}
	if reason, ok := suppressed[to]; ok {
		return nil, es.SuppressedError{Reason: reason}
	}

	now := c.Now()
	day := es.DayKey(now)
	capValue := es.DailyCap(*domain)
	granted, err := c.Usage.TakeDailySlot(ctx, domain.ID, day, capValue)
	if err != nil {
		return nil, err
	}
	if !granted {
		return nil, &es.DailyCapError{Cap: capValue, ResumeAt: es.NextDayStart(now)}
	}
	giveBack := func() {
		if err := c.Usage.GiveBackDailySlot(ctx, domain.ID, day); err != nil {
			log.Printf("[emailsending] daily slot of domain %s could not be returned (counter stays high): %v", domain.ID, err)
		}
	}

	messageID := c.NewID()
	outbound, err := g.build(req, acct, sender, domain, messageID, compose)
	if err != nil {
		giveBack()
		return nil, err
	}
	if err := c.Pacer.Admit(ctx, es.SendTicket{WorkspaceID: req.WorkspaceID, RunKind: req.RunKind}); err != nil {
		giveBack()
		return nil, err
	}
	message := &es.Message{
		ID:          messageID,
		WorkspaceID: req.WorkspaceID,
		RunKind:     req.RunKind,
		RunID:       req.RunID,
		RecipientID: req.RecipientID,
		DomainID:    domain.ID,
		Address:     to,
		Status:      es.MessageQueued,
	}
	if err := c.Messages.Record(ctx, message); err != nil {
		giveBack()
		return nil, err
	}
	receipt, err := c.Provider.Send(ctx, outbound)
	if err != nil {
		giveBack()
		message.Status = es.MessageFailed
		if updateErr := c.Messages.Update(ctx, message); updateErr != nil {
			log.Printf("[emailsending] message %s failed to send and could not be marked failed: %v", message.ID, updateErr)
		}
		return nil, err
	}
	message.ProviderMessageID = receipt.ProviderMessageID
	message.Status = es.MessageSent
	message.SentAt = now
	if err := c.Messages.Update(ctx, message); err != nil {
		log.Printf("[emailsending] CRITICAL message %s was accepted by the provider as %s but the row could not be updated; provider events will heal it through the %s tag: %v",
			message.ID, receipt.ProviderMessageID, es.MessageIDTag, err)
	}
	return message, nil
}

func (g *Gateway) sender(ctx context.Context, req es.SendRequest) (*es.Sender, error) {
	if strings.TrimSpace(req.SenderID) != "" {
		return g.c.Senders.Get(ctx, req.WorkspaceID, req.SenderID)
	}
	sender, err := g.c.Senders.GetDefault(ctx, req.WorkspaceID)
	if errors.Is(err, es.ErrSenderNotFound) {
		return nil, es.ErrNoDefaultSender
	}
	return sender, err
}

func (g *Gateway) Envelope(ctx context.Context, req es.SendRequest, sent es.Message) (es.Envelope, error) {
	if sent.ID == "" || sent.WorkspaceID != req.WorkspaceID {
		return es.Envelope{}, es.ErrMessageNotFound
	}
	sender, err := g.sender(ctx, req)
	if err != nil {
		return es.Envelope{}, err
	}
	domain, err := g.c.Domains.Get(ctx, req.WorkspaceID, sent.DomainID)
	if err != nil {
		return es.Envelope{}, err
	}
	env, _, err := g.envelope(req.Kind, sender, domain, sent.ID)
	return env, err
}

func (g *Gateway) envelope(kind es.MessageKind, sender *es.Sender, domain *es.SendingDomain, messageID string) (es.Envelope, []es.Header, error) {
	c := g.c
	replyTo, err := c.Codec.TaggedAddress(emailtoken.PurposeReply, messageID, domain.ReplyDomain)
	if err != nil {
		return es.Envelope{}, nil, err
	}
	env := es.Envelope{
		MessageID: messageID,
		From:      sender.Mailbox(*domain),
		ReplyTo:   replyTo,
		Domain:    *domain,
		Sender:    *sender,
	}
	if kind != es.KindMarketing {
		return env, nil, nil
	}
	token, err := c.Codec.Seal(emailtoken.PurposeUnsubscribe, messageID)
	if err != nil {
		return es.Envelope{}, nil, err
	}
	mailto, err := c.Codec.TaggedAddress(emailtoken.PurposeUnsubscribe, messageID, domain.ReplyDomain)
	if err != nil {
		return es.Envelope{}, nil, err
	}
	env.UnsubscribeURL = c.Config.PublicBaseURL + UnsubscribePath + token
	headers, err := es.ListUnsubscribeHeaders(env.UnsubscribeURL, string(mailto))
	if err != nil {
		return es.Envelope{}, nil, err
	}
	return env, headers, nil
}

func (g *Gateway) build(req es.SendRequest, acct *es.SenderAccount, sender *es.Sender, domain *es.SendingDomain, messageID string, compose es.ComposeFunc) (es.OutboundMessage, error) {
	env, headers, err := g.envelope(req.Kind, sender, domain, messageID)
	if err != nil {
		return es.OutboundMessage{}, err
	}
	content, err := compose(env)
	if err != nil {
		return es.OutboundMessage{}, err
	}
	if id := angleID(req.InReplyTo); id != "" {
		headers = append(headers, es.Header{Name: "In-Reply-To", Value: id})
	}
	if refs := references(req.References); refs != "" {
		headers = append(headers, es.Header{Name: "References", Value: refs})
	}
	replyTo := []emailaddress.Address{env.ReplyTo}
	if !req.RunKind.RoutesRepliesToInbox() {
		replyTo = req.ReplyTo
		headers = append(headers, req.Headers...)
	}
	tags := make(map[string]string, len(req.Tags)+4)
	for k, v := range req.Tags {
		tags[k] = v
	}
	tags["workspace"] = req.WorkspaceID
	tags["run_kind"] = string(req.RunKind)
	tags["run_id"] = req.RunID
	tags[es.MessageIDTag] = messageID
	msg := es.OutboundMessage{
		TenantName:  acct.TenantName,
		From:        env.From,
		ReplyTo:     replyTo,
		To:          req.To,
		Subject:     content.Subject,
		HTML:        content.HTML,
		Text:        content.Text,
		Headers:     headers,
		Tags:        tags,
		Kind:        req.Kind,
		Attachments: content.Attachments,
	}
	if err := msg.Validate(); err != nil {
		return es.OutboundMessage{}, err
	}
	return msg, nil
}

func angleID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" || strings.ContainsAny(id, "\r\n \t") {
		return ""
	}
	if !strings.HasPrefix(id, "<") {
		id = "<" + id
	}
	if !strings.HasSuffix(id, ">") {
		id += ">"
	}
	return id
}

func references(ids []string) string {
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		if id := angleID(raw); id != "" {
			out = append(out, id)
		}
	}
	return strings.Join(out, " ")
}
