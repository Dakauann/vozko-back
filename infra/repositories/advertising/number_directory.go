package advertising_repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	ads "vozko/domain/advertising"
)

type numberDirectory struct {
	db  *gorm.DB
	now func() time.Time
}

func NewNumberDirectory(db *gorm.DB) ads.NumberDirectory {
	return &numberDirectory{db: db, now: func() time.Time { return time.Now().UTC() }}
}

const workspaceNumbersQuery = `WITH numbers AS (
	SELECT 'official' AS kind, COALESCE(NULLIF(verified_name, ''), display_phone_number) AS label, display_phone_number AS number, COALESCE(business_portfolio_id, '') AS portfolio_id
	FROM whatsapp_business_phone_numbers WHERE owner_workspace_id = ? AND deleted_at IS NULL
	UNION ALL
	SELECT 'unofficial' AS kind, COALESCE(NULLIF(display_name, ''), NULLIF(profile_name, ''), phone_number) AS label, phone_number AS number, '' AS portfolio_id
	FROM unofficial_whatsapp_instances WHERE workspace_id = ? AND deleted_at IS NULL AND phone_number <> ''
)
SELECT n.kind, n.label, n.number, n.portfolio_id,
	COALESCE((SELECT string_agg(l.page_id, ',' ORDER BY l.page_id) FROM ad_page_whatsapp_links l
		WHERE l.workspace_id = ? AND l.number = regexp_replace(n.number, '[^0-9]', '', 'g')), '') AS linked_pages
FROM numbers n`

const recordPageLinkSQL = `INSERT INTO ad_page_whatsapp_links (workspace_id, page_id, number, linked_at) VALUES (?, ?, ?, ?)
ON CONFLICT (workspace_id, page_id, number) DO UPDATE SET linked_at = EXCLUDED.linked_at`

type numberRow struct {
	Kind        string
	Label       string
	Number      string
	PortfolioID string
	LinkedPages string
}

func (d *numberDirectory) List(ctx context.Context, workspaceID string) ([]ads.WorkspaceNumber, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ads.ErrWorkspaceRequired
	}
	var rows []numberRow
	if err := d.db.WithContext(ctx).Raw(workspaceNumbersQuery, workspaceID, workspaceID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("ads: list workspace numbers: %w", err)
	}
	out := make([]ads.WorkspaceNumber, 0, len(rows))
	for _, r := range rows {
		out = append(out, ads.WorkspaceNumber{Kind: ads.NumberKind(r.Kind), Label: r.Label, Number: r.Number, PortfolioID: r.PortfolioID, LinkedPageIDs: splitList(r.LinkedPages)})
	}
	return out, nil
}

func (d *numberDirectory) RecordPageLink(ctx context.Context, workspaceID, pageID, number string) error {
	digits := ads.DigitsOnly(number)
	if strings.TrimSpace(workspaceID) == "" {
		return ads.ErrWorkspaceRequired
	}
	if strings.TrimSpace(pageID) == "" || digits == "" {
		return fmt.Errorf("ads: a page link needs a page and a number")
	}
	return d.db.WithContext(ctx).Exec(recordPageLinkSQL, workspaceID, strings.TrimSpace(pageID), digits, d.now()).Error
}
