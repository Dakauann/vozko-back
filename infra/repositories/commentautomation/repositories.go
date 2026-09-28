package commentautomation_repository

import (
	"context"
	"errors"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type ruleRepository struct {
	db *gorm.DB
}

func NewRuleRepository(db *gorm.DB) ca.Repository {
	return &ruleRepository{db: db}
}

func (r *ruleRepository) Create(ctx context.Context, rule *ca.Rule) error {
	record := toRuleRecord(rule)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	rule.ID, rule.CreatedAt, rule.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *ruleRepository) Update(ctx context.Context, rule *ca.Rule) error {
	result := r.db.WithContext(ctx).Model(&schema.CommentRule{}).
		Where("id = ? AND workspace_id = ?", rule.ID, rule.WorkspaceID).
		Updates(map[string]any{
			"name":               rule.Name,
			"enabled":            rule.Enabled,
			"container_id":       rule.ContainerID,
			"match":              string(rule.Match),
			"keywords":           pq.StringArray(rule.Keywords),
			"actions":            pq.StringArray(actionStrings(rule.Actions)),
			"public_reply_text":  rule.PublicReplyText,
			"private_reply_text": rule.PrivateReplyText,
			"priority":           rule.Priority,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ca.ErrRuleNotFound
	}
	return nil
}

func (r *ruleRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.CommentRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ca.ErrRuleNotFound
	}
	return nil
}

func (r *ruleRepository) FindByID(ctx context.Context, workspaceID, id string) (*ca.Rule, error) {
	var record schema.CommentRule
	if err := r.db.WithContext(ctx).First(&record, "id = ? AND workspace_id = ?", id, workspaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrRuleNotFound
		}
		return nil, err
	}
	return toRuleDomain(&record), nil
}

func (r *ruleRepository) ListByAccount(ctx context.Context, workspaceID string, source shared.EntryType, accountID string) ([]*ca.Rule, error) {
	var records []schema.CommentRule
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND source = ? AND account_id = ?", workspaceID, string(source), accountID).
		Order("priority ASC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toRuleList(records), nil
}

func (r *ruleRepository) ListCandidates(ctx context.Context, source shared.EntryType, accountID, containerID string) ([]*ca.Rule, error) {
	var records []schema.CommentRule
	if err := r.db.WithContext(ctx).
		Where("source = ? AND account_id = ? AND enabled = true AND (container_id = ? OR container_id = '')", string(source), accountID, containerID).
		Order("priority ASC, (container_id <> '') DESC, created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toRuleList(records), nil
}

func toRuleList(records []schema.CommentRule) []*ca.Rule {
	out := make([]*ca.Rule, 0, len(records))
	for i := range records {
		out = append(out, toRuleDomain(&records[i]))
	}
	return out
}

func toRuleRecord(rule *ca.Rule) *schema.CommentRule {
	return &schema.CommentRule{
		ID:               rule.ID,
		WorkspaceID:      rule.WorkspaceID,
		Source:           string(rule.Source),
		AccountID:        rule.AccountID,
		ContainerID:      rule.ContainerID,
		Name:             rule.Name,
		Enabled:          rule.Enabled,
		Match:            string(rule.Match),
		Keywords:         pq.StringArray(rule.Keywords),
		Actions:          pq.StringArray(actionStrings(rule.Actions)),
		PublicReplyText:  rule.PublicReplyText,
		PrivateReplyText: rule.PrivateReplyText,
		Priority:         rule.Priority,
	}
}

func toRuleDomain(record *schema.CommentRule) *ca.Rule {
	actions := make([]ca.Action, 0, len(record.Actions))
	for _, a := range record.Actions {
		actions = append(actions, ca.Action(a))
	}
	return &ca.Rule{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		Source:           shared.EntryType(record.Source),
		AccountID:        record.AccountID,
		ContainerID:      record.ContainerID,
		Name:             record.Name,
		Enabled:          record.Enabled,
		Match:            ca.Match(record.Match),
		Keywords:         []string(record.Keywords),
		Actions:          actions,
		PublicReplyText:  record.PublicReplyText,
		PrivateReplyText: record.PrivateReplyText,
		Priority:         record.Priority,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}

func actionStrings(actions []ca.Action) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, string(a))
	}
	return out
}

type privateReplyRepository struct {
	db *gorm.DB
}

func NewPrivateReplyRepository(db *gorm.DB) privatereply.Repository {
	return &privateReplyRepository{db: db}
}

func (r *privateReplyRepository) Claim(ctx context.Context, source shared.EntryType, commentID, accountID string) (bool, error) {
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source"}, {Name: "comment_id"}}, DoNothing: true}).
		Create(&schema.CommentPrivateReply{
			Source: string(source), CommentID: commentID, AccountID: accountID, Status: string(privatereply.StatusAttempted),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *privateReplyRepository) MarkSent(ctx context.Context, source shared.EntryType, commentID, recipientRef, messageID string) error {
	updates := map[string]any{"status": string(privatereply.StatusSent), "updated_at": time.Now().UTC()}
	if recipientRef != "" {
		updates["recipient_ref"] = recipientRef
	}
	if messageID != "" {
		updates["message_id"] = messageID
	}
	return r.update(ctx, source, commentID, updates)
}

func (r *privateReplyRepository) MarkFailed(ctx context.Context, source shared.EntryType, commentID string, code int, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	return r.update(ctx, source, commentID, map[string]any{
		"status": string(privatereply.StatusFailed), "error_code": code, "error_message": message, "updated_at": time.Now().UTC(),
	})
}

func (r *privateReplyRepository) update(ctx context.Context, source shared.EntryType, commentID string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.CommentPrivateReply{}).
		Where("source = ? AND comment_id = ?", string(source), commentID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errPrivateReplyNotClaimed
	}
	return nil
}

var errPrivateReplyNotClaimed = errors.New("private reply was never claimed")

func (r *privateReplyRepository) Find(ctx context.Context, source shared.EntryType, commentID string) (*privatereply.Record, error) {
	var record schema.CommentPrivateReply
	if err := r.db.WithContext(ctx).First(&record, "source = ? AND comment_id = ?", string(source), commentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toReplyDomain(&record), nil
}

func (r *privateReplyRepository) FindMany(ctx context.Context, source shared.EntryType, commentIDs []string) (map[string]*privatereply.Record, error) {
	out := make(map[string]*privatereply.Record, len(commentIDs))
	if len(commentIDs) == 0 {
		return out, nil
	}
	var records []schema.CommentPrivateReply
	if err := r.db.WithContext(ctx).Where("source = ? AND comment_id IN ?", string(source), commentIDs).Find(&records).Error; err != nil {
		return nil, err
	}
	for i := range records {
		out[records[i].CommentID] = toReplyDomain(&records[i])
	}
	return out, nil
}

func toReplyDomain(r *schema.CommentPrivateReply) *privatereply.Record {
	return &privatereply.Record{
		Source: shared.EntryType(r.Source), CommentID: r.CommentID, AccountID: r.AccountID,
		Status: privatereply.Status(r.Status), RecipientRef: r.RecipientRef, MessageID: r.MessageID,
		ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage, AttemptedAt: r.AttemptedAt, UpdatedAt: r.UpdatedAt,
	}
}
