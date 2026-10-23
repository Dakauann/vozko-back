package emailapi_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	"vozko/domain/emailapi"
	"vozko/domain/emaildesign"
	es "vozko/domain/emailsending"
	"vozko/domain/lead"
	"vozko/domain/media"
	tx "vozko/usecases/transactionalemail"
)

var ErrIncomplete = errors.New("emailapi: a required dependency is missing")

type Deps struct {
	Sends        emailapi.SendStore
	Log          emailapi.LogReader
	Runs         es.RunMessages
	Gateway      es.Gateway
	Billing      Billing
	Designs      tx.Designs
	Senders      Senders
	Domains      Domains
	Suppressions Suppressions
	Files        media.RemoteFiles
	Now          func() time.Time
	NewID        func() string
	Logf         func(format string, args ...any)
}

type Service struct {
	d Deps
}

func New(d Deps) (*Service, error) {
	if d.Sends == nil || d.Log == nil || d.Runs == nil || d.Gateway == nil || d.Billing == nil || d.Designs == nil ||
		d.Senders == nil || d.Domains == nil || d.Suppressions == nil || d.Files == nil {
		return nil, ErrIncomplete
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	if d.Logf == nil {
		d.Logf = log.Printf
	}
	return &Service{d: d}, nil
}

type Result struct {
	Send              emailapi.Send
	Messages          []emailapi.Message
	Repeated          bool
	RetryAfterSeconds int64
}

type composeFunc func(to emailaddress.Address) (es.Content, error)

type prepared struct {
	draft       emailapi.Draft
	sender      es.Sender
	from        emailaddress.Address
	attachments []es.Attachment
	compose     composeFunc
}

func (s *Service) Send(ctx context.Context, req emailapi.Request) (Result, error) {
	p, err := s.prepare(ctx, req)
	if err != nil {
		return Result{}, refusal(err)
	}
	now := s.d.Now()
	lease := now.Add(emailapi.LeaseDuration)
	send := emailapi.Send{
		ID: s.d.NewID(), WorkspaceID: p.draft.WorkspaceID, IdempotencyKey: p.draft.IdempotencyKey, Fingerprint: p.draft.Fingerprint(),
		Origin: p.draft.Origin, APIKeyID: p.draft.APIKeyID, SenderID: p.sender.ID, From: p.from, Subject: p.draft.Content.Subject,
		Tags: p.draft.TagMap(), Recipients: len(p.draft.Recipients), LeaseUntil: &lease, CreatedAt: now,
	}
	messages := make([]emailapi.Message, len(p.draft.Recipients))
	for i, r := range p.draft.Recipients {
		messages[i] = emailapi.Message{ID: s.d.NewID(), SendID: send.ID, WorkspaceID: send.WorkspaceID, Position: r.Position, Role: r.Role,
			Address: r.Address, Status: emailapi.StatusPending, CreatedAt: now}
	}
	claim, err := s.d.Sends.Claim(ctx, send, messages, p.draft.Tags)
	if err != nil {
		return Result{}, fmt.Errorf("emailapi: recording the send: %w", err)
	}
	if !claim.Created {
		if claim.Send.Fingerprint != send.Fingerprint {
			return Result{}, refusal(emailapi.ErrIdempotencyKeyReused)
		}
		taken, err := s.d.Sends.TakeLease(ctx, send.WorkspaceID, claim.Send.ID, now, lease)
		if err != nil {
			return Result{}, fmt.Errorf("emailapi: taking the send: %w", err)
		}
		if !taken {
			return Result{}, refusal(emailapi.ErrSendInProgress)
		}
		send = claim.Send
		if messages, err = s.d.Sends.Messages(ctx, send.ID); err != nil {
			s.release(ctx, send.ID)
			return Result{}, fmt.Errorf("emailapi: reading the send: %w", err)
		}
	}
	defer s.release(ctx, send.ID)
	return s.process(ctx, send, messages, p, !claim.Created)
}

func (s *Service) release(ctx context.Context, sendID string) {
	if err := s.d.Sends.ReleaseLease(context.WithoutCancel(ctx), sendID); err != nil {
		s.d.Logf("[email-api] the lease of send %s was not released and expires on its own: %v", sendID, err)
	}
}

func (s *Service) prepare(ctx context.Context, req emailapi.Request) (prepared, error) {
	draft, err := req.Validate()
	if err != nil {
		return prepared{}, err
	}
	sender, from, err := s.sender(ctx, draft.WorkspaceID, draft.From)
	if err != nil {
		return prepared{}, err
	}
	attachments, err := s.attachments(ctx, draft.Attachments)
	if err != nil {
		return prepared{}, err
	}
	compose, err := s.composer(ctx, draft, attachments)
	if err != nil {
		return prepared{}, err
	}
	if _, err := compose(draft.Recipients[0].Address); err != nil {
		return prepared{}, err
	}
	return prepared{draft: draft, sender: sender, from: from, attachments: attachments, compose: compose}, nil
}

func (s *Service) sender(ctx context.Context, workspaceID, from string) (es.Sender, emailaddress.Address, error) {
	domains, err := s.d.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return es.Sender{}, "", fmt.Errorf("emailapi: reading the sending domains: %w", err)
	}
	byID := make(map[string]es.SendingDomain, len(domains))
	for _, d := range domains {
		byID[d.ID] = d
	}
	pick := func(sender es.Sender) (es.Sender, emailaddress.Address, error) {
		domain, ok := byID[sender.DomainID]
		if !ok {
			return es.Sender{}, "", emailapi.ErrSenderUnknown
		}
		if !domain.Sendable() {
			return es.Sender{}, "", es.ErrDomainNotVerified
		}
		return sender, sender.FromAddress(domain), nil
	}
	if _, err := uuid.Parse(from); err == nil {
		sender, err := s.d.Senders.Get(ctx, workspaceID, from)
		if errors.Is(err, es.ErrSenderNotFound) {
			return es.Sender{}, "", emailapi.ErrSenderUnknown
		}
		if err != nil {
			return es.Sender{}, "", fmt.Errorf("emailapi: reading the sender: %w", err)
		}
		return pick(*sender)
	}
	address, err := emailaddress.Normalize(from)
	if err != nil {
		return es.Sender{}, "", emailapi.ErrSenderUnknown
	}
	senders, err := s.d.Senders.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return es.Sender{}, "", fmt.Errorf("emailapi: reading the senders: %w", err)
	}
	for _, sender := range senders {
		if domain, ok := byID[sender.DomainID]; ok && sender.FromAddress(domain) == address {
			return pick(sender)
		}
	}
	return es.Sender{}, "", emailapi.ErrSenderUnknown
}

func (s *Service) attachments(ctx context.Context, sources []emailapi.AttachmentSource) ([]es.Attachment, error) {
	out := make([]es.Attachment, 0, len(sources))
	var budget int64
	for _, src := range sources {
		data, contentType := src.Data, strings.TrimSpace(src.ContentType)
		if strings.TrimSpace(src.URL) != "" {
			file, err := media.Download(ctx, s.d.Files, strings.TrimSpace(src.URL), es.MaxAttachmentBytes-budget)
			if err != nil {
				if errors.Is(err, media.ErrMediaTooLarge) {
					return nil, err
				}
				return nil, tx.Refuse(CodeAttachmentUnavailable, err)
			}
			data = file.Data
			if contentType == "" {
				contentType = file.ContentType
			}
		}
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		budget += int64(len(data))
		out = append(out, es.Attachment{FileName: es.AttachmentName(src.FileName, contentType), ContentType: contentType, Data: data})
	}
	if err := es.CheckAttachments(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) composer(ctx context.Context, draft emailapi.Draft, attachments []es.Attachment) (composeFunc, error) {
	c := draft.Content
	if !c.UsesDesign() {
		return func(emailaddress.Address) (es.Content, error) {
			return es.Content{Subject: c.Subject, HTML: c.HTML, Text: c.Text, Attachments: attachments}, nil
		}, nil
	}
	doc, err := tx.DesignDocument(ctx, s.d.Designs, draft.WorkspaceID, c.DesignID)
	if err != nil {
		return nil, err
	}
	return func(to emailaddress.Address) (es.Content, error) {
		p := emaildesign.LeadPersonalization(lead.EmailFacts{}, string(to), c.Variables)
		subject, err := emaildesign.RenderSubject(c.Subject, p)
		if err != nil {
			return es.Content{}, tx.Refuse(tx.CodeSubjectInvalid, err)
		}
		content, err := tx.RenderDocument(doc, c.Subject, subject, p)
		if err != nil {
			return es.Content{}, err
		}
		content.Attachments = attachments
		return content, nil
	}, nil
}

func (s *Service) process(ctx context.Context, send emailapi.Send, messages []emailapi.Message, p prepared, repeated bool) (Result, error) {
	if repeated {
		if err := s.reconcile(ctx, send, messages); err != nil {
			return Result{}, err
		}
	}
	if send.RoundOpen() {
		if err := s.settle(ctx, &send, messages); err != nil {
			return Result{}, err
		}
	}
	result := Result{Send: send, Messages: messages, Repeated: repeated}
	outstanding := emailapi.Outstanding(messages)
	if len(outstanding) == 0 {
		return result, nil
	}
	toSend, firstErr, err := s.dropSuppressed(ctx, send, messages, outstanding)
	if err != nil {
		return Result{}, err
	}
	if len(toSend) > 0 {
		retryAfter, sendErr, err := s.round(ctx, &send, messages, toSend, p)
		if err != nil {
			return Result{}, err
		}
		if firstErr == nil {
			firstErr = sendErr
		}
		result.RetryAfterSeconds = retryAfter
	}
	result.Send = send
	for _, m := range messages {
		if m.WentOut() {
			return result, nil
		}
	}
	if firstErr == nil {
		firstErr = refusal(emailapi.ErrSendInProgress)
	}
	return Result{}, firstErr
}

func (s *Service) reconcile(ctx context.Context, send emailapi.Send, messages []emailapi.Message) error {
	sent, err := s.d.Runs.ListByRun(ctx, send.WorkspaceID, es.RunAPI, send.ID)
	if err != nil {
		return fmt.Errorf("emailapi: reading earlier attempts: %w", err)
	}
	byRecipient := make(map[string]es.Message, len(sent))
	for _, m := range sent {
		if m.Status != es.MessageFailed {
			byRecipient[m.RecipientID] = m
		}
	}
	for i, m := range messages {
		found, ok := byRecipient[emailapi.RecipientID(m.Position)]
		if !ok || m.MessageID == found.ID {
			continue
		}
		status := emailapi.StatusOf(found.Status)
		if err := s.d.Sends.MarkSent(ctx, send.ID, m.Position, found.ID, status); err != nil {
			return fmt.Errorf("emailapi: recording an earlier send: %w", err)
		}
		messages[i].MessageID, messages[i].Status, messages[i].ErrorCode = found.ID, status, ""
	}
	return nil
}

func (s *Service) settle(ctx context.Context, send *emailapi.Send, messages []emailapi.Message) error {
	units := send.UnitsToRefund(messages)
	reference := emailapi.RoundReference(send.ID, send.Round)
	if units > 0 {
		charged, err := s.d.Billing.SendCharged(ctx, reference)
		if err != nil {
			return fmt.Errorf("emailapi: reading the charge of %s: %w", reference, err)
		}
		if charged {
			quote := roundQuote(*send)
			if err := s.d.Billing.RefundSend(context.WithoutCancel(ctx), send.WorkspaceID, reference, quote, units); err != nil {
				s.d.Logf("[email-api] CRITICAL %d unsent emails of round %s were not refunded yet; the next call settles them: %v", units, reference, err)
				return nil
			}
		}
	}
	if err := s.d.Sends.SettleRound(context.WithoutCancel(ctx), send.ID, send.Round); err != nil {
		s.d.Logf("[email-api] round %s was refunded but not marked settled; the next call checks it again: %v", reference, err)
		return nil
	}
	send.RoundSettled = true
	return nil
}

func (s *Service) dropSuppressed(ctx context.Context, send emailapi.Send, messages []emailapi.Message, outstanding []emailapi.Message) ([]emailapi.Message, error, error) {
	addresses := make([]emailaddress.Address, len(outstanding))
	for i, m := range outstanding {
		addresses[i] = m.Address
	}
	suppressed, err := s.d.Suppressions.Suppressed(ctx, send.WorkspaceID, addresses)
	if err != nil {
		return nil, nil, fmt.Errorf("emailapi: reading the suppression list: %w", err)
	}
	var keep []emailapi.Message
	var refused []int
	for _, m := range outstanding {
		if _, ok := suppressed[m.Address]; ok {
			refused = append(refused, m.Position)
			continue
		}
		keep = append(keep, m)
	}
	if len(refused) == 0 {
		return keep, nil, nil
	}
	if err := s.markRefused(ctx, send, messages, refused, tx.CodeRecipientSuppressed); err != nil {
		return nil, nil, err
	}
	return keep, tx.Refuse(tx.CodeRecipientSuppressed, es.ErrRecipientSuppressed), nil
}

func (s *Service) markRefused(ctx context.Context, send emailapi.Send, messages []emailapi.Message, positions []int, code string) error {
	if err := s.d.Sends.MarkRefused(context.WithoutCancel(ctx), send.ID, positions, code); err != nil {
		return fmt.Errorf("emailapi: recording refused recipients: %w", err)
	}
	refused := make(map[int]bool, len(positions))
	for _, p := range positions {
		refused[p] = true
	}
	for i := range messages {
		if refused[messages[i].Position] && messages[i].MessageID == "" {
			messages[i].Status, messages[i].ErrorCode = emailapi.StatusRefused, code
		}
	}
	return nil
}

func roundQuote(send emailapi.Send) quote {
	return quote{Units: send.RoundUnits, UnitMicros: send.RoundUnitMicros, UnitCostMicros: send.RoundUnitCostMicros, TotalMicros: send.RoundUnits * send.RoundUnitMicros}
}

func positionsOf(messages []emailapi.Message) []int {
	out := make([]int, len(messages))
	for i, m := range messages {
		out[i] = m.Position
	}
	return out
}

func (s *Service) round(ctx context.Context, send *emailapi.Send, messages []emailapi.Message, toSend []emailapi.Message, p prepared) (int64, error, error) {
	q, err := s.d.Billing.QuoteSend(ctx, send.WorkspaceID, int64(len(toSend)), es.EncodedAttachmentsBytes(p.attachments))
	if err != nil {
		return 0, tx.ChargeRefusal(err), s.markRefused(ctx, *send, messages, positionsOf(toSend), tx.CodeOf(tx.ChargeRefusal(err)))
	}
	next := send.Round + 1
	positions := positionsOf(toSend)
	if err := s.d.Sends.OpenRound(ctx, send.ID, next, q.Units, q.UnitMicros, q.UnitCostMicros, positions); err != nil {
		return 0, nil, fmt.Errorf("emailapi: opening round %d: %w", next, err)
	}
	send.Round, send.RoundUnits, send.RoundUnitMicros, send.RoundUnitCostMicros, send.RoundSettled = next, q.Units, q.UnitMicros, q.UnitCostMicros, false
	inRound := make(map[int]bool, len(positions))
	for _, pos := range positions {
		inRound[pos] = true
	}
	for i := range messages {
		if inRound[messages[i].Position] {
			messages[i].Round, messages[i].Status, messages[i].ErrorCode = next, emailapi.StatusPending, ""
		}
	}
	reference := emailapi.RoundReference(send.ID, next)
	if _, err := s.d.Billing.ChargeSend(ctx, send.WorkspaceID, reference, q); err != nil {
		refused := tx.ChargeRefusal(err)
		if markErr := s.markRefused(ctx, *send, messages, positions, tx.CodeOf(refused)); markErr != nil {
			return 0, nil, markErr
		}
		if settleErr := s.d.Sends.SettleRound(context.WithoutCancel(ctx), send.ID, next); settleErr == nil {
			send.RoundSettled = true
		}
		return 0, refused, nil
	}
	deadline := s.d.Now().Add(emailapi.ProcessingLimit)
	var firstErr error
	var retryAfter int64
	for i, m := range toSend {
		if ctx.Err() != nil || s.d.Now().After(deadline) {
			break
		}
		code, sendErr := s.sendOne(ctx, *send, messages, m, p)
		if sendErr == nil {
			continue
		}
		if firstErr == nil {
			firstErr = sendErr
		}
		if seconds, ok := tx.RetryAfterSeconds(sendErr); ok {
			retryAfter = seconds
		}
		rest := []int{m.Position}
		if !recipientScoped(code) {
			rest = positionsOf(toSend[i:])
		}
		if err := s.markRefused(ctx, *send, messages, rest, code); err != nil {
			return 0, nil, err
		}
		if !recipientScoped(code) {
			break
		}
	}
	if err := s.settle(ctx, send, messages); err != nil {
		return 0, nil, err
	}
	return retryAfter, firstErr, nil
}

func (s *Service) sendOne(ctx context.Context, send emailapi.Send, messages []emailapi.Message, m emailapi.Message, p prepared) (string, error) {
	content, err := p.compose(m.Address)
	if err != nil {
		refused := refusal(err)
		return tx.CodeOf(refused), refused
	}
	sent, err := s.d.Gateway.Send(ctx, es.SendRequest{
		WorkspaceID: send.WorkspaceID,
		SenderID:    p.sender.ID,
		To:          m.Address,
		Kind:        es.KindTransactional,
		RunKind:     es.RunAPI,
		RunID:       send.ID,
		RecipientID: emailapi.RecipientID(m.Position),
		ReplyTo:     p.draft.ReplyTo,
		Headers:     p.draft.Headers,
	}, func(es.Envelope) (es.Content, error) { return content, nil })
	if err != nil {
		refused := tx.SendRefusal(err)
		return tx.CodeOf(refused), refused
	}
	status := emailapi.StatusOf(sent.Status)
	for i := range messages {
		if messages[i].Position == m.Position {
			messages[i].MessageID, messages[i].Status, messages[i].ErrorCode = sent.ID, status, ""
		}
	}
	if err := s.d.Sends.MarkSent(context.WithoutCancel(ctx), send.ID, m.Position, sent.ID, status); err != nil {
		s.d.Logf("[email-api] CRITICAL message %s of send %s went out but its log row was not updated; the next call with the same key repairs it: %v", sent.ID, send.ID, err)
	}
	return "", nil
}
