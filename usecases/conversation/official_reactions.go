package conversation_usecase

import (
	"context"
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type officialReactionChannel struct {
	sender *MessageSenderService
}

func (s *MessageSenderService) OfficialReactions() conversation.MessageActionChannel {
	return officialReactionChannel{sender: s}
}

func (c officialReactionChannel) ResolveEntry(_ context.Context, entryID string) (*conversation.EntryContext, error) {
	leadID, leadNumber, businessPhoneID, err := c.sender.getEntryInfo(entryID, string(shared.EntryTypeWhatsApp))
	if err != nil {
		return nil, err
	}
	return &conversation.EntryContext{
		EntryID:    entryID,
		EntryType:  shared.EntryTypeWhatsApp,
		AccountID:  businessPhoneID,
		ContactID:  leadID,
		ContactRef: leadNumber,
	}, nil
}

func (c officialReactionChannel) SendReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID, reaction string) error {
	if ec == nil {
		return conversation.ErrNoAdapterForEntryType
	}
	if err := c.sender.checkMessageWindow(ec.ContactID, ec.AccountID); err != nil {
		return fmt.Errorf("%w: %v", conversation.ErrOutboundWindowClosed, err)
	}
	client, err := c.sender.whatsappClientFactory.ClientForPhone(ec.AccountID)
	if err != nil {
		return fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}
	reactions, ok := client.(conversation.WhatsAppReactionClient)
	if !ok {
		return fmt.Errorf("%w: this WhatsApp client cannot send reactions", conversation.ErrCapabilityUnsupported)
	}
	return reactions.SendReaction(ctx, ec.ContactRef, targetProviderMessageID, reaction)
}

func (c officialReactionChannel) RemoveReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID string) error {
	return c.SendReaction(ctx, ec, targetProviderMessageID, "")
}

var _ conversation.ReactingAdapter = officialReactionChannel{}
