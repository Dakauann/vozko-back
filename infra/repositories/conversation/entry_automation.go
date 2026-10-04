package conversation_repository

import (
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type EntryInfoReader struct {
	db *gorm.DB
}

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
	DelegateKind    string `gorm:"column:delegate_kind"`
	DelegateID      string `gorm:"column:delegate_id"`
}

func (r *EntryInfoReader) entryInfo(entryID, entryType string) (entryInfoRow, error) {
	var row entryInfoRow
	ch, ok := channelQueryFor(shared.EntryType(entryType))
	if !ok {
		return row, fmt.Errorf("entry info for %q: %w", entryType, conversation.ErrEntryTypeInvalid)
	}
	query, args := ch.entryInfoQuery(entryID)
	res := r.db.Raw(query, args...).Scan(&row)
	if res.Error != nil {
		return row, fmt.Errorf("entry info %s (%s): %w", entryID, entryType, res.Error)
	}
	if res.RowsAffected == 0 {
		return row, fmt.Errorf("entry info %s (%s): %w", entryID, entryType, conversation.ErrConversationNotFound)
	}
	return row, nil
}

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
		Delegate:              row.delegate(),
	}, nil
}

var _ conversation.EntryAutomationReader = (*EntryInfoReader)(nil)

func (r entryInfoRow) delegate() *conversation.Automation {
	a, err := conversation.NewAutomation(r.DelegateKind, r.DelegateID)
	if err != nil {
		return nil
	}
	return &a
}
