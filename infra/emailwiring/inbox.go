package emailwiring

import (
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	"vozko/domain/emailhygiene"
	em "vozko/domain/emailinbox"
	"vozko/domain/media"
	"vozko/infra/emailmime"
	emailinbox_repository "vozko/infra/repositories/emailinbox"
	emailsending_repository "vozko/infra/repositories/emailsending"
	"vozko/infra/ses"
	emuc "vozko/usecases/emailinbox"
	esuc "vozko/usecases/emailsending"
	"vozko/usecases/messagestate"
)

type InboxBilling interface {
	emuc.MessageBilling
	emuc.ReplyBilling
	emuc.ReceiveBilling
}

type InboxAdapters struct {
	Sending     *esuc.Module
	Raw         emuc.RawMessages
	Codec       emuc.AddressCodec
	Consent     emuc.MailtoUnsubscriber
	Billing     InboxBilling
	Leads       emuc.LeadFinder
	History     conversation.MessageHistoryManager
	Messages    emuc.MessageStore
	Media       conversation.MediaStore
	Assignments emuc.AssignmentService
	Automation  emuc.InboundDispatcher
	Claims      emuc.InboundClaims
	Events      conv_event.Logger
	Announcer   messagestate.Announcer
	Originals   emuc.SentOriginals
	Files       media.RemoteFiles
	EmailsSeen  emailhygiene.AddressSeen
}

type Inbox struct {
	Inboxes       em.InboxRepository
	Contacts      em.ContactRepository
	Conversations em.ConversationRepository
	Inbound       *emuc.Inbound
	Adapter       *emuc.ChannelAdapter
	Observer      *emuc.DeliveryObserver
	Transcript    *emuc.OutboundTranscript
}

func NewInbox(db *gorm.DB, a InboxAdapters) (*Inbox, error) {
	if db == nil || a.Sending == nil || a.Sending.Events == nil || a.Sending.Gateway == nil || a.Sending.Replies == nil {
		return nil, fmt.Errorf("%w: the database and email sending must exist before the email inbox", ErrIncomplete)
	}
	if a.Files == nil {
		return nil, fmt.Errorf("%w: reply attachments need the guarded media downloads", ErrIncomplete)
	}
	if a.EmailsSeen == nil {
		return nil, fmt.Errorf("%w: inbound senders must start their domain checks", ErrIncomplete)
	}
	senders := emailsending_repository.NewSenderRepository(db)
	domains := emailsending_repository.NewDomainRepository(db)
	b := &Inbox{
		Inboxes:       emailinbox_repository.NewInboxRepository(db),
		Contacts:      emailinbox_repository.NewContactRepository(db),
		Conversations: emailinbox_repository.NewConversationRepository(db),
	}
	b.Transcript = emuc.NewOutboundTranscript(emuc.OutboundTranscriptDeps{
		Contacts:      b.Contacts,
		Conversations: b.Conversations,
		Inboxes:       b.Inboxes,
		Senders:       senders,
		Domains:       domains,
		History:       a.History,
		Messages:      a.Messages,
		Media:         a.Media,
	})
	b.Inbound = emuc.NewInbound(emuc.InboundDeps{
		Raw:           a.Raw,
		Parser:        emailmime.NewParser(),
		Codec:         a.Codec,
		Sent:          emailsending_repository.NewMessageRepository(db),
		ReplyDomains:  domains,
		Senders:       senders,
		Domains:       domains,
		Unsubscriber:  a.Consent,
		Inboxes:       b.Inboxes,
		Contacts:      b.Contacts,
		Conversations: b.Conversations,
		Leads:         a.Leads,
		History:       a.History,
		Messages:      a.Messages,
		Media:         a.Media,
		Assignments:   a.Assignments,
		Automation:    a.Automation,
		Claims:        a.Claims,
		Forwarder:     emuc.NewForwarder(senders, a.Billing, a.Sending.Gateway, a.Events),
		SenderIP:      ses.ReceivedHeaders{},
		Guard:         a.Sending.Replies,
		Receipts:      a.Billing,
		Originals:     a.Originals,
		Transcript:    b.Transcript,
		Engagement:    NewReputation(db),
		EmailsSeen:    a.EmailsSeen,
	})
	b.Adapter = emuc.NewChannelAdapter(emuc.AdapterDeps{
		Conversations: b.Conversations,
		Contacts:      b.Contacts,
		Accounts:      emailsending_repository.NewAccountRepository(db),
		Suppressions:  emailsending_repository.NewSuppressionRepository(db),
		Billing:       a.Billing,
		Gateway:       a.Sending.Gateway,
		Files:         a.Files,
		Media:         a.Media,
	})
	b.Observer = emuc.NewDeliveryObserver(emuc.DeliveryObserverDeps{
		Messages:      a.Messages,
		Contacts:      b.Contacts,
		Conversations: b.Conversations,
		Announcer:     a.Announcer,
	})
	a.Sending.Events.SetInboundHandler(b.Inbound)
	a.Sending.Events.AddObserver(b.Observer)
	return b, nil
}
