package calllist_repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/calls/calllist"
	"vozko/domain/shared"
	"vozko/infra/database"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

var latestInteractionSQL = "SELECT lead_entries.entry_id::text AS entry_id, lead_entries.entry_type::text AS entry_type, latest.at" +
	" FROM " + infracrmfilter.LeadEntriesSource() +
	" CROSS JOIN LATERAL (SELECT m.created_at AS at FROM conversation_messages m WHERE m.entry_id = lead_entries.entry_id" +
	" AND m.entry_type = lead_entries.entry_type AND m.deleted_at IS NULL AND " + database.RealMessageSQL("m") +
	" ORDER BY m.created_at DESC LIMIT 1) latest" +
	" WHERE lead_entries.lead_id = ? ORDER BY latest.at DESC LIMIT 1"

type Interactions struct {
	db *gorm.DB
}

func NewInteractions(db *gorm.DB) *Interactions {
	return &Interactions{db: db}
}

type interactionRow struct {
	EntryID   string
	EntryType string
	At        time.Time
}

func (i *Interactions) Latest(ctx context.Context, workspaceID, leadID string) (*calllist.LastInteraction, error) {
	if !validRef(workspaceID, leadID) {
		return nil, nil
	}
	var rows []interactionRow
	if err := i.db.WithContext(ctx).Raw(latestInteractionSQL, leadID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("latest interaction of lead %s: %w", leadID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &calllist.LastInteraction{EntryID: rows[0].EntryID, EntryType: shared.EntryType(rows[0].EntryType), At: rows[0].At}, nil
}
