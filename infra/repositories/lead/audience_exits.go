package lead

import (
	"context"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/lead"
)

const audienceExitFactsSQL = "SELECT blocked, (opted_out_at IS NOT NULL) AS opted_out, COUNT(*) AS leads" +
	" FROM leads WHERE workspace_id = ? AND number = ANY(?) AND deleted_at IS NULL GROUP BY 1, 2 ORDER BY 1, 2"

type audienceExitRow struct {
	Blocked  bool `gorm:"column:blocked"`
	OptedOut bool `gorm:"column:opted_out"`
	Leads    int  `gorm:"column:leads"`
}

func (s *SendFacts) AudienceExitFacts(ctx context.Context, workspaceID string, numbers []string) ([]campaign.LeadFactsGroup, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	if len(formats) == 0 {
		return []campaign.LeadFactsGroup{}, nil
	}
	var rows []audienceExitRow
	if err := s.db.WithContext(ctx).Raw(audienceExitFactsSQL, workspaceID, formats).Scan(&rows).Error; err != nil {
		return nil, err
	}
	groups := make([]campaign.LeadFactsGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, campaign.LeadFactsGroup{
			Facts: campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: row.Blocked, OptedOut: row.OptedOut},
			Leads: row.Leads,
		})
	}
	return groups, nil
}
