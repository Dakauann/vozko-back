package container

import (
	"log"

	emailoutreachhttp "vozko/delivery/http/emailoutreach"
	"vozko/domain/copilot"
	"vozko/domain/readiness"
	"vozko/domain/workspace"
	"vozko/infra/emailwiring"
	emailsending_repository "vozko/infra/repositories/emailsending"
	"vozko/usecases/copilot/copilottools/emaildesigntools"
	"vozko/usecases/copilot/copilottools/emailtools"
	readiness_usecase "vozko/usecases/readiness"
	"vozko/usecases/transactionalemail"
)

func (c *Container) buildTransactionalEmail(email *emailBundle, inbox *emailInboxBundle) *transactionalemail.Service {
	if email == nil || email.Sending == nil || email.Sending.Module == nil || email.Sending.Module.Gateway == nil || email.Billing == nil {
		log.Fatalf("[transactional-email] email sending and billing must be built first")
	}
	if inbox == nil || inbox.Transcript == nil {
		log.Fatalf("[transactional-email] the email inbox must be built first, every one-to-one email is recorded in its conversation")
	}
	if c.db == nil || c.repositories == nil || c.repositories.lead == nil {
		log.Fatalf("[transactional-email] the database and the lead repository must exist first")
	}
	service, err := emailwiring.NewTransactional(c.db, emailwiring.TransactionalAdapters{
		Gateway: email.Sending.Module.Gateway,
		Billing: email.Billing,
		Leads:   c.repositories.lead,
		Inbox:   inbox.Inbox,
	})
	if err != nil {
		log.Fatalf("[transactional-email] %v", err)
	}
	return service
}

func (c *Container) bindTransactionalEmail(relay *transactionalemail.Relay, email *emailBundle, inbox *emailInboxBundle) {
	if relay == nil {
		log.Fatalf("[transactional-email] the relay shared by the workflow node and the agent tool was not built")
	}
	relay.Bind(c.buildTransactionalEmail(email, inbox))
	email.Outreach = c.buildEmailOutreach(email, inbox, relay)
}

func (c *Container) buildEmailOutreach(email *emailBundle, inbox *emailInboxBundle, sender transactionalemail.Sender) *emailoutreachhttp.Handler {
	if email.Prices == nil || c.services.assignmentService == nil || c.services.conversationAuthImpl == nil {
		log.Fatalf("[email-outreach] the email prices, assignments and the department scope must exist first")
	}
	start, err := emailwiring.NewOutreach(c.db, emailwiring.OutreachAdapters{
		Sender:      sender,
		Inbox:       inbox.Inbox,
		Assignments: c.services.assignmentService,
	})
	if err != nil {
		log.Fatalf("[email-outreach] %v", err)
	}
	return emailoutreachhttp.NewHandler(emailoutreachhttp.HandlerDeps{
		Start:       start,
		Prices:      email.Prices,
		Departments: c.services.conversationAuthImpl,
	})
}

func emailCopilotTools(email *emailBundle, sender transactionalemail.Sender) []copilot.Tool {
	if email == nil || email.Campaigns == nil || email.Campaigns.Campaigns == nil || email.Billing == nil || sender == nil {
		log.Fatalf("[transactional-email] the Elo email tools need the campaigns, the billing and the sender")
	}
	return emailtools.EmailTools(emailtools.EmailDeps{Sender: sender, Campaigns: email.Campaigns.Campaigns, Quotes: email.Billing})
}

func (c *Container) emailDesignTools() []copilot.Tool {
	if c.email == nil || c.email.Campaigns == nil || c.email.Campaigns.Campaigns == nil {
		log.Fatalf("[email-design] the Elo email design tools need the email campaigns")
	}
	return emaildesigntools.EmailDesignTools(emaildesigntools.Deps{
		Designs: c.email.Campaigns.Campaigns,
		Library: c.useCases.getMedia,
		Files:   c.services.fileReader,
		Media:   c.mediaGeneration().Service,
	})
}

func (c *Container) emailReadinessProbe(access workspace.CheckAccessUseCase) readiness.Probe {
	return readiness_usecase.NewEmailProbe(readiness_usecase.EmailProbeDeps{
		Accounts: emailsending_repository.NewAccountRepository(c.db),
		Senders:  emailsending_repository.NewSenderRepository(c.db),
		Domains:  emailsending_repository.NewDomainRepository(c.db),
		Access:   access,
	})
}
