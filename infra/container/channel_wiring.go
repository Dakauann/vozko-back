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

// Adapters that let any channel join the shared conversation stack.
//
// Two of the conversation services need a per-channel lookup they cannot resolve
// generically: the sender identity shown in the inbox (for channels whose
// contacts are not leads) and the conversation-status read/write. Both were
// originally added Instagram-shaped, a SetInstagram… setter and a hand-written
// Instagram adapter, so the second such channel would have meant copying both.
//
// The ports are now keyed by entry type and the adapters below are plain
// function holders, so each channel supplies only the mapping that is genuinely
// its own and nothing else is duplicated.

// contactIdentityFuncs adapts a channel's repositories onto
// conversation_usecase.ContactIdentityLookup.
type contactIdentityFuncs struct {
	byIDs           func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error)
	forConversation func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error)
	// authorsByHandle is optional: only a channel with group conversations has
	// an author distinct from the subject. Left nil elsewhere, and a nil one
	// resolves nobody, which is precisely the right answer there.
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

// conversationStatusFuncs adapts a channel's conversation repository onto
// conversation_usecase.ConversationStatusStore.
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

// entryResolver is the workspace/department lookup the campaign-owner resolver
// needs per channel. Declared here so both channels attach it by the same
// assertion instead of each inventing its own shape.
type entryResolver interface {
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
}

// buildExportEntriesUseCase assembles export with every channel's source
// registered.
//
// Export used to reject anything but WhatsApp, so a tenant whose conversations
// lived on another channel simply could not get their data out. Registering the
// listers here keeps that a container concern rather than an import of every
// channel's repositories into the export usecase.
//
// WhatsApp is registered the same way as the rest. It used to be hard-wired
// into the usecase as the one channel that could not be a lister, which is what
// made "export every campaign at once" a second code path instead of the same
// one with an empty container id.
func (c *Container) buildExportEntriesUseCase() export_domain.ExportEntriesUseCase {
	uc := export_usecase.NewExportEntriesUseCase(
		c.repositories.analysis,
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
