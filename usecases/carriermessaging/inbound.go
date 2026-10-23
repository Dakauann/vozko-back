package carriermessaging_usecase

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
	"vozko/domain/workflow"
)

const (
	SelectionKind      = "carrier_suggestion"
	sharedReplyHorizon = 30 * 24 * time.Hour
)

var ErrButtonUnknown = errors.New("carrier messaging: the tapped button is not on the message")

type inboundParty struct {
	workspaceID    string
	senderID       string
	channel        carriermessaging.Channel
	conversational bool
	automation     conversation.ChannelAutomation
}

type tapOutcome struct {
	selection *workflow.OptionSelection
	optedOut  bool
	duplicate bool
}

func (s *Service) resolveInbound(ctx context.Context, in carriermessaging.Inbound, phone string, at time.Time) (inboundParty, error) {
	switch in.Channel {
	case carriermessaging.ChannelRCS:
		sender, err := s.Repos.Senders.FindByProfileID(ctx, s.Provider.Name(), in.To)
		if err != nil {
			return inboundParty{}, err
		}
		return inboundParty{
			workspaceID: sender.WorkspaceID, senderID: sender.ID, channel: in.Channel,
			conversational: sender.BillingCategory == rcs.BillingConversational, automation: sender.Automation,
		}, nil
	case carriermessaging.ChannelSMS:
		route, err := s.Repos.Routes.FindByNumber(ctx, in.To)
		if err == nil {
			return inboundParty{workspaceID: route.WorkspaceID, senderID: route.ID, channel: in.Channel, automation: route.Automation}, nil
		}
		if !errors.Is(err, sms.ErrRouteNotFound) || in.To != s.SMSSharedNumber {
			if errors.Is(err, sms.ErrRouteNotFound) {
				return inboundParty{}, sms.ErrInboundUnroutable
			}
			return inboundParty{}, err
		}
		last, err := s.Repos.Messages.LatestSMSTo(ctx, phone, at.Add(-sharedReplyHorizon))
		if err != nil {
			return inboundParty{}, err
		}
		if last == nil {
			return inboundParty{}, sms.ErrInboundUnroutable
		}
		lastRoute, err := s.Repos.Routes.FindByIDUnscoped(ctx, last.SenderID)
		if err != nil {
			return inboundParty{}, err
		}
		return inboundParty{workspaceID: lastRoute.WorkspaceID, senderID: lastRoute.ID, channel: in.Channel, automation: lastRoute.Automation}, nil
	}
	return inboundParty{}, ErrChannelInvalid
}

func (s *Service) HandleInbound(ctx context.Context, in carriermessaging.Inbound) error {
	phone := shared.NormalizePhone(in.Phone)
	if phone == "" {
		return carriermessaging.ErrPhoneInvalid
	}
	at := in.ReceivedAt
	if at.IsZero() {
		at = s.Now()
	}
	party, err := s.resolveInbound(ctx, in, phone, at)
	if err != nil {
		return err
	}
	contactLead, err := s.Leads.EnsureLead(ctx, party.workspaceID, phone, in.ContactName)
	if err != nil {
		return err
	}
	contact, conv, err := s.ensureConversation(ctx, party.channel, party.workspaceID, party.senderID, phone, in.ContactName, contactLead)
	if err != nil {
		return err
	}
	answered := s.answeredMessage(ctx, party.workspaceID, phone, in.InReplyTo, at)
	inserted, err := s.Repos.Events.Record(ctx, &carriermessaging.MessageEvent{
		WorkspaceID: party.workspaceID, MessageID: messageIDOf(answered), SenderID: party.senderID, Phone: phone,
		Kind: carriermessaging.EventInbound, DedupKey: "inbound:" + string(party.channel) + ":" + in.ProviderMessageID, OccurredAt: at,
	})
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	if err := s.conversations(party.channel).RecordInbound(ctx, conv.ID, at); err != nil {
		return err
	}
	s.chargeInbound(ctx, party, *conv, phone, in.ProviderMessageID, at)
	s.recordInbound(ctx, party.channel, conv.ID, phone, in.ContactName, in.ProviderMessageID, in.Text, in.Media, at)
	if err := s.Inbox.EnsureAssignment(ctx, party.workspaceID, party.channel, conv.ID, party.senderID); err != nil {
		log.Printf("[carrier] assignment of conversation %s: %v", conv.ID, err)
	}

	if carriermessaging.IsOptOutKeyword(in.Text) {
		return s.suppress(ctx, party.workspaceID, phone, carriermessaging.SuppressionKeyword, messageIDOf(answered), at)
	}
	var selection *workflow.OptionSelection
	if key, ok := rcs.ParsePostback(in.Postback); ok && answered != nil {
		outcome, err := s.tap(ctx, *answered, key, "tap:"+in.ProviderMessageID, at)
		if err != nil {
			return err
		}
		if outcome.optedOut {
			return nil
		}
		selection = outcome.selection
	}
	if s.flooding(ctx, party, *contact, at) {
		return nil
	}
	s.Inbox.DispatchAutomation(ctx, AutomationInput{
		WorkspaceID: party.workspaceID, Channel: party.channel, ConversationID: conv.ID, SenderID: party.senderID,
		ContactRef: phone, LeadID: contact.LeadID, ProviderMessageID: in.ProviderMessageID, Text: in.Text,
		Selection: selection, ReceivedAt: at, Config: party.automation, ConversationOverride: conv.AutomationEnabled,
	})
	return nil
}

func (s *Service) HandleSuggestionClick(ctx context.Context, click carriermessaging.SuggestionClick) error {
	msg, err := s.Repos.Messages.FindByReference(ctx, click.Reference)
	if err != nil {
		return err
	}
	key, ok := rcs.ParsePostback(click.Postback)
	if !ok {
		return ErrButtonUnknown
	}
	at := click.OccurredAt
	if at.IsZero() {
		at = s.Now()
	}
	outcome, err := s.tap(ctx, *msg, key, "tap:"+click.EventID, at)
	if err != nil || outcome.duplicate || outcome.optedOut {
		return err
	}
	conv, err := s.conversations(msg.Channel).FindByID(ctx, msg.ConversationID)
	if err != nil {
		return err
	}
	sender, err := s.Repos.Senders.FindByIDUnscoped(ctx, msg.SenderID)
	if err != nil {
		return err
	}
	s.recordInbound(ctx, msg.Channel, conv.ID, msg.Phone, "", "tap:"+click.EventID, outcome.selection.Title, nil, at)
	var leadID *string
	if msg.LeadID != "" {
		leadID = &msg.LeadID
	}
	s.Inbox.DispatchAutomation(ctx, AutomationInput{
		WorkspaceID: msg.WorkspaceID, Channel: msg.Channel, ConversationID: conv.ID, SenderID: msg.SenderID,
		ContactRef: msg.Phone, LeadID: leadID, ProviderMessageID: "tap:" + click.EventID,
		Text: outcome.selection.Title, Selection: outcome.selection, ReceivedAt: at,
		Config: sender.Automation, ConversationOverride: conv.AutomationEnabled,
	})
	return nil
}

func (s *Service) ResolveLinkClick(ctx context.Context, token string, at time.Time) (string, error) {
	messageID, key, err := s.Links.Verify(token)
	if err != nil {
		return "", ErrLinkInvalid
	}
	msg, err := s.Repos.Messages.FindByID(ctx, messageID)
	if err != nil {
		return "", err
	}
	suggestion, ok := msg.Content.ButtonAt(key)
	if !ok || suggestion.Kind != rcs.SuggestOpenURL {
		return "", ErrLinkInvalid
	}
	if _, err := s.Repos.Events.Record(ctx, &carriermessaging.MessageEvent{
		WorkspaceID: msg.WorkspaceID, MessageID: msg.ID, SenderID: msg.SenderID, Phone: msg.Phone,
		Kind: carriermessaging.EventClick, Button: &key, Label: suggestion.Label,
		DedupKey: "click:" + token + ":" + strconv.FormatInt(at.Unix(), 10), OccurredAt: at,
	}); err != nil {
		log.Printf("[carrier] click on %s: %v", msg.ID, err)
	}
	return suggestion.URL, nil
}

func (s *Service) tap(ctx context.Context, msg carriermessaging.Message, key rcs.ButtonKey, dedupKey string, at time.Time) (tapOutcome, error) {
	suggestion, ok := msg.Content.ButtonAt(key)
	if !ok {
		return tapOutcome{}, ErrButtonUnknown
	}
	inserted, err := s.Repos.Events.Record(ctx, &carriermessaging.MessageEvent{
		WorkspaceID: msg.WorkspaceID, MessageID: msg.ID, SenderID: msg.SenderID, Phone: msg.Phone,
		Kind: carriermessaging.EventTap, Button: &key, Label: suggestion.Label, DedupKey: dedupKey, OccurredAt: at,
	})
	if err != nil {
		return tapOutcome{}, err
	}
	if !inserted {
		return tapOutcome{duplicate: true}, nil
	}
	if suggestion.OptsOut() {
		return tapOutcome{optedOut: true}, s.suppress(ctx, msg.WorkspaceID, msg.Phone, carriermessaging.SuppressionButton, msg.ID, at)
	}
	id := suggestion.Postback
	if id == "" {
		id = suggestion.Label
	}
	return tapOutcome{selection: &workflow.OptionSelection{ID: id, Title: suggestion.Label, Kind: SelectionKind}}, nil
}

func (s *Service) suppress(ctx context.Context, workspaceID, phone string, source carriermessaging.SuppressionSource, messageID string, at time.Time) error {
	if _, err := s.Repos.Suppressions.Add(ctx, carriermessaging.Suppression{
		WorkspaceID: workspaceID, Phone: phone, Source: source, MessageID: messageID, CreatedAt: at,
	}); err != nil {
		return err
	}
	_, err := s.Repos.Events.Record(ctx, &carriermessaging.MessageEvent{
		WorkspaceID: workspaceID, MessageID: messageID, Phone: phone, Kind: carriermessaging.EventOptOut,
		DedupKey: "opt_out:" + workspaceID + ":" + phone + ":" + messageID, OccurredAt: at,
	})
	return err
}

func (s *Service) answeredMessage(ctx context.Context, workspaceID, phone, inReplyTo string, at time.Time) *carriermessaging.Message {
	if inReplyTo != "" {
		if msg, err := s.Repos.Messages.FindByReference(ctx, inReplyTo); err == nil && msg.WorkspaceID == workspaceID {
			return msg
		}
	}
	msg, err := s.Repos.Messages.LatestDeliveredTo(ctx, workspaceID, phone, at.Add(-carriermessaging.ConversationWindow))
	if err != nil {
		log.Printf("[carrier] reply attribution for %s: %v", phone, err)
		return nil
	}
	return msg
}

func messageIDOf(m *carriermessaging.Message) string {
	if m == nil {
		return ""
	}
	return m.ID
}

func (s *Service) chargeInbound(ctx context.Context, party inboundParty, conv carriermessaging.Conversation, phone, providerMessageID string, at time.Time) {
	if !party.conversational {
		if err := s.Charges.Debit(party.workspaceID, party.channel.InboundCharge(), providerMessageID, 1, true); err != nil {
			log.Printf("CRITICAL: [carrier] inbound charge %s of workspace %s: %v", providerMessageID, party.workspaceID, err)
		}
		return
	}
	if conv.BilledConversationOpen(at) {
		return
	}
	started, err := s.Repos.Messages.LatestDeliveredTo(ctx, party.workspaceID, phone, at.Add(-carriermessaging.ConversationWindow))
	if err != nil || started == nil || started.Channel != party.channel {
		if err != nil {
			log.Printf("[carrier] conversation start for %s: %v", phone, err)
		}
		return
	}
	opened, err := s.conversations(party.channel).OpenBilledConversation(ctx, conv.ID, at)
	if err != nil || !opened {
		if err != nil {
			log.Printf("[carrier] open conversation %s: %v", conv.ID, err)
		}
		return
	}
	if err := s.debit(party.workspaceID, conversationCharge(conv.ID, at), true); err != nil {
		log.Printf("CRITICAL: [carrier] conversation charge %s of workspace %s: %v", conv.ID, party.workspaceID, err)
		return
	}
	s.refundMessage(*started)
}

func (s *Service) flooding(ctx context.Context, party inboundParty, contact carriermessaging.Contact, at time.Time) bool {
	count, err := s.Repos.Events.CountSince(ctx, party.workspaceID, contact.Phone, carriermessaging.EventInbound, at.Add(-carriermessaging.FloodWindow))
	if err != nil {
		log.Printf("[carrier] flood count for %s: %v", contact.Phone, err)
		return true
	}
	if !carriermessaging.IsFlooding(count) {
		return false
	}
	if marked, err := s.Repos.Contacts.MarkFlooding(ctx, contact.ID, at); err == nil && marked {
		s.Notices.ContactFlooding(ctx, party.workspaceID, party.channel, contact, count)
	}
	return true
}
