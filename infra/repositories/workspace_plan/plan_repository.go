package workspace_plan_repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/infra/database/schema"
)

type planRepository struct {
	db *gorm.DB
}

type subscriptionRepository struct {
	db *gorm.DB
}

func NewPlanRepository(db *gorm.DB) workspace_plan.PlanRepository {
	return &planRepository{db: db}
}

func NewSubscriptionRepository(db *gorm.DB) workspace_plan.SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

func (r *planRepository) Create(plan *workspace_plan.PlanDefinition) error {
	if plan == nil {
		return workspace_plan.ErrPlanNotFound
	}
	return r.db.Create(planToSchema(plan)).Error
}

func (r *planRepository) Update(plan *workspace_plan.PlanDefinition) error {
	if plan == nil {
		return workspace_plan.ErrPlanNotFound
	}
	result := r.db.Model(&schema.WorkspacePlanDefinition{}).
		Where("id = ?", plan.ID).
		Select("*").
		Updates(planToSchema(plan))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_plan.ErrPlanNotFound
	}
	return nil
}

func (r *planRepository) Archive(planID string, archivedAt time.Time) error {
	result := r.db.Model(&schema.WorkspacePlanDefinition{}).
		Where("id = ?", planID).
		Update("archived_at", archivedAt)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_plan.ErrPlanNotFound
	}
	return nil
}

func (r *planRepository) GetByID(planID string) (*workspace_plan.PlanDefinition, error) {
	var row schema.WorkspacePlanDefinition
	if err := r.db.Where("id = ?", planID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_plan.ErrPlanNotFound
		}
		return nil, err
	}
	plan := planFromSchema(&row)
	items, err := r.ListPricingItems(planID)
	if err != nil {
		return nil, err
	}
	plan.PricingItems = items
	return plan, nil
}

func (r *planRepository) List(includeArchived bool) ([]*workspace_plan.PlanDefinition, error) {
	query := r.db.Model(&schema.WorkspacePlanDefinition{}).Order("created_at asc")
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var rows []schema.WorkspacePlanDefinition
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	plans := make([]*workspace_plan.PlanDefinition, 0, len(rows))
	for i := range rows {
		plan := planFromSchema(&rows[i])
		items, err := r.ListPricingItems(rows[i].ID)
		if err != nil {
			return nil, err
		}
		plan.PricingItems = items
		plans = append(plans, plan)
	}
	return plans, nil
}

func (r *subscriptionRepository) Create(subscription *workspace_plan.WorkspaceSubscription) error {
	if subscription == nil {
		return workspace_plan.ErrInvalidSubscription
	}
	return r.db.Create(subscriptionToSchema(subscription)).Error
}

func (r *subscriptionRepository) Update(subscription *workspace_plan.WorkspaceSubscription) error {
	if subscription == nil {
		return workspace_plan.ErrInvalidSubscription
	}
	result := r.db.Model(&schema.WorkspaceSubscription{}).
		Where("id = ?", subscription.ID).
		Updates(subscriptionToSchema(subscription))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_plan.ErrSubscriptionNotFound
	}
	return nil
}

func (r *subscriptionRepository) GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	var row schema.WorkspaceSubscription
	err := r.db.Where("workspace_id = ? AND status IN ? AND current_period_end >= ?", workspaceID, []string{string(workspace_plan.SubscriptionStatusActive), string(workspace_plan.SubscriptionStatusCancelled)}, at).
		Order("current_period_end desc, updated_at desc").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_plan.ErrSubscriptionNotCurrent
		}
		return nil, err
	}
	return subscriptionFromSchema(&row), nil
}

func (r *subscriptionRepository) GetLatestByWorkspaceID(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	var row schema.WorkspaceSubscription
	err := r.db.Where("workspace_id = ?", workspaceID).
		Order("current_period_end desc, updated_at desc, created_at desc").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_plan.ErrSubscriptionNotFound
		}
		return nil, err
	}
	return subscriptionFromSchema(&row), nil
}

func (r *subscriptionRepository) GetCurrentByWorkspaceIDs(workspaceIDs []string, at time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	var rows []schema.WorkspaceSubscription
	err := r.db.Where("workspace_id IN ? AND status IN ? AND current_period_end >= ?",
		workspaceIDs,
		[]string{string(workspace_plan.SubscriptionStatusActive), string(workspace_plan.SubscriptionStatusCancelled)},
		at,
	).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]*workspace_plan.WorkspaceSubscription, len(rows))
	for i := range rows {
		sub := subscriptionFromSchema(&rows[i])

		if existing, ok := result[sub.WorkspaceID]; ok {
			if sub.CurrentPeriodEnd.After(existing.CurrentPeriodEnd) {
				result[sub.WorkspaceID] = sub
			}
		} else {
			result[sub.WorkspaceID] = sub
		}
	}
	return result, nil
}

func (r *subscriptionRepository) ListActiveBillingDue(throughPeriodEnd time.Time, afterID string, limit int) ([]*workspace_plan.WorkspaceSubscription, error) {
	if limit <= 0 {
		limit = 500
	}
	q := r.db.Where("status = ? AND current_period_end <= ?",
		string(workspace_plan.SubscriptionStatusActive), throughPeriodEnd)
	if afterID != "" {
		q = q.Where("id > ?", afterID)
	}
	var rows []schema.WorkspaceSubscription
	if err := q.Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*workspace_plan.WorkspaceSubscription, 0, len(rows))
	for i := range rows {
		out = append(out, subscriptionFromSchema(&rows[i]))
	}
	return out, nil
}

func (r *subscriptionRepository) ListUpcomingExpirations(from, to time.Time, batchSize int) ([]*workspace_plan.WorkspaceSubscription, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var rows []schema.WorkspaceSubscription
	err := r.db.Where("status IN ? AND current_period_end > ? AND current_period_end <= ?",
		[]string{string(workspace_plan.SubscriptionStatusActive), string(workspace_plan.SubscriptionStatusCancelled)},
		from, to).
		Limit(batchSize).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*workspace_plan.WorkspaceSubscription, 0, len(rows))
	for i := range rows {
		out = append(out, subscriptionFromSchema(&rows[i]))
	}
	return out, nil
}

func (r *subscriptionRepository) ExpireOverdue(at time.Time, batchSize int) ([]string, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	// Postgres has no UPDATE ... LIMIT, so select the batch first (capturing the
	// workspace ids the caller needs for entitlement reconciliation), then update
	// exactly those rows by primary key.
	var due []struct {
		ID          string
		WorkspaceID string
	}
	if err := r.db.Model(&schema.WorkspaceSubscription{}).
		Select("id, workspace_id").
		Where("status IN ? AND current_period_end < ?",
			[]string{string(workspace_plan.SubscriptionStatusActive), string(workspace_plan.SubscriptionStatusCancelled)},
			at,
		).
		Limit(batchSize).
		Scan(&due).Error; err != nil {
		return nil, err
	}
	if len(due) == 0 {
		return nil, nil
	}
	ids := make([]string, len(due))
	workspaceIDs := make([]string, len(due))
	for i, d := range due {
		ids[i] = d.ID
		workspaceIDs[i] = d.WorkspaceID
	}
	if err := r.db.Model(&schema.WorkspaceSubscription{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"status":     string(workspace_plan.SubscriptionStatusExpired),
			"updated_at": at,
		}).Error; err != nil {
		return nil, err
	}
	return workspaceIDs, nil
}

func planToSchema(plan *workspace_plan.PlanDefinition) *schema.WorkspacePlanDefinition {
	return &schema.WorkspacePlanDefinition{
		ID:                             plan.ID,
		Name:                           plan.Name,
		Description:                    plan.Description,
		BasePriceBRLCents:              plan.BasePriceBRLCents,
		MaxCallChannels:                plan.MaxCallChannels,
		IncludedWhatsAppBusinessPhones: plan.IncludedWhatsAppBusinessPhones,
		MaxBranches:                    plan.MaxBranches,
		MaxHoldMusicTracks:             plan.MaxHoldMusicTracks,
		IsGloballyVisible:              plan.IsGloballyVisible,
		ExclusiveAffiliateID:           plan.ExclusiveAffiliateID,
		ArchivedAt:                     plan.ArchivedAt,
		CreatedAt:                      plan.CreatedAt,
		UpdatedAt:                      plan.UpdatedAt,
	}
}

func planFromSchema(row *schema.WorkspacePlanDefinition) *workspace_plan.PlanDefinition {
	return &workspace_plan.PlanDefinition{
		ID:                             row.ID,
		Name:                           row.Name,
		Description:                    row.Description,
		BasePriceBRLCents:              row.BasePriceBRLCents,
		MaxCallChannels:                row.MaxCallChannels,
		IncludedWhatsAppBusinessPhones: row.IncludedWhatsAppBusinessPhones,
		MaxBranches:                    row.MaxBranches,
		MaxHoldMusicTracks:             row.MaxHoldMusicTracks,
		IsGloballyVisible:              row.IsGloballyVisible,
		ExclusiveAffiliateID:           row.ExclusiveAffiliateID,
		ArchivedAt:                     row.ArchivedAt,
		CreatedAt:                      row.CreatedAt,
		UpdatedAt:                      row.UpdatedAt,
	}
}

func subscriptionToSchema(subscription *workspace_plan.WorkspaceSubscription) *schema.WorkspaceSubscription {
	return &schema.WorkspaceSubscription{
		ID:                 subscription.ID,
		WorkspaceID:        subscription.WorkspaceID,
		PlanDefinitionID:   subscription.PlanDefinitionID,
		PlanName:           subscription.PlanName,
		MaxCallChannels:    subscription.MaxCallChannels,
		BillingCycle:       string(subscription.BillingCycle),
		Status:             string(subscription.Status),
		CurrentPeriodStart: subscription.CurrentPeriodStart,
		CurrentPeriodEnd:   subscription.CurrentPeriodEnd,
		CancelledAt:        subscription.CancelledAt,
		CreatedAt:          subscription.CreatedAt,
		UpdatedAt:          subscription.UpdatedAt,
	}
}

func subscriptionFromSchema(row *schema.WorkspaceSubscription) *workspace_plan.WorkspaceSubscription {
	return &workspace_plan.WorkspaceSubscription{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		PlanDefinitionID:   row.PlanDefinitionID,
		PlanName:           row.PlanName,
		MaxCallChannels:    row.MaxCallChannels,
		BillingCycle:       workspace_plan.BillingCycle(row.BillingCycle),
		Status:             workspace_plan.SubscriptionStatus(row.Status),
		CurrentPeriodStart: row.CurrentPeriodStart,
		CurrentPeriodEnd:   row.CurrentPeriodEnd,
		CancelledAt:        row.CancelledAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func (r *planRepository) ReplacePricingItems(planID string, items []workspace_plan.PlanPricingItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plan_definition_id = ?", planID).Delete(&schema.PlanPricingItem{}).Error; err != nil {
			return err
		}
		for _, item := range items {
			row := planPricingItemToSchema(&item, planID)
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *planRepository) ListPricingItems(planID string) ([]workspace_plan.PlanPricingItem, error) {
	var rows []schema.PlanPricingItem
	if err := r.db.Where("plan_definition_id = ?", planID).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]workspace_plan.PlanPricingItem, len(rows))
	for i, row := range rows {
		items[i] = planPricingItemFromSchema(&row)
	}
	return items, nil
}

func planPricingItemToSchema(item *workspace_plan.PlanPricingItem, planID string) schema.PlanPricingItem {
	return schema.PlanPricingItem{
		ID:               item.ID,
		PlanDefinitionID: planID,
		Category:         item.Category,
		Service:          item.Service,
		Metric:           item.Metric,
		CostMicros:       item.CostMicros,
		PriceMicros:      item.PriceMicros,
		MarkupPct:        item.MarkupPct,
		Currency:         item.Currency,
	}
}

func planPricingItemFromSchema(row *schema.PlanPricingItem) workspace_plan.PlanPricingItem {
	return workspace_plan.PlanPricingItem{
		ID:               row.ID,
		PlanDefinitionID: row.PlanDefinitionID,
		Category:         row.Category,
		Service:          row.Service,
		Metric:           row.Metric,
		CostMicros:       row.CostMicros,
		PriceMicros:      row.PriceMicros,
		MarkupPct:        row.MarkupPct,
		Currency:         row.Currency,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func (r *planRepository) SetVisibility(planID string, workspaceIDs []string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plan_definition_id = ?", planID).Delete(&schema.PlanVisibilityEntry{}).Error; err != nil {
			return err
		}
		for _, wsID := range workspaceIDs {
			entry := schema.PlanVisibilityEntry{
				PlanDefinitionID: planID,
				WorkspaceID:      wsID,
			}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *planRepository) GetAllowedWorkspaceIDs(planID string) ([]string, error) {
	var rows []schema.PlanVisibilityEntry
	if err := r.db.Where("plan_definition_id = ?", planID).Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.WorkspaceID
	}
	return ids, nil
}

func (r *planRepository) GetAllowedWorkspaces(planID string) ([]workspace_plan.AllowedWorkspace, error) {
	type row struct {
		ID   string `gorm:"column:workspace_id"`
		Name string `gorm:"column:name"`
	}
	var rows []row
	err := r.db.Raw(
		"SELECT pv.workspace_id, w.name FROM plan_visibility_entries pv JOIN workspaces w ON w.id = pv.workspace_id WHERE pv.plan_definition_id = ? ORDER BY w.name ASC",
		planID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]workspace_plan.AllowedWorkspace, len(rows))
	for i, r := range rows {
		result[i] = workspace_plan.AllowedWorkspace{ID: r.ID, Name: r.Name}
	}
	return result, nil
}

func (r *planRepository) ListVisiblePlans(workspaceID string) ([]*workspace_plan.PlanDefinition, error) {
	var rows []schema.WorkspacePlanDefinition
	err := r.db.Where(
		"archived_at IS NULL AND exclusive_affiliate_id IS NULL AND (is_globally_visible = ? OR id IN (SELECT plan_definition_id FROM plan_visibility_entries WHERE workspace_id = ?))",
		true, workspaceID,
	).Order("created_at asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	plans := make([]*workspace_plan.PlanDefinition, 0, len(rows))
	for i := range rows {
		plan := planFromSchema(&rows[i])
		items, err := r.ListPricingItems(rows[i].ID)
		if err != nil {
			return nil, err
		}
		plan.PricingItems = items
		plans = append(plans, plan)
	}
	return plans, nil
}

func (r *planRepository) SetExclusiveAffiliate(planID string, affiliateID *string) error {
	result := r.db.Model(&schema.WorkspacePlanDefinition{}).
		Where("id = ?", planID).
		Update("exclusive_affiliate_id", affiliateID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_plan.ErrPlanNotFound
	}
	return nil
}

func (r *planRepository) ListByExclusiveAffiliateID(affiliateID string) ([]*workspace_plan.PlanDefinition, error) {
	var rows []schema.WorkspacePlanDefinition
	err := r.db.Where("archived_at IS NULL AND exclusive_affiliate_id = ?", affiliateID).
		Order("created_at asc").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	plans := make([]*workspace_plan.PlanDefinition, 0, len(rows))
	for i := range rows {
		plan := planFromSchema(&rows[i])
		items, err := r.ListPricingItems(rows[i].ID)
		if err != nil {
			return nil, err
		}
		plan.PricingItems = items
		plans = append(plans, plan)
	}
	return plans, nil
}
