package container

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
)

type analysisContainer struct {
	ID         string
	Name       string
	Automation conversation.ChannelAutomation
}

type analysisConversation struct {
	ID          string
	WorkspaceID string
	ContactID   string
	Container   analysisContainer
}

type analysisContact struct {
	Label  string
	LeadID *string
}

func channelAnalysisResolver(
	entryType shared.EntryType,
	findConversation func(ctx context.Context, entryID string) (*analysisConversation, error),
	findContact func(ctx context.Context, contactID string) (*analysisContact, error),
) conversation_usecase.AnalysisSubjectResolver {
	return func(ctx context.Context, entryID string) (*conversation_usecase.AnalysisSubject, error) {
		conv, err := findConversation(ctx, entryID)
		if err != nil || conv == nil {
			return nil, err
		}

		label := conv.Container.Name
		var leadID string
		if contact, err := findContact(ctx, conv.ContactID); err == nil && contact != nil {
			label = contact.Label
			leadID = derefID(contact.LeadID)
		}

		automation := conv.Container.Automation
		return &conversation_usecase.AnalysisSubject{
			EntryID:           conv.ID,
			EntryType:         entryType,
			WorkspaceID:       conv.WorkspaceID,
			ContainerID:       conv.Container.ID,
			ContainerName:     conv.Container.Name,
			ContactLabel:      label,
			LeadID:            leadID,
			AgentID:           derefID(automation.AgentID),
			EnableAnalysis:    automation.EnableAnalysis,
			EnableAutoStaging: automation.EnableAutoStaging,
			EnableAutoMemory:  automation.EnableAutoMemory,
		}, nil
	}
}

func unofficialWhatsAppAnalysisResolver(bundle *unofficialWhatsAppBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeUnofficialWhatsApp,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := bundle.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			instance, err := bundle.Instances.FindByID(ctx, conv.InstanceID)
			if err != nil || instance == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID,
				Container: analysisContainer{ID: instance.ID, Name: instance.Label(), Automation: conversation.ChannelAutomation{
					AgentID:           instance.AgentID,
					EnableAnalysis:    instance.EnableAnalysis,
					EnableAutoStaging: instance.EnableAutoStaging,
					EnableAutoMemory:  instance.EnableAutoMemory,
				}},
			}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			contact, err := bundle.Contacts.FindByID(ctx, contactID)
			if err != nil || contact == nil {
				return nil, err
			}
			return &analysisContact{Label: contact.DisplayName(), LeadID: contact.LeadID}, nil
		})
}

func instagramAnalysisResolver(bundle *instagramBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeInstagram,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := bundle.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			account, err := bundle.Accounts.FindByID(ctx, conv.IGAccountID)
			if err != nil || account == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID,
				Container: analysisContainer{ID: account.ID, Name: "@" + account.Username, Automation: account.Automation()},
			}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			contact, err := bundle.Contacts.FindByID(ctx, contactID)
			if err != nil || contact == nil {
				return nil, err
			}
			return &analysisContact{Label: contact.DisplayName(), LeadID: contact.LeadID}, nil
		})
}

func telegramAnalysisResolver(bundle *telegramBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeTelegram,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := bundle.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			account, err := bundle.Accounts.FindByID(ctx, conv.AccountID)
			if err != nil || account == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID,
				Container: analysisContainer{ID: account.ID, Name: account.DisplayName(), Automation: account.Automation()},
			}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			contact, err := bundle.Contacts.FindByID(ctx, contactID)
			if err != nil || contact == nil {
				return nil, err
			}
			return &analysisContact{Label: contact.DisplayName(), LeadID: contact.LeadID}, nil
		})
}

func facebookAnalysisResolver(bundle *facebookBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeFacebook,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := bundle.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			page, err := bundle.Pages.FindByID(ctx, conv.PageID)
			if err != nil || page == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID,
				Container: analysisContainer{ID: page.ID, Name: page.Name, Automation: page.Automation()},
			}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			contact, err := bundle.Contacts.FindByID(ctx, contactID)
			if err != nil || contact == nil {
				return nil, err
			}
			return &analysisContact{Label: contact.DisplayName(), LeadID: contact.LeadID}, nil
		})
}
