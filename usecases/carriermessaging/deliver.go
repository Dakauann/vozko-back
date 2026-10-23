package carriermessaging_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/working_hours"
)

const (
	ErrorCodeProviderUnavailable = "provider_unavailable"
	ErrorCodeChargeRefused       = "charge_refused"
	ErrorCodeWhatsAppRefused     = "whatsapp_refused"
	spendWindow                  = 30 * 24 * time.Hour
	frequencyWeek                = 7 * 24 * time.Hour
)

var (
	ErrChannelInvalid   = errors.New("carrier messaging: unknown channel")
	errAlreadyFellBack  = errors.New("carrier messaging: the fallback already ran")
	errFallbackNotARoot = errors.New("carrier messaging: a fallback step has no fallback of its own")
)

type DeliverRequest struct {
	MessageID   string
	WorkspaceID string
	Channel     carriermessaging.Channel
	SenderID    string
	Phone       string
	ContactName string
	Lead        *lead.Lead
	RunKind     carriermessaging.RunKind
	Marketing   bool
	RunID       string
	Variant     int
	Content     rcs.Content
	Text        string
	Vars        []string
	Fallback    carriermessaging.FallbackSpec
	SentBy      conversation.SentBy

	RecordedByCaller bool
}

type plannedCharge struct {
	kind     carriermessaging.ChargeKind
	key      string
	quantity int
}

func (s *Service) Deliver(ctx context.Context, req DeliverRequest) (*carriermessaging.Message, error) {
	return s.deliver(ctx, req, nil)
}

func (s *Service) deliver(ctx context.Context, req DeliverRequest, parent *carriermessaging.Message) (*carriermessaging.Message, error) {
	now := s.Now()
	if !req.Channel.Valid() {
		return nil, ErrChannelInvalid
	}
	if !s.launched(req.Channel) {
		return nil, carriermessaging.ErrChannelNotLaunched
	}
	if parent != nil && req.Fallback.Plan().Enabled() {
		return nil, errFallbackNotARoot
	}
	phone := shared.NormalizePhone(req.Phone)
	if phone == "" {
		return nil, carriermessaging.ErrPhoneInvalid
	}
	out, err := s.prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	contactLead := req.Lead
	if contactLead == nil {
		if contactLead, err = s.Leads.EnsureLead(ctx, req.WorkspaceID, phone, req.ContactName); err != nil {
			return nil, err
		}
	}
	if parent == nil {
		if err := s.admit(ctx, req, phone, contactLead, now); err != nil {
			return nil, err
		}
	}
	_, conv, err := s.ensureConversation(ctx, req.Channel, req.WorkspaceID, out.senderID, phone, req.ContactName, contactLead)
	if err != nil {
		return nil, err
	}
	if err := s.sendableContentMedia(req.WorkspaceID, *conv, out.content); err != nil {
		return nil, err
	}

	messageID := req.MessageID
	if messageID == "" {
		messageID = s.NewID()
	}
	reference, err := carriermessaging.ReferenceFor(messageID)
	if err != nil {
		return nil, err
	}
	if parent != nil {
		linked, err := s.Repos.Messages.LinkFallback(ctx, parent.ID, messageID)
		if err != nil {
			return nil, err
		}
		if !linked {
			return nil, errAlreadyFellBack
		}
	}
	messageCharge, conversationCharge := s.chargePlan(req.Channel, out, *conv, req.RunKind, messageID, now)
	if parent == nil {
		if err := s.affords(ctx, req.WorkspaceID, req.Channel, messageCharge, conversationCharge); err != nil {
			return nil, err
		}
	}
	if conversationCharge != nil {
		if opened, err := s.conversations(req.Channel).OpenBilledConversation(ctx, conv.ID, now); err != nil {
			return nil, err
		} else if !opened {
			conversationCharge = nil
		}
	}

	fallback := carriermessaging.FallbackSpec{}
	if parent == nil && req.Channel == carriermessaging.ChannelRCS {
		fallback = req.Fallback
	}
	validity := fallback.Plan().Validity()
	expires := now.Add(validity)
	msg := &carriermessaging.Message{
		ID: messageID, WorkspaceID: req.WorkspaceID, Channel: req.Channel, Reference: reference,
		SenderID: out.senderID, Phone: phone, LeadID: leadIDOf(contactLead), ConversationID: conv.ID,
		RunKind: req.RunKind, Marketing: req.Marketing, RunID: req.RunID, Variant: req.Variant,
		Content: out.content, Text: out.text, Segments: out.segments, Class: out.class,
		Step: stepOf(req.Channel), Status: carriermessaging.StatusPending, Fallback: fallback,
		ParentMessageID: parentIDOf(parent), ExpiresAt: &expires, CreatedAt: now,
	}

	if err := s.debit(req.WorkspaceID, messageCharge, false); err != nil {
		if parent == nil {
			return nil, err
		}
		msg.Status, msg.ErrorCode, msg.FailedAt = carriermessaging.StatusFailed, ErrorCodeChargeRefused, &now
		if cerr := s.Repos.Messages.Create(ctx, msg); cerr != nil {
			log.Printf("[carrier] refused fallback row %s: %v", msg.ID, cerr)
		}
		return msg, nil
	}
	if err := s.debit(req.WorkspaceID, conversationCharge, true); err != nil {
		s.refund(req.WorkspaceID, messageCharge)
		return nil, err
	}
	if messageCharge != nil {
		msg.ChargeReference = carriermessaging.ChargeReference(messageCharge.kind, messageCharge.key)
	}
	if err := s.Repos.Messages.Create(ctx, msg); err != nil {
		s.refund(req.WorkspaceID, messageCharge)
		return nil, err
	}

	if !req.RecordedByCaller {
		s.recordOutbound(ctx, *msg, out.from, sentByOf(req.SentBy), now)
	}
	if err := s.conversations(req.Channel).RecordOutbound(ctx, conv.ID, now); err != nil {
		log.Printf("[carrier] conversation %s outbound clock: %v", conv.ID, err)
	}

	outcome, sendErr := s.send(ctx, req.Channel, out, *msg, validity)
	switch {
	case errors.Is(sendErr, carriermessaging.ErrSendOutcomeUnknown):
		log.Printf("[carrier] provider answer for %s lost, waiting for its report: %v", reference, sendErr)
		return msg, nil
	case sendErr != nil:
		log.Printf("[carrier] provider send %s: %v", reference, sendErr)
		return s.fail(ctx, *msg, carriermessaging.StatusFailed, ErrorCodeProviderUnavailable)
	case !outcome.Accepted:
		return s.fail(ctx, *msg, carriermessaging.StatusRejected, outcome.ErrorCode)
	}
	return s.advance(ctx, reference, carriermessaging.StatusAccepted, "", now)
}

type outbound struct {
	senderID       string
	from           string
	content        rcs.Content
	text           string
	class          rcs.BillingClass
	segments       int
	conversational bool
}

func (s *Service) prepare(ctx context.Context, req DeliverRequest) (outbound, error) {
	if req.Channel == carriermessaging.ChannelSMS {
		return s.prepareSMS(ctx, req)
	}
	return s.prepareRCS(ctx, req)
}

func (s *Service) send(ctx context.Context, channel carriermessaging.Channel, out outbound, msg carriermessaging.Message, validity time.Duration) (carriermessaging.SendOutcome, error) {
	if channel == carriermessaging.ChannelSMS {
		return s.SMS.SendSMS(ctx, carriermessaging.SMSRequest{Reference: msg.Reference, From: out.from, Phone: msg.Phone, Text: out.text})
	}
	outgoing, err := s.tracked(out.content, msg.ID)
	if err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %v", carriermessaging.ErrProviderUnavailable, err)
	}
	return s.Provider.Send(ctx, carriermessaging.SendRequest{
		Reference: msg.Reference, SenderProfileID: out.from, Phone: msg.Phone, Content: outgoing, Validity: validity,
	})
}

func stepOf(channel carriermessaging.Channel) carriermessaging.Step {
	if channel == carriermessaging.ChannelSMS {
		return carriermessaging.StepSMS
	}
	return carriermessaging.StepRCS
}

func parentIDOf(parent *carriermessaging.Message) string {
	if parent == nil {
		return ""
	}
	return parent.ID
}

func sentByOf(sentBy conversation.SentBy) conversation.SentBy {
	if sentBy == (conversation.SentBy{}) {
		return conversation.SentBySystem()
	}
	return sentBy
}

func (s *Service) admit(ctx context.Context, req DeliverRequest, phone string, contactLead *lead.Lead, now time.Time) error {
	if contactLead != nil && contactLead.Blocked {
		return carriermessaging.ErrRecipientRefused
	}
	switch {
	case req.RunKind == carriermessaging.RunTest:
		settings, err := s.settingsFor(ctx, req.WorkspaceID)
		if err != nil {
			return err
		}
		start, err := dayStart(settings, now)
		if err != nil {
			return err
		}
		tests, err := s.Repos.Messages.CountRunKindSince(ctx, req.WorkspaceID, carriermessaging.RunTest, start)
		if err != nil {
			return err
		}
		if tests >= carriermessaging.DailyFreeTestSends {
			return carriermessaging.ErrTestLimit
		}
	case req.Marketing:
		if err := campaign.RecipientRefusal(contactLead); err != nil {
			return fmt.Errorf("%w: %v", carriermessaging.ErrRecipientRefused, err)
		}
		suppressed, err := s.Repos.Suppressions.IsSuppressed(ctx, req.WorkspaceID, phone)
		if err != nil {
			return err
		}
		if suppressed {
			return carriermessaging.ErrSuppressed
		}
		return s.marketingWindow(ctx, req.WorkspaceID, phone, now)
	}
	return nil
}

func (s *Service) marketingWindow(ctx context.Context, workspaceID, phone string, now time.Time) error {
	settings, err := s.settingsFor(ctx, workspaceID)
	if err != nil {
		return err
	}
	schedule, err := settings.QuietHours.Compile()
	if err != nil {
		return err
	}
	if !schedule.IsOpen(now) {
		next, ok := schedule.NextOpen(now)
		if !ok {
			return carriermessaging.ErrQuietHours
		}
		return &carriermessaging.QuietHoursError{NextOpen: next}
	}
	start, err := dayStart(settings, now)
	if err != nil {
		return err
	}
	today, err := s.Repos.Messages.CountMarketingSince(ctx, workspaceID, phone, start)
	if err != nil {
		return err
	}
	week, err := s.Repos.Messages.CountMarketingSince(ctx, workspaceID, phone, now.Add(-frequencyWeek))
	if err != nil {
		return err
	}
	if !settings.FrequencyCap.Allows(today, week) {
		return carriermessaging.ErrFrequencyCap
	}
	return nil
}

func (s *Service) settingsFor(ctx context.Context, workspaceID string) (carriermessaging.Settings, error) {
	stored, err := s.Repos.Settings.Get(ctx, workspaceID)
	if err != nil {
		return carriermessaging.Settings{}, err
	}
	if stored != nil {
		return *stored, nil
	}
	timezone, err := s.Timezones.Timezone(ctx, workspaceID)
	if err != nil {
		return carriermessaging.Settings{}, err
	}
	return carriermessaging.DefaultSettings(workspaceID, timezone), nil
}

func dayStart(settings carriermessaging.Settings, now time.Time) (time.Time, error) {
	spec := working_hours.Spec{Timezone: settings.QuietHours.Timezone}
	schedule, err := spec.Compile()
	if err != nil {
		return time.Time{}, err
	}
	local := now.In(schedule.Location())
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, local.Location()), nil
}

func (s *Service) conversations(channel carriermessaging.Channel) carriermessaging.ConversationRepository {
	if channel == carriermessaging.ChannelSMS {
		return s.Repos.SMSConversations
	}
	return s.Repos.RCSConversations
}

func (s *Service) ensureConversation(ctx context.Context, channel carriermessaging.Channel, workspaceID, senderID, phone, name string, contactLead *lead.Lead) (*carriermessaging.Contact, *carriermessaging.Conversation, error) {
	contact, err := s.Repos.Contacts.FindOrCreate(ctx, workspaceID, phone, name)
	if err != nil {
		return nil, nil, err
	}
	if contact.LeadID == nil && contactLead != nil && contactLead.ID != "" {
		if err := s.Repos.Contacts.SetLead(ctx, contact.ID, contactLead.ID); err != nil {
			return nil, nil, err
		}
		leadID := contactLead.ID
		contact.LeadID = &leadID
	}
	conv, _, err := s.conversations(channel).FindOrCreate(ctx, workspaceID, senderID, contact.ID)
	if err != nil {
		return nil, nil, err
	}
	conv.Channel = channel
	return contact, conv, nil
}

func (s *Service) chargePlan(channel carriermessaging.Channel, out outbound, conv carriermessaging.Conversation, kind carriermessaging.RunKind, messageID string, now time.Time) (*plannedCharge, *plannedCharge) {
	if kind == carriermessaging.RunTest {
		return nil, nil
	}
	if channel == carriermessaging.ChannelSMS {
		return &plannedCharge{kind: carriermessaging.ChargeSMSSegment, key: messageID, quantity: out.segments}, nil
	}
	message := &plannedCharge{kind: carriermessaging.ChargeForClass(out.class), key: messageID, quantity: 1}
	if !out.conversational {
		return message, nil
	}
	if conv.BilledConversationOpen(now) {
		return nil, nil
	}
	if conv.LastCustomerMessageAt != nil && now.Before(conv.LastCustomerMessageAt.Add(carriermessaging.ConversationWindow)) {
		return nil, conversationCharge(conv.ID, now)
	}
	return message, nil
}

func conversationCharge(conversationID string, openedAt time.Time) *plannedCharge {
	return &plannedCharge{kind: carriermessaging.ChargeRCSConversation, key: conversationID + ":" + openedAt.UTC().Format(time.RFC3339), quantity: 1}
}

func (s *Service) affords(ctx context.Context, workspaceID string, channel carriermessaging.Channel, charges ...*plannedCharge) error {
	var needed int64
	for _, c := range charges {
		if c == nil {
			continue
		}
		amount, err := s.Charges.Amount(workspaceID, c.kind, c.quantity)
		if err != nil {
			return err
		}
		needed += amount
	}
	if needed == 0 {
		return nil
	}
	inbound, err := s.Charges.Price(workspaceID, channel.InboundCharge())
	if err != nil {
		return err
	}
	spend, err := s.Spend.SpendSince(ctx, workspaceID, s.Now().Add(-spendWindow))
	if err != nil {
		return err
	}
	balance, err := s.Balance.GetBalance(workspaceID)
	if err != nil {
		return err
	}
	floor := carriermessaging.ReplyFloor(spend, inbound.PriceMicros)
	if !carriermessaging.AffordsSend(balance, needed, floor) {
		return &carriermessaging.ShortfallError{MissingMicros: carriermessaging.Shortfall(balance, needed, floor)}
	}
	return nil
}

func (s *Service) debit(workspaceID string, c *plannedCharge, allowNegative bool) error {
	if c == nil {
		return nil
	}
	return s.Charges.Debit(workspaceID, c.kind, c.key, c.quantity, allowNegative)
}

func (s *Service) refund(workspaceID string, c *plannedCharge) {
	if c == nil {
		return
	}
	if err := s.Charges.Refund(workspaceID, c.kind, c.key, c.quantity); err != nil {
		log.Printf("CRITICAL: [carrier] refund %s of workspace %s failed: %v", carriermessaging.ChargeReference(c.kind, c.key), workspaceID, err)
	}
}

func leadIDOf(l *lead.Lead) string {
	if l == nil {
		return ""
	}
	return l.ID
}

func (s *Service) advance(ctx context.Context, reference string, next carriermessaging.Status, code string, at time.Time) (*carriermessaging.Message, error) {
	m, moved, err := s.Repos.Messages.Advance(ctx, reference, next, code, at)
	if err != nil {
		return nil, err
	}
	if moved {
		s.afterMove(ctx, *m)
	}
	return m, nil
}

func (s *Service) fail(ctx context.Context, m carriermessaging.Message, status carriermessaging.Status, code string) (*carriermessaging.Message, error) {
	return s.advance(ctx, m.Reference, status, code, s.Now())
}

func (s *Service) afterMove(ctx context.Context, m carriermessaging.Message) {
	s.publishStatus(m)
	if !m.Status.Terminal() {
		return
	}
	s.refundMessage(m)
	s.runFallback(ctx, m)
}

func (s *Service) refundMessage(m carriermessaging.Message) {
	if m.ChargeReference == "" {
		return
	}
	charge := plannedCharge{kind: carriermessaging.ChargeForClass(m.Class), key: m.ID, quantity: 1}
	if m.Step == carriermessaging.StepSMS {
		charge = plannedCharge{kind: carriermessaging.ChargeSMSSegment, key: m.ID, quantity: m.Segments}
	}
	if carriermessaging.ChargeReference(charge.kind, charge.key) != m.ChargeReference {
		log.Printf("CRITICAL: [carrier] message %s charge %s does not match its step, not refunding", m.ID, m.ChargeReference)
		return
	}
	s.refund(m.WorkspaceID, &charge)
}
