package lead

import (
	"context"
	"database/sql"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	"vozko/infra/database"
	lcs_repository "vozko/infra/repositories/lead_campaign_send"
	leadsend_repository "vozko/infra/repositories/leadsend"
)

const (
	leadFactsColumns = "SELECT id, (number IS NOT NULL AND number <> '') AS has_identity, blocked," +
		" (opted_out_at IS NOT NULL) AS opted_out, (whatsapp_opt_in_at IS NOT NULL) AS has_consent"
	leadFactsFrom = " FROM leads WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND deleted_at IS NULL"
	leadFactsSQL  = leadFactsColumns + leadFactsFrom
)

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
	ids := database.UUIDsOf(leadIDs)
	out := make(map[string]campaign.LeadFacts, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []leadFactsRow
	if err := s.db.WithContext(ctx).Raw(leadFactsSQL, workspaceID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = campaign.LeadFacts{Found: true, HasIdentity: row.HasIdentity, Blocked: row.Blocked, OptedOut: row.OptedOut, HasConsent: row.HasConsent}
	}
	return out, nil
}

const (
	screeningChunk = "WITH chunk AS (SELECT unnest(?::uuid[]) AS lead_id) "
	inChunk        = "IN (SELECT lead_id FROM chunk)"
	screeningWhere = " WHERE workspace_id = ? AND id " + inChunk + " AND deleted_at IS NULL"
)

func screeningSQL(q campaign.ScreeningQuery) (string, []any) {
	columns, joins := leadFactsColumns, ""
	var args []any
	if q.SenderID != "" {
		columns += ", sent.last_sent_at"
		joins += " LEFT JOIN (" + lcs_repository.LastSentSQL(inChunk) + ") sent ON sent.lead_id = leads.id"
		args = append(args, q.SenderID)
	} else {
		columns += ", NULL::timestamptz AS last_sent_at"
	}
	if q.Running {
		running, runningArgs := leadsend_repository.RunningPendingSQL(q.WorkspaceID, inChunk)
		columns += ", running.lead_id IS NOT NULL AS in_running_campaign"
		joins += " LEFT JOIN (" + running + ") running ON running.lead_id = leads.id"
		args = append(args, runningArgs...)
	} else {
		columns += ", false AS in_running_campaign"
	}
	return screeningChunk + columns + " FROM leads" + joins + screeningWhere, args
}

func (s *SendFacts) ScreeningFacts(ctx context.Context, q campaign.ScreeningQuery) (map[string]campaign.ScreeningFacts, error) {
	workspaceID := strings.TrimSpace(q.WorkspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	ids := database.UUIDsOf(q.LeadIDs)
	out := make(map[string]campaign.ScreeningFacts, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	q.WorkspaceID = workspaceID
	statement, args := screeningSQL(q)
	rows, err := s.db.WithContext(ctx).Raw(statement, append(append([]any{ids}, args...), workspaceID)...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var f campaign.ScreeningFacts
		var lastSent sql.NullTime
		if err := rows.Scan(&id, &f.Lead.HasIdentity, &f.Lead.Blocked, &f.Lead.OptedOut, &f.Lead.HasConsent, &lastSent, &f.InRunningCampaign); err != nil {
			return nil, err
		}
		f.Lead.Found = true
		if lastSent.Valid {
			at := lastSent.Time
			f.LastSentAt = &at
		}
		out[id] = f
	}
	return out, rows.Err()
}
