package geocoding_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/geocoding"
)

const (
	settingsColumnsSQL = "workspace_id::text AS workspace_id, provider, provider_changed_by::text AS provider_changed_by, provider_changed_at," +
		" monthly_ceiling, ceiling_changed_by::text AS ceiling_changed_by, ceiling_changed_at"

	readSettingsSQL = "SELECT " + settingsColumnsSQL + " FROM geocoding_settings WHERE workspace_id = ?"

	readManySettingsSQL = "SELECT " + settingsColumnsSQL + " FROM geocoding_settings WHERE workspace_id = ANY(?::uuid[])"

	ensureSettingsSQL = "INSERT INTO geocoding_settings (workspace_id, updated_at) VALUES (?, ?) ON CONFLICT (workspace_id) DO NOTHING"

	lockSettingsSQL = "SELECT " + settingsColumnsSQL + " FROM geocoding_settings WHERE workspace_id = ? FOR UPDATE"

	writeSettingsSQL = "UPDATE geocoding_settings SET provider = ?, provider_changed_by = ?, provider_changed_at = ?," +
		" monthly_ceiling = ?, ceiling_changed_by = ?, ceiling_changed_at = ?, updated_at = ? WHERE workspace_id = ?"

	takeSlotSQL = "WITH archived AS (INSERT INTO geocoding_usage_months AS m (workspace_id, cycle_start, requests, updated_at)" +
		" SELECT p.workspace_id, p.cycle_start, p.requests, p.updated_at FROM geocoding_usage p WHERE p.workspace_id = ?::uuid AND p.cycle_start <> ?::timestamptz" +
		" ON CONFLICT (workspace_id, cycle_start) DO UPDATE SET requests = GREATEST(m.requests, excluded.requests))," +
		" taken AS (INSERT INTO geocoding_usage AS u (workspace_id, cycle_start, requests, day, day_requests, updated_at)" +
		" SELECT ?::uuid, ?::timestamptz, 1, ?::timestamptz, 1, ?::timestamptz" +
		" WHERE EXISTS (SELECT 1 FROM geocoding_settings s WHERE s.workspace_id = ?::uuid AND s.provider = ? AND s.monthly_ceiling IS NOT DISTINCT FROM ?::bigint)" +
		" ON CONFLICT (workspace_id) DO UPDATE SET" +
		" requests = (CASE WHEN u.cycle_start = excluded.cycle_start THEN u.requests ELSE 0 END) + 1," +
		" day_requests = (CASE WHEN u.day = excluded.day THEN u.day_requests ELSE 0 END) + 1," +
		" cycle_start = excluded.cycle_start, day = excluded.day, updated_at = excluded.updated_at" +
		" WHERE (CASE WHEN u.cycle_start = excluded.cycle_start THEN u.requests ELSE 0 END) < ?" +
		" AND (CASE WHEN u.day = excluded.day THEN u.day_requests ELSE 0 END) < ?" +
		" RETURNING u.workspace_id, u.cycle_start, u.requests, u.day_requests, u.updated_at)," +
		" kept AS (INSERT INTO geocoding_usage_months AS m (workspace_id, cycle_start, requests, updated_at) SELECT workspace_id, cycle_start, requests, updated_at FROM taken" +
		" ON CONFLICT (workspace_id, cycle_start) DO UPDATE SET requests = GREATEST(m.requests, excluded.requests), updated_at = excluded.updated_at)" +
		" SELECT requests, day_requests FROM taken"

	readUsageSQL = "SELECT cycle_start, requests, day, day_requests FROM geocoding_usage WHERE workspace_id = ?"

	readUsageOfSQL = "SELECT workspace_id::text AS workspace_id, cycle_start, requests, day, day_requests FROM geocoding_usage WHERE workspace_id = ANY(?::uuid[])"

	readHistorySQL = "SELECT workspace_id::text AS workspace_id, cycle_start, requests FROM geocoding_usage_months WHERE workspace_id = ANY(?::uuid[]) AND cycle_start >= ?" +
		" UNION ALL SELECT u.workspace_id::text, u.cycle_start, u.requests FROM geocoding_usage u WHERE u.workspace_id = ANY(?::uuid[]) AND u.cycle_start >= ?" +
		" AND NOT EXISTS (SELECT 1 FROM geocoding_usage_months m WHERE m.workspace_id = u.workspace_id AND m.cycle_start = u.cycle_start)" +
		" ORDER BY 1, 2 DESC"
)

var errSettingsRowMissing = errors.New("geocoding settings: the settings row could not be created")

type settingsRow struct {
	WorkspaceID       string
	Provider          *string
	ProviderChangedBy *string
	ProviderChangedAt *time.Time
	MonthlyCeiling    *int64
	CeilingChangedBy  *string
	CeilingChangedAt  *time.Time
}

func (r settingsRow) settings() geocoding.Settings {
	return geocoding.Settings{
		WorkspaceID:       r.WorkspaceID,
		Provider:          geocoding.Provider(textOf(r.Provider)),
		ProviderChangedBy: textOf(r.ProviderChangedBy),
		ProviderChangedAt: r.ProviderChangedAt,
		MonthlyCeiling:    r.MonthlyCeiling,
		CeilingChangedBy:  textOf(r.CeilingChangedBy),
		CeilingChangedAt:  r.CeilingChangedAt,
	}
}

func textOf(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func optional(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

type SettingsStore struct {
	db *gorm.DB
}

var _ geocoding.SettingsStore = (*SettingsStore)(nil)

func NewSettingsStore(db *gorm.DB) *SettingsStore {
	return &SettingsStore{db: db}
}

func (s *SettingsStore) Settings(ctx context.Context, workspaceID string) (geocoding.Settings, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return geocoding.Settings{}, geocoding.ErrSettingsUnreadable
	}
	var rows []settingsRow
	if err := s.db.WithContext(ctx).Raw(readSettingsSQL, workspaceID).Scan(&rows).Error; err != nil {
		return geocoding.Settings{}, err
	}
	if len(rows) == 0 {
		return geocoding.Settings{WorkspaceID: workspaceID}, nil
	}
	return rows[0].settings(), nil
}

func (s *SettingsStore) SettingsOf(ctx context.Context, workspaceIDs []string) (map[string]geocoding.Settings, error) {
	out := make(map[string]geocoding.Settings, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	var rows []settingsRow
	if err := s.db.WithContext(ctx).Raw(readManySettingsSQL, pq.StringArray(workspaceIDs)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.WorkspaceID] = row.settings()
	}
	return out, nil
}

func (s *SettingsStore) ChangeSettings(ctx context.Context, workspaceID string, change func(geocoding.Settings) (geocoding.Settings, error)) (geocoding.Settings, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || change == nil {
		return geocoding.Settings{}, geocoding.ErrSettingsUnreadable
	}
	var saved geocoding.Settings
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Exec(ensureSettingsSQL, workspaceID, now).Error; err != nil {
			return err
		}
		var rows []settingsRow
		if err := tx.Raw(lockSettingsSQL, workspaceID).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return errSettingsRowMissing
		}
		next, err := change(rows[0].settings())
		if err != nil {
			return err
		}
		if err := tx.Exec(writeSettingsSQL,
			optional(string(next.Provider)), optional(next.ProviderChangedBy), next.ProviderChangedAt,
			next.MonthlyCeiling, optional(next.CeilingChangedBy), next.CeilingChangedAt, now, workspaceID,
		).Error; err != nil {
			return err
		}
		next.WorkspaceID = workspaceID
		saved = next
		return nil
	})
	return saved, err
}

type UsageStore struct {
	db *gorm.DB
}

var _ geocoding.UsageStore = (*UsageStore)(nil)

func NewUsageStore(db *gorm.DB) *UsageStore {
	return &UsageStore{db: db}
}

func (s *UsageStore) TakeSlot(ctx context.Context, workspaceID string, slot geocoding.Slot) (bool, geocoding.Usage, error) {
	if slot.MonthlyLimit < 1 || slot.DailyLimit < 1 || !slot.Provider.Known() {
		return false, geocoding.Usage{}, nil
	}
	var taken []struct {
		Requests    int64
		DayRequests int64
	}
	var stored any
	if slot.StoredCeiling != nil {
		stored = *slot.StoredCeiling
	}
	err := s.db.WithContext(ctx).Raw(takeSlotSQL,
		workspaceID, slot.CycleStart,
		workspaceID, slot.CycleStart, slot.Day, time.Now().UTC(),
		workspaceID, string(slot.Provider), stored,
		slot.MonthlyLimit, slot.DailyLimit,
	).Scan(&taken).Error
	if err != nil {
		return false, geocoding.Usage{}, err
	}
	if len(taken) == 1 {
		return true, geocoding.Usage{CycleStart: slot.CycleStart, Requests: taken[0].Requests, Day: slot.Day, DayRequests: taken[0].DayRequests}, nil
	}
	usage, err := s.Usage(ctx, workspaceID)
	return false, usage, err
}

func (s *UsageStore) Usage(ctx context.Context, workspaceID string) (geocoding.Usage, error) {
	var rows []struct {
		CycleStart  time.Time
		Requests    int64
		Day         time.Time
		DayRequests int64
	}
	if err := s.db.WithContext(ctx).Raw(readUsageSQL, workspaceID).Scan(&rows).Error; err != nil {
		return geocoding.Usage{}, err
	}
	if len(rows) == 0 {
		return geocoding.Usage{}, nil
	}
	r := rows[0]
	return geocoding.Usage{CycleStart: r.CycleStart, Requests: r.Requests, Day: r.Day, DayRequests: r.DayRequests}, nil
}

var _ geocoding.UsageHistory = (*UsageStore)(nil)

type usageRow struct {
	WorkspaceID string
	CycleStart  time.Time
	Requests    int64
	Day         time.Time
	DayRequests int64
}

func (s *UsageStore) UsageOf(ctx context.Context, workspaceIDs []string) (map[string]geocoding.Usage, error) {
	out := make(map[string]geocoding.Usage, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	var rows []usageRow
	if err := s.db.WithContext(ctx).Raw(readUsageOfSQL, pq.StringArray(workspaceIDs)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkspaceID] = geocoding.Usage{CycleStart: r.CycleStart, Requests: r.Requests, Day: r.Day, DayRequests: r.DayRequests}
	}
	return out, nil
}

func (s *UsageStore) History(ctx context.Context, workspaceIDs []string, since time.Time) (map[string][]geocoding.MonthUsage, error) {
	out := make(map[string][]geocoding.MonthUsage, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	ids := pq.StringArray(workspaceIDs)
	var rows []usageRow
	if err := s.db.WithContext(ctx).Raw(readHistorySQL, ids, since, ids, since).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkspaceID] = append(out[r.WorkspaceID], geocoding.MonthUsage{CycleStart: r.CycleStart, Requests: r.Requests})
	}
	return out, nil
}
