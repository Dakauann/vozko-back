package advertising_repository

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	ads "vozko/domain/advertising"
)

type numberDirectory struct {
	db *gorm.DB
}

func NewNumberDirectory(db *gorm.DB) ads.NumberDirectory {
	return &numberDirectory{db: db}
}

const workspaceNumbersQuery = `SELECT 'official' AS kind, COALESCE(NULLIF(verified_name, ''), display_phone_number) AS label, display_phone_number AS number
FROM whatsapp_business_phone_numbers WHERE owner_workspace_id = ? AND deleted_at IS NULL
UNION ALL
SELECT 'unofficial' AS kind, COALESCE(NULLIF(display_name, ''), NULLIF(profile_name, ''), phone_number) AS label, phone_number AS number
FROM unofficial_whatsapp_instances WHERE workspace_id = ? AND deleted_at IS NULL AND phone_number <> ''`

type numberRow struct {
	Kind   string
	Label  string
	Number string
}

func (d *numberDirectory) List(ctx context.Context, workspaceID string) ([]ads.WorkspaceNumber, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ads.ErrWorkspaceRequired
	}
	var rows []numberRow
	if err := d.db.WithContext(ctx).Raw(workspaceNumbersQuery, workspaceID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("ads: list workspace numbers: %w", err)
	}
	out := make([]ads.WorkspaceNumber, 0, len(rows))
	for _, r := range rows {
		out = append(out, ads.WorkspaceNumber{Kind: ads.NumberKind(r.Kind), Label: r.Label, Number: r.Number})
	}
	return out, nil
}
