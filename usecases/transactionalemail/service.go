package transactionalemail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	em "vozko/domain/emailinbox"
	es "vozko/domain/emailsending"
	"vozko/domain/lead"
	balance_usecase "vozko/usecases/balance"
)

type Origin string

const (
	OriginWorkflow Origin = "workflow"
	OriginAgent    Origin = "agent"
	OriginCopilot  Origin = "copilot"
	OriginAPI      Origin = "api"
	OriginOutreach Origin = "outreach"
)

func (o Origin) Valid() bool {
	switch o {
	case OriginWorkflow, OriginAgent, OriginCopilot, OriginAPI, OriginOutreach:
		return true
	}
	return false
}

func (o Origin) startsConversations() bool {
	return o == OriginOutreach
}

const chargeKeyPrefix = "tx:"

type Body struct {
	Text     string
	DesignID string
	Document *emaildesign.Document
}

func (b Body) chosen() int {
	n := 0
	if b.Text != "" {
		n++
	}
	if strings.TrimSpace(b.DesignID) != "" {
		n++
	}
	if b.Document != nil {
		n++
	}
	return n
}

type Request struct {
	WorkspaceID    string
	SenderID       string
	To             string
	Subject        string
	Body           Body
	LeadID         string
	Variables      map[string]string
	IdempotencyKey string
	Origin         Origin
	SentBy         conversation.SentBy
}

type Result struct {
	MessageID         string `json:"messageId"`
	ProviderMessageID string `json:"providerMessageId,omitempty"`
	To                string `json:"to"`
	Repeated          bool   `json:"repeated"`
	ConversationID    string `json:"conversationId,omitempty"`
}

type Sender interface {
	Send(ctx context.Context, req Request) (Result, error)
}

type Billing interface {
	ChargeMessageRound(ctx context.Context, workspaceID, messageKey string) (balance_usecase.EmailMessageCharge, error)
	RefundMessage(ctx context.Context, workspaceID, messageKey string, charge balance_usecase.EmailQuote) error
}

type Designs interface {
	Get(ctx context.Context, workspaceID, id string) (*emailcampaign.Design, error)
}

type LeadFinder interface {
	FindByEmail(workspaceID, email string) (*lead.Lead, error)
}

type Deps struct {
	Gateway    es.Gateway
	Billing    Billing
	Runs       es.RunMessages
	Designs    Designs
	LeadFacts  lead.EmailFactsReader
	Leads      LeadFinder
	Transcript em.SentEmailTranscript
	Logf       func(format string, args ...any)
}

type Service struct {
	deps Deps
}

var _ Sender = (*Service)(nil)

func New(deps Deps) (*Service, error) {
	missing := map[string]bool{
		"gateway": deps.Gateway == nil, "billing": deps.Billing == nil, "runs": deps.Runs == nil,
		"designs": deps.Designs == nil, "lead facts": deps.LeadFacts == nil, "leads": deps.Leads == nil,
		"transcript": deps.Transcript == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", ErrIncomplete, name)
		}
	}
	if deps.Logf == nil {
		deps.Logf = log.Printf
	}
	return &Service{deps: deps}, nil
}

func (s *Service) Send(ctx context.Context, req Request) (Result, error) {
	if err := req.check(); err != nil {
		return Result{}, err
	}
	facts, to, err := s.recipient(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if err := leadRefusal(req.Origin, facts); err != nil {
		return Result{}, err
	}
	content, err := s.compose(ctx, req, emaildesign.LeadPersonalization(facts, to.String(), req.Variables))
	if err != nil {
		return Result{}, err
	}
	delivered := transcriptOf(req, facts, to, content)
	runID := RunID(req.WorkspaceID, req.IdempotencyKey)
	if recorded, found, err := s.recorded(ctx, req.WorkspaceID, runID); err != nil || found {
		return s.transcribeRepeat(ctx, delivered, recorded), err
	}
	charge, err := s.deps.Billing.ChargeMessageRound(ctx, req.WorkspaceID, chargeKeyPrefix+runID)
	if err != nil {
		return Result{}, chargeRefusal(err)
	}
	if !charge.Recorded {
		held, err := s.heldElsewhere(ctx, req.WorkspaceID, runID)
		return s.transcribeRepeat(ctx, delivered, held), err
	}
	sent, err := s.deps.Gateway.Send(ctx, es.SendRequest{
		WorkspaceID: req.WorkspaceID,
		SenderID:    strings.TrimSpace(req.SenderID),
		To:          to,
		Kind:        es.KindTransactional,
		RunKind:     es.RunTriggered,
		RunID:       runID,
		Tags:        map[string]string{"origin": string(req.Origin)},
	}, func(env es.Envelope) (es.Content, error) {
		if env.Sender.ID != "" {
			delivered.SenderID = env.Sender.ID
		}
		return content, nil
	})
	if err != nil {
		if refundErr := s.deps.Billing.RefundMessage(ctx, req.WorkspaceID, charge.Key, charge.Quote); refundErr != nil {
			s.deps.Logf("[transactional-email] CRITICAL send %s of workspace %s failed and its charge %s was not refunded: %v", runID, req.WorkspaceID, charge.Key, refundErr)
		}
		return Result{}, sendRefusal(err)
	}
	return s.transcribe(ctx, delivered, Result{MessageID: sent.ID, ProviderMessageID: sent.ProviderMessageID, To: to.String()}), nil
}

func transcriptOf(req Request, facts lead.EmailFacts, to emailaddress.Address, content es.Content) em.OutboundEmail {
	return em.OutboundEmail{
		WorkspaceID:  req.WorkspaceID,
		LeadID:       facts.ID,
		LeadName:     facts.FullName(),
		To:           to,
		SenderID:     strings.TrimSpace(req.SenderID),
		Subject:      content.Subject,
		Text:         content.Text,
		HTML:         content.HTML,
		SentBy:       req.SentBy,
		OpensContact: req.Origin.startsConversations(),
	}
}

func (s *Service) transcribe(ctx context.Context, email em.OutboundEmail, sent Result) Result {
	email.ExternalID = em.OutboundExternalID(sent.ProviderMessageID, sent.MessageID)
	conversationID, err := s.deps.Transcript.Transcribe(ctx, email)
	if err != nil {
		s.deps.Logf("[transactional-email] CRITICAL email %s of workspace %s was sent but is missing from the inbox transcript: %v", sent.MessageID, email.WorkspaceID, err)
		return sent
	}
	sent.ConversationID = conversationID
	return sent
}

func (s *Service) transcribeRepeat(ctx context.Context, email em.OutboundEmail, earlier Result) Result {
	if earlier.MessageID == "" || earlier.ProviderMessageID == "" {
		return earlier
	}
	return s.transcribe(ctx, email, earlier)
}

func leadRefusal(origin Origin, facts lead.EmailFacts) error {
	if facts.ID == "" {
		return nil
	}
	reason := campaign.LeadRefusal(campaign.LeadFacts{
		Found:       true,
		HasIdentity: true,
		Blocked:     facts.Blocked,
		OptedOut:    facts.OptedOut && origin.startsConversations(),
	})
	switch reason {
	case campaign.SkipBlocked:
		return refuse(CodeLeadBlocked, ErrLeadBlocked)
	case campaign.SkipOptedOut:
		return refuse(CodeLeadOptedOut, ErrLeadOptedOut)
	}
	return nil
}

func (req Request) check() error {
	switch {
	case strings.TrimSpace(req.WorkspaceID) == "":
		return ErrWorkspaceRequired
	case strings.TrimSpace(req.IdempotencyKey) == "":
		return ErrIdempotencyKeyRequired
	case !req.Origin.Valid():
		return fmt.Errorf("%w: %q", ErrOriginInvalid, req.Origin)
	case !req.SentBy.FromTheBusiness():
		return ErrSentByInvalid
	case req.Body.chosen() != 1:
		return refuse(CodeMessageInvalid, ErrBodyInvalid)
	}
	return nil
}

func RunID(workspaceID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(workspaceID + "\x00" + idempotencyKey))
	return hex.EncodeToString(sum[:])
}

func (s *Service) recipient(ctx context.Context, req Request) (lead.EmailFacts, emailaddress.Address, error) {
	var facts lead.EmailFacts
	leadID := strings.TrimSpace(req.LeadID)
	if leadID != "" {
		found, err := s.facts(ctx, req.WorkspaceID, leadID)
		if err != nil {
			return facts, "", err
		}
		facts = found
	}
	raw := strings.TrimSpace(req.To)
	if raw == "" {
		raw = facts.Email
	}
	if strings.TrimSpace(raw) == "" && leadID != "" {
		return facts, "", refuse(CodeLeadNoEmail, ErrLeadNoEmail)
	}
	if strings.TrimSpace(raw) == "" {
		return facts, "", refuse(CodeRecipientMissing, ErrRecipientMissing)
	}
	to, err := emailaddress.Normalize(raw)
	if err != nil {
		return facts, "", refuse(CodeAddressInvalid, err)
	}
	if leadID != "" {
		return facts, to, nil
	}
	owner, err := s.deps.Leads.FindByEmail(req.WorkspaceID, to.String())
	switch {
	case errors.Is(err, lead.ErrLeadNotFound) || errors.Is(err, lead.ErrLeadInvalid):
		return facts, to, nil
	case err != nil:
		return facts, "", fmt.Errorf("transactionalemail: finding the lead of the recipient: %w", err)
	case owner == nil:
		return facts, to, nil
	}
	found, err := s.facts(ctx, req.WorkspaceID, owner.ID)
	return found, to, err
}

func (s *Service) facts(ctx context.Context, workspaceID, leadID string) (lead.EmailFacts, error) {
	found, err := s.deps.LeadFacts.EmailFacts(ctx, workspaceID, []string{leadID})
	if err != nil {
		return lead.EmailFacts{}, fmt.Errorf("transactionalemail: reading the lead: %w", err)
	}
	facts, ok := found[leadID]
	if !ok {
		return lead.EmailFacts{}, refuse(CodeLeadNotFound, ErrLeadNotFound)
	}
	return facts, nil
}

func (s *Service) compose(ctx context.Context, req Request, p emaildesign.Personalization) (es.Content, error) {
	subject, err := emaildesign.RenderSubject(req.Subject, p)
	if err != nil {
		return es.Content{}, refuse(CodeSubjectInvalid, err)
	}
	if req.Body.Text != "" {
		text, err := emaildesign.ExpandText(req.Body.Text, p)
		if err != nil {
			return es.Content{}, refuse(CodeMessageInvalid, err)
		}
		return es.Content{Subject: subject, Text: text, HTML: em.ReplyHTML(text)}, nil
	}
	doc, err := s.document(ctx, req)
	if err != nil {
		return es.Content{}, err
	}
	return RenderDocument(doc, req.Subject, subject, p)
}

func RenderDocument(doc emaildesign.Document, rawSubject, subject string, p emaildesign.Personalization) (es.Content, error) {
	rendered, err := emaildesign.Render(doc, emaildesign.RenderInput{Personalization: p, Subject: rawSubject, Transactional: true})
	if err != nil {
		return es.Content{}, refuse(CodeMessageInvalid, err)
	}
	return es.Content{Subject: subject, Text: rendered.Text, HTML: rendered.HTML}, nil
}

func RenderDesign(ctx context.Context, designs Designs, workspaceID, designID, rawSubject string, p emaildesign.Personalization) (es.Content, error) {
	subject, err := emaildesign.RenderSubject(rawSubject, p)
	if err != nil {
		return es.Content{}, refuse(CodeSubjectInvalid, err)
	}
	doc, err := DesignDocument(ctx, designs, workspaceID, designID)
	if err != nil {
		return es.Content{}, err
	}
	return RenderDocument(doc, rawSubject, subject, p)
}

func (s *Service) document(ctx context.Context, req Request) (emaildesign.Document, error) {
	if req.Body.Document != nil {
		return *req.Body.Document, nil
	}
	return DesignDocument(ctx, s.deps.Designs, req.WorkspaceID, req.Body.DesignID)
}

func DesignDocument(ctx context.Context, designs Designs, workspaceID, designID string) (emaildesign.Document, error) {
	design, err := designs.Get(ctx, workspaceID, strings.TrimSpace(designID))
	if errors.Is(err, emailcampaign.ErrDesignNotFound) {
		return emaildesign.Document{}, refuse(CodeDesignNotFound, err)
	}
	if err != nil {
		return emaildesign.Document{}, fmt.Errorf("transactionalemail: reading the design: %w", err)
	}
	return design.Document, nil
}

func (s *Service) heldElsewhere(ctx context.Context, workspaceID, runID string) (Result, error) {
	recorded, found, err := s.recorded(ctx, workspaceID, runID)
	if err != nil {
		return Result{}, err
	}
	if found {
		return recorded, nil
	}
	return Result{}, refuse(CodeSendInProgress, ErrSendInProgress)
}

func (s *Service) recorded(ctx context.Context, workspaceID, runID string) (Result, bool, error) {
	messages, err := s.deps.Runs.ListByRun(ctx, workspaceID, es.RunTriggered, runID)
	if err != nil {
		return Result{}, false, fmt.Errorf("transactionalemail: reading earlier attempts: %w", err)
	}
	for _, m := range messages {
		if m.Status != es.MessageFailed {
			return Result{MessageID: m.ID, ProviderMessageID: m.ProviderMessageID, To: m.Address.String(), Repeated: true}, true, nil
		}
	}
	return Result{}, false, nil
}
