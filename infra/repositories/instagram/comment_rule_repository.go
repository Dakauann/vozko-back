package instagram_repository

import (
	"context"
	"errors"

	"github.com/lib/pq"
	"gorm.io/gorm"

	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
)

type commentRuleRepository struct {
	db *gorm.DB
}

func NewCommentRuleRepository(db *gorm.DB) igdomain.CommentRuleRepository {
	return &commentRuleRepository{db: db}
}

func (r *commentRuleRepository) Create(ctx context.Context, rule *igdomain.CommentRule) error {
	record := toCommentRuleSchema(rule)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	rule.ID = record.ID
	rule.CreatedAt = record.CreatedAt
	rule.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *commentRuleRepository) Update(ctx context.Context, rule *igdomain.CommentRule) error {
	// Scoped by workspace as well as id: a rule id from another tenant must not
	// be updatable even if it is guessed.
	result := r.db.WithContext(ctx).
		Model(&schema.InstagramCommentRule{}).
		Where("id = ? AND workspace_id = ?", rule.ID, rule.WorkspaceID).
		Updates(map[string]interface{}{
			"name":               rule.Name,
			"enabled":            rule.Enabled,
			"ig_media_id":        rule.IGMediaID,
			"match":              string(rule.Match),
			"keywords":           pq.StringArray(rule.Keywords),
			"actions":            pq.StringArray(actionsToStrings(rule.Actions)),
			"public_reply_text":  rule.PublicReplyText,
			"private_reply_text": rule.PrivateReplyText,
			"priority":           rule.Priority,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrCommentRuleNotFound
	}
	return nil
}

func (r *commentRuleRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		Delete(&schema.InstagramCommentRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrCommentRuleNotFound
	}
	return nil
}

func (r *commentRuleRepository) FindByID(ctx context.Context, workspaceID, id string) (*igdomain.CommentRule, error) {
	var record schema.InstagramCommentRule
	if err := r.db.WithContext(ctx).
		First(&record, "id = ? AND workspace_id = ?", id, workspaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrCommentRuleNotFound
		}
		return nil, err
	}
	return toCommentRuleDomain(&record), nil
}

func (r *commentRuleRepository) ListByAccount(ctx context.Context, workspaceID, igAccountID string) ([]*igdomain.CommentRule, error) {
	var records []schema.InstagramCommentRule
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND ig_account_id = ?", workspaceID, igAccountID).
		Order("priority ASC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toCommentRuleDomainList(records), nil
}

// ListCandidates returns the enabled rules that could fire for one post: those
// scoped to it plus the account-wide defaults.
//
// Both tiers come back in one query, ordered so a post-scoped rule is evaluated
// before a default of the same priority, the specific rule should win.
func (r *commentRuleRepository) ListCandidates(ctx context.Context, igAccountID, igMediaID string) ([]*igdomain.CommentRule, error) {
	var records []schema.InstagramCommentRule
	if err := r.db.WithContext(ctx).
		Where("ig_account_id = ? AND enabled = true AND (ig_media_id = ? OR ig_media_id = '')", igAccountID, igMediaID).
		Order("priority ASC, (ig_media_id <> '') DESC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toCommentRuleDomainList(records), nil
}

func toCommentRuleDomainList(records []schema.InstagramCommentRule) []*igdomain.CommentRule {
	out := make([]*igdomain.CommentRule, 0, len(records))
	for i := range records {
		out = append(out, toCommentRuleDomain(&records[i]))
	}
	return out
}

func toCommentRuleSchema(rule *igdomain.CommentRule) *schema.InstagramCommentRule {
	return &schema.InstagramCommentRule{
		ID:               rule.ID,
		WorkspaceID:      rule.WorkspaceID,
		IGAccountID:      rule.IGAccountID,
		Name:             rule.Name,
		Enabled:          rule.Enabled,
		IGMediaID:        rule.IGMediaID,
		Match:            string(rule.Match),
		Keywords:         pq.StringArray(rule.Keywords),
		Actions:          pq.StringArray(actionsToStrings(rule.Actions)),
		PublicReplyText:  rule.PublicReplyText,
		PrivateReplyText: rule.PrivateReplyText,
		Priority:         rule.Priority,
	}
}

func toCommentRuleDomain(record *schema.InstagramCommentRule) *igdomain.CommentRule {
	actions := make([]igdomain.CommentRuleAction, 0, len(record.Actions))
	for _, a := range record.Actions {
		actions = append(actions, igdomain.CommentRuleAction(a))
	}
	return &igdomain.CommentRule{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		IGAccountID:      record.IGAccountID,
		Name:             record.Name,
		Enabled:          record.Enabled,
		IGMediaID:        record.IGMediaID,
		Match:            igdomain.CommentRuleMatch(record.Match),
		Keywords:         []string(record.Keywords),
		Actions:          actions,
		PublicReplyText:  record.PublicReplyText,
		PrivateReplyText: record.PrivateReplyText,
		Priority:         record.Priority,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}

func actionsToStrings(actions []igdomain.CommentRuleAction) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, string(a))
	}
	return out
}
