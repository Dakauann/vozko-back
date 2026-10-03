package advertising_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

const objectBatchSize = 200

var syncedObjectColumns = []string{
	"workspace_id", "ad_account_id", "level", "campaign_meta_id", "adset_meta_id", "name",
	"status", "effective_status", "objective", "special_category", "destination_type", "optimization_goal", "bid_strategy",
	"daily_budget", "lifetime_budget", "budget_remaining", "start_time", "end_time",
	"creative", "review_feedback", "issues", "created_time", "updated_time", "synced_at",
}

type objectRepository struct {
	db *gorm.DB
}

func NewObjectRepository(db *gorm.DB) advertising.ObjectRepository {
	return &objectRepository{db: db}
}

func (r *objectRepository) ReplaceLevel(ctx context.Context, accountID string, level advertising.Level, objects []*advertising.Object, at time.Time) error {
	if blank(accountID) {
		return errAccountRequired
	}
	if !level.Valid() {
		return fmt.Errorf("advertising repository: unknown level %q", level)
	}
	records := make([]*schema.AdObject, 0, len(objects))
	kept := make([]string, 0, len(objects))
	for _, object := range objects {
		if object.AdAccountID != accountID || object.Level != level || blank(object.WorkspaceID) || blank(object.MetaID) {
			return fmt.Errorf("%w: %s", errForeignObject, object.MetaID)
		}
		record, err := toObjectRecord(object)
		if err != nil {
			return err
		}
		record.SyncedAt = at
		records = append(records, record)
		kept = append(kept, object.MetaID)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(records) > 0 {
			if err := tx.Clauses(objectConflict(syncedObjectColumns)).CreateInBatches(records, objectBatchSize).Error; err != nil {
				return err
			}
		}
		stale := tx.Model(&schema.AdObject{}).Where("ad_account_id = ? AND level = ?", accountID, string(level))
		if len(kept) > 0 {
			stale = stale.Where("meta_id NOT IN ?", kept)
		}
		return stale.Update("removed", true).Error
	})
}

func (r *objectRepository) Upsert(ctx context.Context, object *advertising.Object) error {
	if blank(object.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	if blank(object.AdAccountID) {
		return errAccountRequired
	}
	record, err := toObjectRecord(object)
	if err != nil {
		return err
	}
	columns := append(append([]string{}, syncedObjectColumns...), "budget_changes")
	return r.db.WithContext(ctx).Clauses(objectConflict(columns)).Create(record).Error
}

func objectConflict(columns []string) clause.OnConflict {
	set := clause.AssignmentColumns(columns)
	set = append(set, clause.Assignment{Column: clause.Column{Name: "removed"}, Value: false})
	return clause.OnConflict{Columns: []clause.Column{{Name: "meta_id"}}, DoUpdates: set}
}

func (r *objectRepository) Find(ctx context.Context, workspaceID, metaID string) (*advertising.Object, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdObject
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND meta_id = ?", workspaceID, metaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrObjectNotFound
		}
		return nil, err
	}
	return toObject(&record)
}

func (r *objectRepository) List(ctx context.Context, q advertising.ObjectQuery) ([]*advertising.Object, error) {
	if blank(q.WorkspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	if blank(q.AdAccountID) {
		return nil, errAccountRequired
	}
	query := r.db.WithContext(ctx).Where("workspace_id = ? AND ad_account_id = ?", q.WorkspaceID, q.AdAccountID)
	if q.Level != "" {
		if !q.Level.Valid() {
			return nil, fmt.Errorf("advertising repository: unknown level %q", q.Level)
		}
		query = query.Where("level = ?", string(q.Level))
	}
	if len(q.CampaignIDs) > 0 {
		query = query.Where("campaign_meta_id IN ?", q.CampaignIDs)
	}
	if len(q.AdSetIDs) > 0 {
		query = query.Where("adset_meta_id IN ?", q.AdSetIDs)
	}
	if len(q.MetaIDs) > 0 {
		query = query.Where("meta_id IN ?", q.MetaIDs)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		query = query.Where(`name ILIKE ? ESCAPE '\'`, "%"+escapeLike(search)+"%")
	}
	if !q.IncludeRemoved {
		query = query.Where("removed = ?", false)
	}
	var records []schema.AdObject
	if err := query.Order("created_time DESC NULLS LAST, meta_id").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*advertising.Object, 0, len(records))
	for i := range records {
		object, err := toObject(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, object)
	}
	return out, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }

func toObjectRecord(o *advertising.Object) (*schema.AdObject, error) {
	creative, err := encodeJSON(o.Creative)
	if err != nil {
		return nil, err
	}
	feedback, err := encodeJSON(o.ReviewFeedback)
	if err != nil {
		return nil, err
	}
	issues, err := encodeJSON(o.Issues)
	if err != nil {
		return nil, err
	}
	changes, err := encodeJSON(o.BudgetChanges)
	if err != nil {
		return nil, err
	}
	return &schema.AdObject{
		MetaID:           o.MetaID,
		WorkspaceID:      o.WorkspaceID,
		AdAccountID:      o.AdAccountID,
		Level:            string(o.Level),
		CampaignMetaID:   o.CampaignMetaID,
		AdSetMetaID:      o.AdSetMetaID,
		Name:             o.Name,
		Status:           string(o.Status),
		EffectiveStatus:  string(o.EffectiveStatus),
		Objective:        o.Objective,
		SpecialCategory:  string(o.SpecialCategory),
		DestinationType:  o.DestinationType,
		OptimizationGoal: o.OptimizationGoal,
		BidStrategy:      o.BidStrategy,
		DailyBudget:      o.DailyBudget,
		LifetimeBudget:   o.LifetimeBudget,
		BudgetRemaining:  o.BudgetRemaining,
		StartTime:        o.StartTime,
		EndTime:          o.EndTime,
		Creative:         creative,
		ReviewFeedback:   feedback,
		Issues:           issues,
		BudgetChanges:    changes,
		CreatedTime:      o.CreatedTime,
		UpdatedTime:      o.UpdatedTime,
		SyncedAt:         o.SyncedAt,
	}, nil
}

func toObject(r *schema.AdObject) (*advertising.Object, error) {
	o := &advertising.Object{
		MetaID:           r.MetaID,
		WorkspaceID:      r.WorkspaceID,
		AdAccountID:      r.AdAccountID,
		Level:            advertising.Level(r.Level),
		CampaignMetaID:   r.CampaignMetaID,
		AdSetMetaID:      r.AdSetMetaID,
		Name:             r.Name,
		Status:           advertising.ConfiguredStatus(r.Status),
		EffectiveStatus:  advertising.EffectiveStatus(r.EffectiveStatus),
		Objective:        r.Objective,
		SpecialCategory:  advertising.SpecialCategory(r.SpecialCategory),
		DestinationType:  r.DestinationType,
		OptimizationGoal: r.OptimizationGoal,
		BidStrategy:      r.BidStrategy,
		DailyBudget:      r.DailyBudget,
		LifetimeBudget:   r.LifetimeBudget,
		BudgetRemaining:  r.BudgetRemaining,
		StartTime:        r.StartTime,
		EndTime:          r.EndTime,
		CreatedTime:      r.CreatedTime,
		UpdatedTime:      r.UpdatedTime,
		SyncedAt:         r.SyncedAt,
	}
	if len(r.Creative) > 0 {
		o.Creative = &advertising.Creative{}
		if err := decodeJSON(r.Creative, o.Creative); err != nil {
			return nil, err
		}
	}
	if err := decodeJSON(r.ReviewFeedback, &o.ReviewFeedback); err != nil {
		return nil, err
	}
	if err := decodeJSON(r.Issues, &o.Issues); err != nil {
		return nil, err
	}
	if err := decodeJSON(r.BudgetChanges, &o.BudgetChanges); err != nil {
		return nil, err
	}
	return o, nil
}
