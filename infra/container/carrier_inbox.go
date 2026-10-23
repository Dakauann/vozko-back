package container

import (
	"context"
	"log"

	"vozko/domain/carriermessaging"
	export_domain "vozko/domain/export"
	"vozko/domain/shared"
	carriermessaging_repository "vozko/infra/repositories/carriermessaging"
	conversation_usecase "vozko/usecases/conversation"
)

type carrierAnalysisSources struct {
	Conversations carriermessaging.ConversationRepository
	Contacts      carriermessaging.ContactRepository
	Senders       carriermessaging.SenderRepository
	Routes        carriermessaging.RouteRepository
}

func (c *Container) wireCarrierConversationStack() {
	b := c.carrier
	if b == nil || b.Service == nil || b.RCSConversations == nil || b.SMSConversations == nil || b.Contacts == nil {
		log.Fatalf("[carrier] the conversation stack needs RCS and SMS built first")
	}
	if c.services.conversationAutomation == nil || c.services.conversationAuthImpl == nil || c.services.conversationStatusService == nil {
		log.Fatalf("[carrier] conversation services must be built before the RCS and SMS conversation stack")
	}
	automationReader, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	})
	if !ok {
		log.Fatalf("[carrier] history provider exposes no SetAutomationReader")
	}
	owners, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	})
	if !ok {
		log.Fatalf("[carrier] workspace resolver exposes no SetEntryOwnerResolver")
	}
	identities, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	})
	if !ok {
		log.Fatalf("[carrier] history provider exposes no SetContactIdentityLookup")
	}
	for _, channel := range carriermessaging.AllChannels() {
		entryType := channel.EntryType()
		conversations := b.conversations(channel)

		c.registerChannelAdapter(b.Service.Adapter(channel))
		automationReader.SetAutomationReader(entryType, func(ctx context.Context, entryID string) (*bool, error) {
			conv, err := conversations.FindByID(ctx, entryID)
			if err != nil {
				return nil, err
			}
			return conv.AutomationEnabled, nil
		})
		c.services.conversationAutomation.Register(entryType, conversations.SetAutomationEnabled)
		c.services.conversationAuthImpl.SetEntryAccessRepo(entryType, conversations)
		c.services.conversationStatusService.SetConversationStatusStore(entryType,
			conversationStatusFuncs{status: conversations.StatusForEntry, set: conversations.SetStatus})
		c.services.conversationStatusService.SetConversationCounter(entryType, conversations.CountByStatus)
		owners.SetEntryOwnerResolver(entryType, conversations)
		identities.SetContactIdentityLookup(entryType, carrierContactIdentity(b.Contacts, conversations))
	}
}

func (b *carrierBundle) conversations(channel carriermessaging.Channel) *carriermessaging_repository.ConversationRepository {
	if channel == carriermessaging.ChannelSMS {
		return b.SMSConversations
	}
	return b.RCSConversations
}

func carrierContactDisplay(ct *carriermessaging.Contact) conversation_usecase.ContactDisplay {
	out := conversation_usecase.ContactDisplay{ContactID: ct.ID, Ref: ct.Phone, Handle: ct.Phone, Name: ct.Name()}
	if ct.LeadID != nil {
		out.LeadID = *ct.LeadID
	}
	return out
}

func carrierContactIdentity(contacts carriermessaging.ContactRepository, conversations carriermessaging.ConversationRepository) conversation_usecase.ContactIdentityLookup {
	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := contacts.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, ct := range found {
				if ct != nil {
					out[ct.ID] = carrierContactDisplay(ct)
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
			return carrierContactDisplay(ct), conv.WorkspaceID, nil
		},
	}
}

func (s *carrierAnalysisSources) container(ctx context.Context, entryType shared.EntryType, senderID string) (*analysisContainer, error) {
	if entryType == shared.EntryTypeSMS {
		route, err := s.Routes.FindByIDUnscoped(ctx, senderID)
		if err != nil || route == nil {
			return nil, err
		}
		return &analysisContainer{ID: route.ID, Name: route.BrandPrefix, Automation: route.Automation}, nil
	}
	sender, err := s.Senders.FindByIDUnscoped(ctx, senderID)
	if err != nil || sender == nil {
		return nil, err
	}
	return &analysisContainer{ID: sender.ID, Name: sender.BrandName, Automation: sender.Automation}, nil
}

func carrierAnalysisResolver(entryType shared.EntryType, s *carrierAnalysisSources) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(entryType,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := s.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			container, err := s.container(ctx, entryType, conv.SenderID)
			if err != nil || container == nil {
				return nil, err
			}
			return &analysisConversation{ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID, Container: *container}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			ct, err := s.Contacts.FindByID(ctx, contactID)
			if err != nil || ct == nil {
				return nil, err
			}
			return &analysisContact{Label: ct.Name(), LeadID: ct.LeadID}, nil
		})
}

func (c *Container) carrierAnalysisChannels() []analysisChannel {
	b := c.carrier
	if b == nil {
		log.Fatalf("[carrier] conversation analysis was built before RCS and SMS")
	}
	out := make([]analysisChannel, 0, len(carriermessaging.AllChannels()))
	for _, channel := range carriermessaging.AllChannels() {
		sources := &carrierAnalysisSources{Conversations: b.conversations(channel), Contacts: b.Contacts, Senders: b.Senders, Routes: b.Routes}
		out = append(out, analysisChannel{channel.EntryType(), carrierAnalysisResolver(channel.EntryType(), sources)})
	}
	return out
}

func (c *Container) carrierExportListers() map[export_domain.EntryType]export_domain.ChannelEntryLister {
	return map[export_domain.EntryType]export_domain.ChannelEntryLister{
		export_domain.EntryTypeRCS: carriermessaging_repository.NewExportRepository(c.db, carriermessaging.ChannelRCS),
		export_domain.EntryTypeSMS: carriermessaging_repository.NewExportRepository(c.db, carriermessaging.ChannelSMS),
	}
}
