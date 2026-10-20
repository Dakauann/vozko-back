package unofficial_whatsapp_group_repository

import (
	"context"

	"github.com/lib/pq"
	"gorm.io/gorm"

	uwg "vozko/domain/unofficial_whatsapp_group"
)

type campaignReader struct{ db *gorm.DB }

func NewCampaignReader(db *gorm.DB) uwg.CampaignReader { return &campaignReader{db: db} }

func campaignScope(scope uwg.Scope) (string, []any) {
	if !scope.Restrict {
		return "TRUE", nil
	}
	if len(scope.InstanceIDs) == 0 {
		return "FALSE", nil
	}
	return "(c.instance_id IN ? OR c.sender_instance_ids && ?)", []any{scope.InstanceIDs, pq.StringArray(scope.InstanceIDs)}
}

func (r *campaignReader) ForGroup(ctx context.Context, workspaceID, groupJID string, scope uwg.Scope, limit int) ([]uwg.GroupCampaign, error) {
	where, args := campaignScope(scope)
	var out []uwg.GroupCampaign
	err := r.db.WithContext(ctx).Raw(`
		SELECT c.id AS campaign_id, c.name, c.status AS campaign_status, COALESCE(e.sender_instance_id::text, c.instance_id::text, '') AS instance_id,
		       e.status AS entry_status, e.error_message, e.sent_at, c.created_at
		  FROM unofficial_whatsapp_campaign_entries e
		  JOIN unofficial_whatsapp_campaigns c ON c.id = e.campaign_id AND c.deleted_at IS NULL
		 WHERE e.workspace_id = ? AND e.jid = ? AND e.lead_id IS NULL AND e.deleted_at IS NULL AND `+where+`
		 ORDER BY c.created_at DESC
		 LIMIT ?`, append(append([]any{workspaceID, groupJID}, args...), limit)...).Scan(&out).Error
	return out, err
}

func (r *campaignReader) RunningForGroups(ctx context.Context, workspaceID string, groupJIDs []string, scope uwg.Scope) (map[string]int, error) {
	out := map[string]int{}
	if len(groupJIDs) == 0 {
		return out, nil
	}
	type row struct {
		JID   string `gorm:"column:jid"`
		Total int
	}
	var rows []row
	where, args := campaignScope(scope)
	err := r.db.WithContext(ctx).Raw(`
		SELECT e.jid, COUNT(DISTINCT c.id) AS total
		  FROM unofficial_whatsapp_campaign_entries e
		  JOIN unofficial_whatsapp_campaigns c ON c.id = e.campaign_id AND c.deleted_at IS NULL AND c.status = 'RUNNING'
		 WHERE e.workspace_id = ? AND e.jid IN ? AND e.lead_id IS NULL AND e.deleted_at IS NULL AND `+where+`
		 GROUP BY e.jid`, append([]any{workspaceID, groupJIDs}, args...)...).Scan(&rows).Error
	for _, rw := range rows {
		out[rw.JID] = rw.Total
	}
	return out, err
}
