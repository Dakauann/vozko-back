package leadexit_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/leadexit"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type UnofficialCampaignEntries interface {
	FindLatestByConversationID(conversationID string) (*uwc.Entry, error)
}

type StoredMessagesByID interface {
	GetByID(id string) (*conversation.Message, error)
}

type UnofficialCampaignMessagesDeps struct {
	Entries  UnofficialCampaignEntries
	Messages StoredMessagesByID
}

type UnofficialCampaignMessages struct {
	deps UnofficialCampaignMessagesDeps
}

var _ CampaignMessages = (*UnofficialCampaignMessages)(nil)

func NewUnofficialCampaignMessages(deps UnofficialCampaignMessagesDeps) (*UnofficialCampaignMessages, error) {
	if deps.Entries == nil || deps.Messages == nil {
		return nil, leadexit.ErrUnavailable
	}
	return &UnofficialCampaignMessages{deps: deps}, nil
}

func (r *UnofficialCampaignMessages) LastCampaignMessage(_ context.Context, entryType, entryID string) (*leadexit.SentCampaignMessage, error) {
	if shared.EntryType(entryType) != shared.EntryTypeUnofficialWhatsApp {
		return nil, nil
	}
	entry, err := r.deps.Entries.FindLatestByConversationID(entryID)
	if errors.Is(err, uwc.ErrEntryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest campaign entry of %s: %w", entryID, err)
	}
	shownID := strings.TrimSpace(entry.ShownMessageID())
	if entry.SentAt == nil || shownID == "" {
		return nil, nil
	}
	stored, err := r.deps.Messages.GetByID(shownID)
	if errors.Is(err, conversation.ErrMessageNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("campaign message %s: %w", shownID, err)
	}
	sent, ok := leadexit.CampaignMessageOf(stored)
	if !ok || sent.CampaignID != entry.CampaignID || stored.EntryID != entryID {
		return nil, nil
	}
	return &sent, nil
}
