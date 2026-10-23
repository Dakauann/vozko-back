package emailwiring

import (
	"fmt"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	emailsending_repository "vozko/infra/repositories/emailsending"
	lead_repository "vozko/infra/repositories/lead"
	"vozko/usecases/emailoutreach"
	"vozko/usecases/transactionalemail"
)

type TransactionalAdapters struct {
	Gateway es.Gateway
	Billing transactionalemail.Billing
	Leads   transactionalemail.LeadFinder
	Inbox   *Inbox
}

func NewTransactional(db *gorm.DB, a TransactionalAdapters) (*transactionalemail.Service, error) {
	if db == nil || a.Inbox == nil || a.Inbox.Transcript == nil {
		return nil, fmt.Errorf("%w: every one-to-one email is recorded in the inbox, which must exist first", ErrIncomplete)
	}
	return transactionalemail.New(transactionalemail.Deps{
		Gateway:    a.Gateway,
		Billing:    a.Billing,
		Runs:       emailsending_repository.NewRunMessages(db),
		Designs:    emailcampaign_repository.NewDesignRepository(db),
		LeadFacts:  lead_repository.NewEmailFacts(db),
		Leads:      a.Leads,
		Transcript: a.Inbox.Transcript,
	})
}

type OutreachAdapters struct {
	Sender      transactionalemail.Sender
	Inbox       *Inbox
	Assignments emailoutreach.ConversationClaimer
}

func NewOutreach(db *gorm.DB, a OutreachAdapters) (*emailoutreach.StartConversation, error) {
	if db == nil || a.Inbox == nil {
		return nil, fmt.Errorf("%w: outreach starts conversations in the inbox, which must exist first", ErrIncomplete)
	}
	return emailoutreach.NewStartConversation(emailoutreach.Deps{
		Sender:        a.Sender,
		Senders:       emailsending_repository.NewSenderRepository(db),
		Inboxes:       a.Inbox.Inboxes,
		Conversations: a.Inbox.Conversations,
		Assignments:   a.Assignments,
	})
}
