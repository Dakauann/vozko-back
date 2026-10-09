package lead

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	"vozko/infra/database"
)

const leadFactsSQL = "SELECT id, (number IS NOT NULL AND number <> '') AS has_identity, blocked," +
	" (opted_out_at IS NOT NULL) AS opted_out, (whatsapp_opt_in_at IS NOT NULL) AS has_consent" +
	" FROM leads WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND deleted_at IS NULL"

type SendFacts struct {
	db *gorm.DB
}

func NewSendFacts(db *gorm.DB) *SendFacts {
	return &SendFacts{db: db}
}

type leadFactsRow struct {
	ID          string `gorm:"column:id"`
	HasIdentity bool   `gorm:"column:has_identity"`
	Blocked     bool   `gorm:"column:blocked"`
	OptedOut    bool   `gorm:"column:opted_out"`
	HasConsent  bool   `gorm:"column:has_consent"`
}

func (s *SendFacts) LeadFacts(ctx context.Context, workspaceID string, leadIDs []string) (map[string]campaign.LeadFacts, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	ids := database.UUIDArray(leadIDs)
	out := make(map[string]campaign.LeadFacts, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []leadFactsRow
	if err := s.db.WithContext(ctx).Raw(leadFactsSQL, workspaceID, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = campaign.LeadFacts{Found: true, HasIdentity: row.HasIdentity, Blocked: row.Blocked, OptedOut: row.OptedOut, HasConsent: row.HasConsent}
	}
	return out, nil
}
