package container

import (
	"context"
	"log"

	emailinboxhttp "vozko/delivery/http/emailinbox"
	conversation_domain "vozko/domain/conversation"
	emdomain "vozko/domain/emailinbox"
	export_domain "vozko/domain/export"
	"vozko/domain/shared"
	"vozko/infra/emailwiring"
	emailinbox_repository "vozko/infra/repositories/emailinbox"
	emailsending_repository "vozko/infra/repositories/emailsending"
	conversation_usecase "vozko/usecases/conversation"
	ce_usecase "vozko/usecases/conversation_event"
	emuc "vozko/usecases/emailinbox"
	"vozko/usecases/shared/automationrefs"
)

type emailInboxBundle struct {
	*emailwiring.Inbox
	Handler *emailinboxhttp.Handler
}

func (c *Container) initEmailInbox(history conversation_domain.MessageHistoryManager) *emailInboxBundle {
	email := c.email
	if email == nil || email.Sending == nil || email.Sending.Module == nil || email.Campaigns == nil || email.Campaigns.Consent == nil || email.Campaigns.Campaigns == nil || email.Billing == nil {
		log.Fatalf("[emailinbox] email sending, consent and billing must be built before the email inbox")
	}
	if history == nil || c.services.assignmentService == nil || c.services.conversationHub == nil {
		log.Fatalf("[emailinbox] the history manager, assignments and the conversation hub must exist before the email inbox")
	}
	automation := conversation_usecase.NewInboundAutomation(
		c.useCases.triggerEvaluator,
		c.mustChannelAIReply(),
		c.services.analysisScheduler,
		c.repositories.conversation,
	).WithDelegations(c.services.delegations)
	inbox, err := emailwiring.NewInbox(c.db, emailwiring.InboxAdapters{
		Sending:     email.Sending.Module,
		Raw:         email.Sending.Raw,
		Codec:       email.Sending.Codec,
		Consent:     email.Campaigns.Consent,
		Billing:     email.Billing,
		Leads:       c.repositories.lead,
		History:     history,
		Messages:    c.repositories.conversation,
		Media:       c.mediaStore(),
		Assignments: c.services.assignmentService,
		Automation:  automation,
		Claims:      c.processedWebhookEvents(),
		Events:      ce_usecase.NewLogger(c.services.crmTelemetryPublisher),
		Announcer:   c.services.conversationHub,
		Originals:   email.Campaigns.Campaigns,
		Files:       mediaDownloads(),
		EmailsSeen:  c.emailHygiene().Service,
	})
	if err != nil {
		log.Fatalf("[emailinbox] %v", err)
	}
	return &emailInboxBundle{
		Inbox: inbox,
		Handler: emailinboxhttp.NewHandler(emuc.NewInboxes(inbox.Inboxes, emailsending_repository.NewSenderRepository(c.db), emailsending_repository.NewDomainRepository(c.db), automationrefs.Finders{
			Agents:      c.repositories.agent,
			Workflows:   c.repositories.workflow,
			Pipelines:   c.repositories.pipeline,
			Departments: c.repositories.workspaceDepartment,
		})),
	}
}

func (c *Container) wireEmailInboxConversationStack(b *emailInboxBundle) {
	if b == nil || b.Adapter == nil {
		log.Fatalf("[emailinbox] the conversation stack needs the inbox bundle")
	}
	if c.services.conversationAutomation == nil || c.services.conversationAuthImpl == nil || c.services.conversationStatusService == nil {
		log.Fatalf("[emailinbox] conversation services must be built before the email conversation stack")
	}
	conversations := b.Conversations

	c.registerChannelAdapter(b.Adapter)

	automationReader, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	})
	if !ok {
		log.Fatalf("[emailinbox] history provider exposes no SetAutomationReader")
	}
	automationReader.SetAutomationReader(shared.EntryTypeEmail, func(ctx context.Context, entryID string) (*bool, error) {
		conv, err := conversations.FindByID(ctx, entryID)
		if err != nil {
			return nil, err
		}
		return conv.AutomationEnabled, nil
	})
	c.services.conversationAutomation.Register(shared.EntryTypeEmail, conversations.SetAutomationEnabled)
	c.services.conversationAuthImpl.SetEntryAccessRepo(shared.EntryTypeEmail, conversations)
	c.services.conversationStatusService.SetConversationStatusStore(shared.EntryTypeEmail,
		conversationStatusFuncs{status: conversations.StatusForEntry, set: conversations.SetStatus})
	c.services.conversationStatusService.SetConversationCounter(shared.EntryTypeEmail, conversations.CountByStatus)

	owners, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	})
	if !ok {
		log.Fatalf("[emailinbox] workspace resolver exposes no SetEntryOwnerResolver")
	}
	owners.SetEntryOwnerResolver(shared.EntryTypeEmail, conversations)

	identities, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	})
	if !ok {
		log.Fatalf("[emailinbox] history provider exposes no SetContactIdentityLookup")
	}
	identities.SetContactIdentityLookup(shared.EntryTypeEmail, emailContactIdentity(b.Contacts, b.Conversations))
}

func emailContactDisplay(ct *emdomain.Contact) conversation_usecase.ContactDisplay {
	out := conversation_usecase.ContactDisplay{
		ContactID: ct.ID,
		Ref:       string(ct.Address),
		Handle:    string(ct.Address),
		Name:      ct.Name(),
	}
	if ct.LeadID != nil {
		out.LeadID = *ct.LeadID
	}
	return out
}

func emailContactIdentity(contacts emdomain.ContactRepository, conversations emdomain.ConversationRepository) conversation_usecase.ContactIdentityLookup {
	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := contacts.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, ct := range found {
				if ct != nil {
					out[ct.ID] = emailContactDisplay(ct)
				}
			}
			return out, nil
		},
		forConversation: func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error) {
			conv, err := conversations.FindByID(ctx, conversationID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, "", err
			}
			ct, err := contacts.FindByID(ctx, conv.ContactID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, conv.WorkspaceID, err
			}
			return emailContactDisplay(ct), conv.WorkspaceID, nil
		},
	}
}

func emailInboxAnalysisChannel(b *emailInboxBundle) analysisChannel {
	if b == nil {
		log.Fatalf("[emailinbox] conversation analysis was built before the email inbox")
	}
	return analysisChannel{shared.EntryTypeEmail, emailAnalysisResolver(b)}
}

func emailAnalysisResolver(b *emailInboxBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeEmail,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := b.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			inbox, err := b.Inboxes.FindByIDUnscoped(ctx, conv.SenderID)
			if err != nil || inbox == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID,
				Container: analysisContainer{ID: inbox.ID, Name: inbox.Label(), Automation: inbox.Automation()},
			}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			ct, err := b.Contacts.FindByID(ctx, contactID)
			if err != nil || ct == nil {
				return nil, err
			}
			return &analysisContact{Label: ct.Name(), LeadID: ct.LeadID}, nil
		})
}

func (c *Container) emailInboxExportLister() export_domain.ChannelEntryLister {
	return emailinbox_repository.NewExportRepository(c.db)
}

func emailInboxHandler(b *emailInboxBundle) *emailinboxhttp.Handler {
	if b == nil || b.Handler == nil {
		log.Fatalf("[emailinbox] routes requested before the email inbox was built")
	}
	return b.Handler
}
