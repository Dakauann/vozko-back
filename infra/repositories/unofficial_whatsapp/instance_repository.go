package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type instanceRepository struct {
	db *gorm.DB
}

// NewInstanceRepository builds the connected-number repository.
func NewInstanceRepository(db *gorm.DB) uw.InstanceRepository {
	return &instanceRepository{db: db}
}

func (r *instanceRepository) Create(ctx context.Context, i *uw.Instance) error {
	record := toInstanceSchema(i)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if isUniqueViolation(err) {
			return uw.ErrNumberAlreadyLinked
		}
		return err
	}
	i.ID = record.ID
	i.CreatedAt = record.CreatedAt
	i.UpdatedAt = record.UpdatedAt
	return nil
}

// Update writes the operator-editable configuration.
//
// It deliberately does NOT write status, session identity or restriction state:
// those are owned by UpdateStatus / UpdateSession / UpdateRestriction, which the
// health cron drives. A config save that also carried a stale status would
// resurrect a disconnected instance every time someone renamed it.
func (r *instanceRepository) Update(ctx context.Context, i *uw.Instance) error {
	record := toInstanceSchema(i)
	update := map[string]any{
		"department_id":          record.DepartmentID,
		"display_name":           record.DisplayName,
		"daily_send_cap":         record.DailySendCap,
		"send_delay_min_ms":      record.SendDelayMinMS,
		"send_delay_max_ms":      record.SendDelayMaxMS,
		"auto_reject_calls":      record.AutoRejectCalls,
		"agent_id":               record.AgentID,
		"workflow_id":            record.WorkflowID,
		"pipeline_id":            record.PipelineID,
		"enable_agent_responses": record.EnableAgentResponses,
		"enable_workflow":        record.EnableWorkflow,
		"enable_analysis":        record.EnableAnalysis,
		"enable_auto_staging":    record.EnableAutoStaging,
		"handle_groups":          record.HandleGroups,
		"warmup_started_at":      record.WarmupStartedAt,
	}
	if i.InstanceToken != "" {
		update["instance_token"] = record.InstanceToken
	}

	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("id = ?", i.ID).
		Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrInstanceNotFound
	}
	return nil
}

func (r *instanceRepository) UpdateStatus(ctx context.Context, id string, status uw.Status, reason string) error {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        string(status),
			"status_reason": truncate(reason, 255),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrInstanceNotFound
	}
	return nil
}

// UpdateSession records what one status poll learned.
//
// Identity fields are written only when the poll actually carried them: a
// disconnected instance answers with an empty JID and profile, and blanking the
// stored identity would leave the operator looking at a nameless row with no way
// to tell which number just dropped.
func (r *instanceRepository) UpdateSession(ctx context.Context, id string, in uw.SessionUpdate) error {
	update := map[string]any{"last_polled_at": in.PolledAt}

	if in.Status != nil {
		update["status"] = string(*in.Status)
		update["status_reason"] = truncate(in.StatusReason, 255)
	}
	setIfPresent(update, "jid", in.JID)
	setIfPresent(update, "lid", in.LID)
	setIfPresent(update, "phone_number", in.PhoneNumber)
	setIfPresent(update, "profile_name", in.ProfileName)
	setIfPresent(update, "profile_pic_url", in.ProfilePicURL)
	setIfPresent(update, "platform", in.Platform)
	if in.JID != "" {
		// Only trustworthy alongside a live identity; a logged-out poll reports
		// false for every account, business or not.
		update["is_business_acct"] = in.IsBusinessAcct
	}
	if in.ConnectedAt != nil {
		update["connected_at"] = in.ConnectedAt
	}
	if in.LastDisconnectAt != nil {
		update["last_disconnect_at"] = in.LastDisconnectAt
		update["last_disconnect_reason"] = truncate(in.LastDisconnectReason, 255)
	}

	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("id = ?", id).
		Updates(update)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			// The JID index fired: this number is already connected elsewhere.
			return uw.ErrNumberAlreadyLinked
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrInstanceNotFound
	}
	return nil
}

func (r *instanceRepository) UpdateRestriction(ctx context.Context, id string, restriction uw.Restriction) error {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"restriction_can_send_new": restriction.CanSendNewChats,
			"restriction_key":          truncate(restriction.Key, 64),
			"restriction_message":      truncate(restriction.Message, 500),
			"restriction_until":        restriction.Until,
			"restriction_used_quota":   restriction.UsedQuota,
			"restriction_total_quota":  restriction.TotalQuota,
			"restriction_checked_at":   restriction.CheckedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrInstanceNotFound
	}
	return nil
}

func (r *instanceRepository) SetWebhookRegistered(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("id = ?", id).
		UpdateColumn("webhook_set_at", at).Error
}

// RotateDeliveryToken replaces the webhook credential.
//
// Both columns move in one statement: the digest is what the endpoint resolves
// by and the encrypted copy is what the UI re-displays, so writing them
// separately would leave a window where the URL shown to an operator is not the
// URL that works.
func (r *instanceRepository) RotateDeliveryToken(ctx context.Context, id, token, tokenHash string) error {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"delivery_token":      piigorm.NewEncrypted(token),
			"delivery_token_hash": tokenHash,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrInstanceNotFound
	}
	return nil
}

func (r *instanceRepository) FindByID(ctx context.Context, id string) (*uw.Instance, error) {
	return r.first(ctx, r.db.WithContext(ctx).Where("id = ?", id))
}

// FindByDeliveryTokenHash resolves the tenant for an inbound webhook.
//
// Every non-deleted instance is eligible regardless of status: one that just
// dropped is precisely the one whose next event matters, and filtering by
// CONNECTED here would discard the disconnection notice itself.
func (r *instanceRepository) FindByDeliveryTokenHash(ctx context.Context, tokenHash string) (*uw.Instance, error) {
	if strings.TrimSpace(tokenHash) == "" {
		return nil, uw.ErrInstanceNotFound
	}
	return r.first(ctx, r.db.WithContext(ctx).Where("delivery_token_hash = ?", tokenHash))
}

func (r *instanceRepository) FindByJID(ctx context.Context, jid string) (*uw.Instance, error) {
	if strings.TrimSpace(jid) == "" {
		return nil, uw.ErrInstanceNotFound
	}
	return r.first(ctx, r.db.WithContext(ctx).Where("jid = ?", jid))
}

func (r *instanceRepository) FindByProviderInstanceID(ctx context.Context, serverID, providerInstanceID string) (*uw.Instance, error) {
	return r.first(ctx, r.db.WithContext(ctx).
		Where("server_id = ? AND provider_instance_id = ?", serverID, providerInstanceID))
}

func (r *instanceRepository) ListByWorkspace(
	ctx context.Context,
	input uw.ListInstancesInput,
) (*shared.PaginatedResult[*uw.Instance], error) {
	query := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("workspace_id = ?", input.WorkspaceID)

	if s := strings.TrimSpace(input.Search); s != "" {
		like := "%" + strings.ToLower(s) + "%"
		query = query.Where(
			"LOWER(display_name) LIKE ? OR LOWER(profile_name) LIKE ? OR phone_number LIKE ?",
			like, like, like)
	}
	if input.Status != nil {
		query = query.Where("status = ?", string(*input.Status))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	pagination := shared.NormalizePagination(input.Options.Pagination)

	var records []schema.UnofficialWhatsAppInstance
	if err := query.Order("created_at DESC").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(instancesToDomain(records), pagination, total), nil
}

func (r *instanceRepository) ListByServer(ctx context.Context, serverID string) ([]*uw.Instance, error) {
	var records []schema.UnofficialWhatsAppInstance
	if err := r.db.WithContext(ctx).Where("server_id = ?", serverID).Find(&records).Error; err != nil {
		return nil, err
	}
	return instancesToDomain(records), nil
}

// ListForHealthCheck returns instances not polled since `before`, oldest first.
//
// Terminal statuses are excluded: a banned number cannot recover, and polling it
// forever would spend the cron's budget on the one instance guaranteed not to
// change.
func (r *instanceRepository) ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*uw.Instance, error) {
	if limit <= 0 {
		limit = 100
	}
	var records []schema.UnofficialWhatsAppInstance
	err := r.db.WithContext(ctx).
		Where("status NOT IN ?", []string{
			string(uw.StatusBanned),
			string(uw.StatusProvisionFailed),
		}).
		Where("last_polled_at IS NULL OR last_polled_at < ?", before).
		Order("last_polled_at ASC NULLS FIRST").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return instancesToDomain(records), nil
}

// ListConnected returns every live instance, oldest-contacted first.
//
// Unlike ListForHealthCheck this ignores when the instance was last heard from:
// the integrity sweep asks whether our webhook is still registered and whether
// WhatsApp is restricting the number, and no inbound event reports either, so a
// chatty instance needs the sweep exactly as much as a quiet one.
func (r *instanceRepository) ListConnected(ctx context.Context, limit int) ([]*uw.Instance, error) {
	if limit <= 0 {
		limit = 500
	}
	var records []schema.UnofficialWhatsAppInstance
	err := r.db.WithContext(ctx).
		Where("status = ?", string(uw.StatusConnected)).
		Order("last_polled_at ASC NULLS FIRST").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return instancesToDomain(records), nil
}

func (r *instanceRepository) CountByServer(ctx context.Context, serverID string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppInstance{}).
		Where("server_id = ?", serverID).
		Count(&count).Error
	return int(count), err
}

func (r *instanceRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).
		Delete(&schema.UnofficialWhatsAppInstance{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrInstanceNotFound
	}
	return nil
}

func (r *instanceRepository) first(_ context.Context, query *gorm.DB) (*uw.Instance, error) {
	var record schema.UnofficialWhatsAppInstance
	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrInstanceNotFound
		}
		return nil, err
	}
	return toInstanceDomain(&record), nil
}

func instancesToDomain(records []schema.UnofficialWhatsAppInstance) []*uw.Instance {
	out := make([]*uw.Instance, 0, len(records))
	for i := range records {
		out = append(out, toInstanceDomain(&records[i]))
	}
	return out
}

// setIfPresent writes a column only when the poll supplied a value, so an
// incomplete answer never blanks what we already knew.
func setIfPresent(update map[string]any, column, value string) {
	if strings.TrimSpace(value) != "" {
		update[column] = value
	}
}
