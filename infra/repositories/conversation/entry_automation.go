package conversation_repository

import (
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// EntryInfoReader reads what the roulette needs about one conversation through
// the same per-channel entry query the inbox broadcast uses: its automation
// settings (so the roulette and the inbox ai_handler chip read one source) and
// the channel account that keys the roulette pointer.
type EntryInfoReader struct {
	db *gorm.DB
}

// NewEntryAutomationReader builds the reader.
func NewEntryAutomationReader(db *gorm.DB) *EntryInfoReader {
	return &EntryInfoReader{db: db}
}

type entryInfoRow struct {
	AccountID       string `gorm:"column:business_phone_id"`
	AgentID         string `gorm:"column:agent_id"`
	WorkflowID      string `gorm:"column:workflow_id"`
	AgentEnabled    bool   `gorm:"column:agent_responses_enabled"`
	WorkflowEnabled bool   `gorm:"column:workflow_enabled"`
	AutomationOn    *bool  `gorm:"column:automation_enabled"`
}

// entryInfo reads one conversation through its channel's entry query. Every
// way the read can fail is an error, never a zero row.
func (r *EntryInfoReader) entryInfo(entryID, entryType string) (entryInfoRow, error) {
	var row entryInfoRow
	ch, ok := channelQueryFor(shared.EntryType(entryType))
	if !ok {
		return row, fmt.Errorf("entry info for %q: %w", entryType, conversation.ErrEntryTypeInvalid)
	}
	res := r.db.Raw(ch.entryInfoSQL(), entryID).Scan(&row)
	if res.Error != nil {
		return row, fmt.Errorf("entry info %s (%s): %w", entryID, entryType, res.Error)
	}
	if res.RowsAffected == 0 {
		return row, fmt.Errorf("entry info %s (%s): %w", entryID, entryType, conversation.ErrConversationNotFound)
	}
	return row, nil
}

// EntryAccountID is the channel account the conversation came in on (the
// business phone on WhatsApp), which keys the roulette pointer.
func (r *EntryInfoReader) EntryAccountID(entryID, entryType string) (string, error) {
	row, err := r.entryInfo(entryID, entryType)
	if err != nil {
		return "", err
	}
	return row.AccountID, nil
}

func (r *EntryInfoReader) EntryAutomation(entryID, entryType string) (conversation.AutomationProfile, error) {
	row, err := r.entryInfo(entryID, entryType)
	if err != nil {
		return conversation.AutomationProfile{}, err
	}

	return conversation.AutomationProfile{
		AgentID:               row.AgentID,
		AgentResponsesEnabled: row.AgentEnabled,
		WorkflowID:            row.WorkflowID,
		WorkflowEnabled:       row.WorkflowEnabled,
		AutomationEnabled:     row.AutomationOn,
	}, nil
}

var _ conversation.EntryAutomationReader = (*EntryInfoReader)(nil)
