package leadsend_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	lmw "vozko/domain/lead_message_window"
	"vozko/infra/database"
)

const runningChunk = 5000

var (
	errWorkspaceRequired = errors.New("lead send store: workspace is required")
	errUnknownChannel    = errors.New("lead send store: unknown channel")
	errNegativeKeep      = errors.New("lead send store: the number of entries to keep cannot be negative")
)

type tables struct {
	campaigns string
	entries   string
}

func tablesOf(channel campaign.Channel) (tables, error) {
	switch channel {
	case campaign.ChannelOfficial:
		return officialTables, nil
	case campaign.ChannelUnofficial:
		return unofficialTables, nil
	}
	return tables{}, fmt.Errorf("%w: %q", errUnknownChannel, channel)
}

func pendingInRunningSQL(t tables) string {
	return `SELECT e.lead_id FROM ` + t.entries + ` e WHERE e.campaign_id IN (SELECT c.id FROM ` + t.campaigns +
		` c WHERE c.workspace_id = ?::uuid AND c.status = ? AND c.deleted_at IS NULL) AND e.status = ? AND e.deleted_at IS NULL`
}

var (
	officialTables   = tables{campaigns: "whatsapp_campaigns", entries: "whatsapp_campaign_entries"}
	unofficialTables = tables{campaigns: "unofficial_whatsapp_campaigns", entries: "unofficial_whatsapp_campaign_entries"}
)

var inRunningCampaignsSQL = pendingInRunningSQL(officialTables) + ` AND e.lead_id = ANY(?::uuid[]) UNION ` +
	pendingInRunningSQL(unofficialTables) + ` AND e.lead_id = ANY(?::uuid[])`

var pendingInAnyRunningSQL = pendingInRunningSQL(officialTables) + ` UNION ALL ` + pendingInRunningSQL(unofficialTables)

func skipInRunningSQL(channel campaign.Channel) string {
	t, err := tablesOf(channel)
	if err != nil {
		return ""
	}
	return `UPDATE ` + t.entries + ` SET status = ?, error_code = ?, error_message = ?, updated_at = ? WHERE campaign_id = ?::uuid AND status = ? AND deleted_at IS NULL` +
		` AND EXISTS (SELECT 1 FROM ` + t.campaigns + ` part WHERE part.id = ?::uuid AND part.workspace_id = ?::uuid AND part.status = ? AND part.deleted_at IS NULL)` +
		` AND lead_id IN (` + pendingInAnyRunningSQL + `)`
}

func deleteStoppedSQL(channel campaign.Channel) string {
	t, err := tablesOf(channel)
	if err != nil {
		return ""
	}
	return `UPDATE ` + t.campaigns + ` SET deleted_at = ? WHERE id = ?::uuid AND workspace_id = ?::uuid AND status = ? AND deleted_at IS NULL`
}

func deleteEntriesSQL(t tables) string {
	return `UPDATE ` + t.entries + ` SET deleted_at = ? WHERE campaign_id = ?::uuid AND deleted_at IS NULL`
}

func tallySQL(channel campaign.Channel) string {
	t, err := tablesOf(channel)
	if err != nil {
		return ""
	}
	window := `0`
	if channel == campaign.ChannelOfficial {
		window = `COUNT(*) FILTER (WHERE e.status = ? AND EXISTS (SELECT 1 FROM lead_message_windows w WHERE w.lead_id = e.lead_id AND w.business_phone_id = ?::uuid AND w.last_message_at > ?))`
	}
	return `SELECT e.campaign_id, COUNT(*) AS entries, COUNT(*) FILTER (WHERE e.status = ?) AS eligible,` +
		` COUNT(*) FILTER (WHERE e.status = ? AND l.whatsapp_opt_in_at IS NULL) AS no_consent, ` + window + ` AS window_open` +
		` FROM ` + t.entries + ` e JOIN ` + t.campaigns + ` c ON c.id = e.campaign_id AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL` +
		` LEFT JOIN leads l ON l.id = e.lead_id WHERE e.campaign_id = ANY(?::uuid[]) AND e.deleted_at IS NULL GROUP BY e.campaign_id`
}

func skipsSQL(channel campaign.Channel) string {
	t, err := tablesOf(channel)
	if err != nil {
		return ""
	}
	return `SELECT e.campaign_id, e.error_code, CASE WHEN e.error_code = ANY(?::int[]) THEN COALESCE(e.error_message, '') ELSE '' END AS detail, COUNT(*) AS n FROM ` + t.entries + ` e` +
		` JOIN ` + t.campaigns + ` c ON c.id = e.campaign_id AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL` +
		` WHERE e.campaign_id = ANY(?::uuid[]) AND e.deleted_at IS NULL AND e.error_code = ANY(?::int[]) GROUP BY 1, 2, 3`
}

func skipBeyondSQL(channel campaign.Channel) string {
	t, err := tablesOf(channel)
	if err != nil {
		return ""
	}
	return `UPDATE ` + t.entries + ` SET status = ?, error_code = ?, error_message = ?, updated_at = ? WHERE id IN (` +
		`SELECT pending.id FROM ` + t.entries + ` pending JOIN ` + t.campaigns + ` c ON c.id = pending.campaign_id AND c.workspace_id = ?::uuid AND c.status = ? AND c.deleted_at IS NULL` +
		` WHERE pending.campaign_id = ?::uuid AND pending.status = ? AND pending.deleted_at IS NULL ORDER BY pending.lead_id OFFSET ?)`
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) InRunningCampaigns(ctx context.Context, workspaceID string, leadIDs []string) (map[string]bool, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, errWorkspaceRequired
	}
	running := map[string]bool{}
	ids := database.UUIDArray(leadIDs)
	for start := 0; start < len(ids); start += runningChunk {
		chunk := pq.StringArray(ids[start:min(start+runningChunk, len(ids))])
		var found []string
		err := s.db.WithContext(ctx).Raw(inRunningCampaignsSQL,
			workspaceID, string(campaign.StatusRunning), string(campaign.SendStatusPending), chunk,
			workspaceID, string(campaign.StatusRunning), string(campaign.SendStatusPending), chunk,
		).Scan(&found).Error
		if err != nil {
			return nil, fmt.Errorf("leads already in a running campaign: %w", err)
		}
		for _, id := range found {
			running[id] = true
		}
	}
	return running, nil
}

type tallyRow struct {
	CampaignID string
	Entries    int
	Eligible   int
	NoConsent  int
	WindowOpen int
}

type skipRow struct {
	CampaignID string
	ErrorCode  int
	Detail     string
	N          int
}

func (s *Store) Tally(ctx context.Context, channel campaign.Channel, workspaceID string, campaignIDs []string, businessPhoneID string, now time.Time) ([]campaign.PartTally, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, errWorkspaceRequired
	}
	if _, err := tablesOf(channel); err != nil {
		return nil, err
	}
	ids := pq.StringArray(database.UUIDArray(campaignIDs))
	pending := string(campaign.SendStatusPending)
	args := []interface{}{pending, pending}
	if channel == campaign.ChannelOfficial {
		args = append(args, pending, businessPhoneID, now.UTC().Add(-lmw.MessageWindowDuration))
	}
	args = append(args, workspaceID, ids)
	var rows []tallyRow
	if err := s.db.WithContext(ctx).Raw(tallySQL(channel), args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("selection send tally: %w", err)
	}
	var skips []skipRow
	if err := s.db.WithContext(ctx).Raw(skipsSQL(channel), pq.Array(campaign.DetailedSkipCodes()), workspaceID, ids, pq.Array(campaign.SkipFailureCodes())).Scan(&skips).Error; err != nil {
		return nil, fmt.Errorf("selection send skips: %w", err)
	}
	parts := make([]campaign.PartTally, 0, len(rows))
	byID := map[string]int{}
	for _, row := range rows {
		byID[row.CampaignID] = len(parts)
		parts = append(parts, campaign.PartTally{
			CampaignID: row.CampaignID, Entries: row.Entries, Missing: map[campaign.MissingVariable]int{},
			Tally: campaign.Tally{Eligible: row.Eligible, Skipped: map[campaign.SkipReason]int{},
				Counted: map[campaign.CountedReason]int{campaign.CountedWindowOpen: row.WindowOpen, campaign.CountedNoConsentRecorded: row.NoConsent}},
		})
	}
	for _, skip := range skips {
		at, known := byID[skip.CampaignID]
		reason, coded := campaign.SkipReasonOfFailure(skip.ErrorCode)
		if !known || !coded {
			continue
		}
		parts[at].Skipped[reason] += skip.N
		detail := campaign.ParseSkipDetail(reason, skip.Detail)
		for _, missing := range detail.Missing {
			parts[at].Missing[missing] += skip.N
		}
		parts[at].CooldownDays = max(parts[at].CooldownDays, detail.CooldownDays)
	}
	return parts, nil
}

func (s *Store) SkipBeyond(ctx context.Context, channel campaign.Channel, workspaceID, campaignID string, keep int) (int64, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return 0, errWorkspaceRequired
	}
	if keep < 0 {
		return 0, errNegativeKeep
	}
	if _, err := tablesOf(channel); err != nil {
		return 0, err
	}
	status, code, message := campaign.SkipOverCap.Outcome()
	result := s.db.WithContext(ctx).Exec(skipBeyondSQL(channel),
		string(status), code, message, time.Now().UTC(), workspaceID, string(campaign.StatusStopped), campaignID, string(campaign.SendStatusPending), keep)
	if result.Error != nil {
		return 0, fmt.Errorf("selection send over the first %d: %w", keep, result.Error)
	}
	return result.RowsAffected, nil
}

func (s *Store) SkipInRunning(ctx context.Context, channel campaign.Channel, workspaceID, campaignID string) (int64, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return 0, errWorkspaceRequired
	}
	if _, err := tablesOf(channel); err != nil {
		return 0, err
	}
	status, code, message := campaign.SkipAlreadyInRunningCampaign.Outcome()
	pending, running := string(campaign.SendStatusPending), string(campaign.StatusRunning)
	result := s.db.WithContext(ctx).Exec(skipInRunningSQL(channel),
		string(status), code, message, time.Now().UTC(), campaignID, pending, campaignID, workspaceID, string(campaign.StatusStopped),
		workspaceID, running, pending, workspaceID, running, pending)
	if result.Error != nil {
		return 0, fmt.Errorf("selection send leads already in a running campaign: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (s *Store) DeleteStopped(ctx context.Context, channel campaign.Channel, workspaceID, campaignID string) (bool, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return false, errWorkspaceRequired
	}
	t, err := tablesOf(channel)
	if err != nil {
		return false, err
	}
	deleted := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		claimed := tx.Exec(deleteStoppedSQL(channel), now, campaignID, workspaceID, string(campaign.StatusStopped))
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected == 0 {
			return nil
		}
		deleted = true
		return tx.Exec(deleteEntriesSQL(t), now, campaignID).Error
	})
	if err != nil {
		return false, fmt.Errorf("cancel the stopped send %s: %w", campaignID, err)
	}
	return deleted, nil
}

func keyedPartsSQL(channel campaign.Channel) string {
	t, err := tablesOf(channel)
	if err != nil {
		return ""
	}
	return `SELECT id, idempotency_key FROM ` + t.campaigns + ` WHERE workspace_id = ?::uuid AND deleted_at IS NULL AND idempotency_key LIKE ? ORDER BY idempotency_key`
}

var errKeyBaseInvalid = errors.New("lead send store: the send key base is empty or holds a LIKE wildcard")

func (s *Store) KeyedParts(ctx context.Context, channel campaign.Channel, workspaceID, base string) ([]campaign.KeyedPart, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, errWorkspaceRequired
	}
	if strings.TrimSpace(base) == "" || strings.ContainsAny(base, `%_\`) {
		return nil, errKeyBaseInvalid
	}
	if _, err := tablesOf(channel); err != nil {
		return nil, err
	}
	var rows []struct {
		ID             string
		IdempotencyKey string
	}
	if err := s.db.WithContext(ctx).Raw(keyedPartsSQL(channel), workspaceID, base+":%").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("campaigns of send %s: %w", base, err)
	}
	parts := make([]campaign.KeyedPart, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, campaign.KeyedPart{ID: row.ID, Key: row.IdempotencyKey})
	}
	return parts, nil
}
