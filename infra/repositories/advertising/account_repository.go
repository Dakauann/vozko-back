package advertising_repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type accountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) advertising.AccountRepository {
	return &accountRepository{db: db}
}

const upsertAccountSQL = `INSERT INTO ad_accounts (id, workspace_id, grant_id, meta_account_id, name, business_id, business_name, currency, timezone,
	meta_status, disable_reason, has_funding, amount_spent, spend_cap, user_tasks, connection, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
ON CONFLICT (meta_account_id) DO UPDATE SET
	grant_id = EXCLUDED.grant_id,
	name = EXCLUDED.name,
	business_name = EXCLUDED.business_name,
	currency = EXCLUDED.currency,
	timezone = EXCLUDED.timezone,
	meta_status = EXCLUDED.meta_status,
	disable_reason = EXCLUDED.disable_reason,
	has_funding = EXCLUDED.has_funding,
	amount_spent = EXCLUDED.amount_spent,
	spend_cap = EXCLUDED.spend_cap,
	connection = EXCLUDED.connection,
	business_id = EXCLUDED.business_id,
	user_tasks = EXCLUDED.user_tasks,
	updated_at = NOW()
WHERE ad_accounts.workspace_id = EXCLUDED.workspace_id
RETURNING id, created_at, updated_at`

func (r *accountRepository) Upsert(ctx context.Context, a *advertising.AdAccount) error {
	if blank(a.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	metaAccountID := advertising.NormalizeAccountID(a.MetaAccountID)
	if metaAccountID == "" {
		return errMetaAccountRequired
	}
	type upserted struct {
		ID        string    `gorm:"column:id"`
		CreatedAt time.Time `gorm:"column:created_at"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	var rows []upserted
	err := r.db.WithContext(ctx).Raw(upsertAccountSQL,
		uuid.New().String(), a.WorkspaceID, a.GrantID, metaAccountID, a.Name, a.BusinessID, a.BusinessName, a.Currency, a.Timezone,
		int(a.MetaStatus), a.DisableReason, a.HasFunding, a.AmountSpent, a.SpendCap, pq.StringArray(storedTasks(a.Tasks)), string(a.Connection),
	).Scan(&rows).Error
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return advertising.ErrAccountLinkedElsewhere
	}
	a.ID, a.MetaAccountID, a.CreatedAt, a.UpdatedAt = rows[0].ID, metaAccountID, rows[0].CreatedAt, rows[0].UpdatedAt
	return nil
}

func (r *accountRepository) FindByID(ctx context.Context, workspaceID, id string) (*advertising.AdAccount, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdAccount
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND id = ?", workspaceID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccount(&record), nil
}

func (r *accountRepository) FindByMetaAccountID(ctx context.Context, metaAccountID string) (*advertising.AdAccount, error) {
	normalized := advertising.NormalizeAccountID(metaAccountID)
	if normalized == "" {
		return nil, errMetaAccountRequired
	}
	var record schema.AdAccount
	if err := r.db.WithContext(ctx).
		First(&record, "meta_account_id = ? AND connection <> ?", normalized, string(advertising.ConnectionDisconnected)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccount(&record), nil
}

func (r *accountRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*advertising.AdAccount, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdAccount
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND connection <> ?", workspaceID, string(advertising.ConnectionDisconnected)).
		Order("name, id").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toAccounts(records), nil
}

func (r *accountRepository) ListConnected(ctx context.Context, limit, offset int) ([]*advertising.AdAccount, error) {
	var records []schema.AdAccount
	if err := r.db.WithContext(ctx).
		Where("connection = ?", string(advertising.ConnectionConnected)).
		Order("id").Limit(limit).Offset(offset).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toAccounts(records), nil
}

func (r *accountRepository) SetConnection(ctx context.Context, id string, c advertising.Connection) error {
	return r.update(ctx, id, map[string]any{"connection": string(c)})
}

func (r *accountRepository) MarkSynced(ctx context.Context, id string, at time.Time) error {
	return r.update(ctx, id, map[string]any{"last_synced_at": at})
}

func (r *accountRepository) update(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.AdAccount{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrAccountNotFound
	}
	return nil
}

func toAccounts(records []schema.AdAccount) []*advertising.AdAccount {
	out := make([]*advertising.AdAccount, 0, len(records))
	for i := range records {
		out = append(out, toAccount(&records[i]))
	}
	return out
}

func toAccount(record *schema.AdAccount) *advertising.AdAccount {
	return &advertising.AdAccount{
		ID:            record.ID,
		WorkspaceID:   record.WorkspaceID,
		GrantID:       record.GrantID,
		MetaAccountID: record.MetaAccountID,
		Name:          record.Name,
		BusinessID:    record.BusinessID,
		BusinessName:  record.BusinessName,
		Currency:      record.Currency,
		Timezone:      record.Timezone,
		MetaStatus:    advertising.MetaAccountStatus(record.MetaStatus),
		DisableReason: record.DisableReason,
		HasFunding:    record.HasFunding,
		AmountSpent:   record.AmountSpent,
		SpendCap:      record.SpendCap,
		Tasks:         []string(record.UserTasks),
		Connection:    advertising.Connection(record.Connection),
		LastSyncedAt:  record.LastSyncedAt,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}

func storedTasks(tasks []string) []string {
	if tasks == nil {
		return []string{}
	}
	return tasks
}
