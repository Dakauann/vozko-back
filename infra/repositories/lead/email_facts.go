package lead

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/infra/database"
)

const (
	emailFactsSQL = "SELECT l.id, l.name, COALESCE(l.email, '') AS email, l.custom_fields, (l.opted_out_at IS NOT NULL) AS opted_out, l.blocked," +
		" COALESCE(c.name, '') AS company" +
		" FROM leads l LEFT JOIN lead_companies c ON c.id = l.company_id AND c.workspace_id = l.workspace_id AND c.deleted_at IS NULL" +
		" WHERE l.workspace_id = ? AND l.id = ANY(?::uuid[]) AND l.deleted_at IS NULL"
)

type EmailFacts struct {
	db *gorm.DB
}

func NewEmailFacts(db *gorm.DB) *EmailFacts {
	return &EmailFacts{db: db}
}

type emailFactsRow struct {
	ID           string         `gorm:"column:id"`
	Name         string         `gorm:"column:name"`
	Email        string         `gorm:"column:email"`
	CustomFields datatypes.JSON `gorm:"column:custom_fields"`
	OptedOut     bool           `gorm:"column:opted_out"`
	Blocked      bool           `gorm:"column:blocked"`
	Company      string         `gorm:"column:company"`
}

func (s *EmailFacts) EmailFacts(ctx context.Context, workspaceID string, leadIDs []string) (map[string]lead.EmailFacts, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	ids := database.UUIDsOf(leadIDs)
	out := make(map[string]lead.EmailFacts, len(ids))
	for start := 0; start < len(ids); start += selection.Chunk {
		chunk := ids[start:min(start+selection.Chunk, len(ids))]
		var rows []emailFactsRow
		if err := s.db.WithContext(ctx).Raw(emailFactsSQL, workspaceID, chunk).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			facts, err := row.domain()
			if err != nil {
				return nil, err
			}
			out[row.ID] = facts
		}
	}
	return out, nil
}

func (row emailFactsRow) domain() (lead.EmailFacts, error) {
	facts := lead.EmailFacts{
		ID:       row.ID,
		Name:     row.Name,
		Email:    strings.TrimSpace(row.Email),
		Company:  row.Company,
		OptedOut: row.OptedOut,
		Blocked:  row.Blocked,
	}
	if len(row.CustomFields) > 0 && string(row.CustomFields) != "null" {
		if err := json.Unmarshal(row.CustomFields, &facts.CustomFields); err != nil {
			return lead.EmailFacts{}, err
		}
	}
	return facts, nil
}

var _ lead.EmailFactsReader = (*EmailFacts)(nil)
