package whatsapp_campaign_entry

import (
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/infra/database"
)

const entryPlacementsSQL = "SELECT whatsapp_campaign_entries.id::text AS entry_id, whatsapp_campaigns.workspace_id::text AS workspace_id," +
	" COALESCE(whatsapp_campaigns.department_id::text, '') AS department_id FROM whatsapp_campaign_entries" +
	" JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id AND whatsapp_campaigns.deleted_at IS NULL" +
	" WHERE whatsapp_campaign_entries.id = ANY(?::uuid[]) AND whatsapp_campaign_entries.deleted_at IS NULL"

type PlacementDirectory struct {
	db *gorm.DB
}

func NewPlacementDirectory(db *gorm.DB) *PlacementDirectory {
	return &PlacementDirectory{db: db}
}

type placementRow struct {
	EntryID      string
	WorkspaceID  string
	DepartmentID string
}

func (d *PlacementDirectory) EntryPlacements(entryIDs []string) (map[string]conversation.EntryPlacement, error) {
	ids := database.UUIDArray(entryIDs)
	placements := make(map[string]conversation.EntryPlacement, len(ids))
	if len(ids) == 0 {
		return placements, nil
	}
	var rows []placementRow
	if err := d.db.Raw(entryPlacementsSQL, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		placements[row.EntryID] = conversation.EntryPlacement{WorkspaceID: row.WorkspaceID, DepartmentID: row.DepartmentID}
	}
	return placements, nil
}
