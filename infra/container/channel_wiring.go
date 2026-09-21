package container

import (
	"context"
	"time"

	export_domain "vozko/domain/export"
	instagram_repository "vozko/infra/repositories/instagram"
	telegram_repository "vozko/infra/repositories/telegram"
	unofficial_whatsapp_repository "vozko/infra/repositories/unofficial_whatsapp"
	wc_entry_repository "vozko/infra/repositories/whatsapp_campaign_entry"
	conversation_usecase "vozko/usecases/conversation"
	export_usecase "vozko/usecases/export"
)

type contactIdentityFuncs struct {
	byIDs           func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error)
	forConversation func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error)
	authorsByHandle func(ctx context.Context, entryID string, handles []string) (map[string]conversation_usecase.ContactDisplay, error)
}

func (f contactIdentityFuncs) ContactsByIDs(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
	if f.byIDs == nil {
		return nil, nil
	}
	return f.byIDs(ctx, ids)
}

func (f contactIdentityFuncs) ContactForConversation(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error) {
	if f.forConversation == nil {
		return conversation_usecase.ContactDisplay{}, "", nil
	}
	return f.forConversation(ctx, conversationID)
}

func (f contactIdentityFuncs) AuthorsByHandle(ctx context.Context, entryID string, handles []string) (map[string]conversation_usecase.ContactDisplay, error) {
	if f.authorsByHandle == nil {
		return nil, nil
	}
	return f.authorsByHandle(ctx, entryID, handles)
}

var _ conversation_usecase.ContactIdentityLookup = contactIdentityFuncs{}

type conversationStatusFuncs struct {
	status func(ctx context.Context, entryID string) (string, error)
	set    func(ctx context.Context, entryID, status, closeSource, closeReason string, closedAt *time.Time) error
}

func (f conversationStatusFuncs) Status(ctx context.Context, entryID string) (string, error) {
	if f.status == nil {
		return "", nil
	}
	return f.status(ctx, entryID)
}

func (f conversationStatusFuncs) SetStatus(ctx context.Context, entryID, status, closeSource, closeReason string, closedAt *time.Time) error {
	if f.set == nil {
		return nil
	}
	return f.set(ctx, entryID, status, closeSource, closeReason, closedAt)
}

var _ conversation_usecase.ConversationStatusStore = conversationStatusFuncs{}

type entryResolver interface {
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
}

func (c *Container) buildExportEntriesUseCase() export_domain.ExportEntriesUseCase {
	uc := export_usecase.NewExportEntriesUseCase(
		c.repositories.conversationAnalyses,
		c.repositories.stage,
	)

	setter, ok := uc.(interface {
		SetChannelEntryLister(export_domain.EntryType, export_domain.ChannelEntryLister)
	})
	if !ok {
		return uc
	}
	setter.SetChannelEntryLister(export_domain.EntryTypeWhatsApp,
		wc_entry_repository.NewExportRepository(c.db))
	if c.instagram != nil && c.instagram.Enabled {
		setter.SetChannelEntryLister(export_domain.EntryTypeInstagram, instagram_repository.NewExportRepository(c.db))
	}
	if c.telegram != nil && c.telegram.Enabled {
		setter.SetChannelEntryLister(export_domain.EntryTypeTelegram, telegram_repository.NewExportRepository(c.db))
	}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		setter.SetChannelEntryLister(export_domain.EntryTypeUnofficialWhatsApp,
			unofficial_whatsapp_repository.NewExportRepository(c.db))
	}
	return uc
}
