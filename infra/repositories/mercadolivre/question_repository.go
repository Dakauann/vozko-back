package mercadolivre_repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const upsertQuestionSQL = `INSERT INTO mercadolivre_questions (id, workspace_id, account_id, question_id, item_id, contact_id, text, status, unanswered, ` +
	`answer_text, answer_status, answered_by, answered_at, asked_at, suspected_spam, last_fetched_at, created_at, updated_at) ` +
	`VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, now(), now()) ` +
	`ON CONFLICT (account_id, question_id) DO UPDATE SET ` +
	`item_id = EXCLUDED.item_id, contact_id = EXCLUDED.contact_id, text = EXCLUDED.text, status = EXCLUDED.status, unanswered = EXCLUDED.unanswered, ` +
	`answer_text = EXCLUDED.answer_text, answer_status = EXCLUDED.answer_status, ` +
	`answered_by = CASE WHEN mercadolivre_questions.answered_by <> '' THEN mercadolivre_questions.answered_by ELSE EXCLUDED.answered_by END, ` +
	`answered_at = COALESCE(EXCLUDED.answered_at, mercadolivre_questions.answered_at), ` +
	`suspected_spam = EXCLUDED.suspected_spam, last_fetched_at = EXCLUDED.last_fetched_at, updated_at = now() ` +
	`WHERE mercadolivre_questions.last_fetched_at <= EXCLUDED.last_fetched_at ` +
	`RETURNING id, (xmax = 0) AS created`

type questionRepository struct {
	db *gorm.DB
}

func NewQuestionRepository(db *gorm.DB) mldomain.QuestionRepository {
	return &questionRepository{db: db}
}

func (r *questionRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.MercadoLivreQuestion{})
}

type upsertOutcome struct {
	ID      string `gorm:"column:id"`
	Created bool   `gorm:"column:created"`
}

func (r *questionRepository) Upsert(ctx context.Context, q *mldomain.Question) (bool, error) {
	if q == nil || !mldomain.ValidNumericID(q.QuestionID) || !mldomain.ValidItemID(q.ItemID) || strings.TrimSpace(q.AccountID) == "" {
		return false, mldomain.ErrInvalidResource
	}
	if _, err := mldomain.ParseQuestionStatus(string(q.Status)); err != nil {
		return false, err
	}
	id := q.ID
	if id == "" {
		id = uuid.NewString()
	}
	var out []upsertOutcome
	if err := r.db.WithContext(ctx).Raw(upsertQuestionSQL,
		id, q.WorkspaceID, q.AccountID, q.QuestionID, q.ItemID, q.ContactID, q.Text, string(q.Status), q.Unanswered(),
		q.AnswerText, string(q.AnswerStatus), string(q.AnsweredBy), q.AnsweredAt, q.AskedAt, q.SuspectedSpam, q.LastFetchedAt,
	).Scan(&out).Error; err != nil {
		return false, err
	}
	if len(out) == 0 {
		return false, nil
	}
	q.ID = out[0].ID
	return out[0].Created, nil
}

func (r *questionRepository) FindByQuestionID(ctx context.Context, accountID, questionID string) (*mldomain.Question, error) {
	var record schema.MercadoLivreQuestion
	if err := r.db.WithContext(ctx).Where("account_id = ? AND question_id = ?", accountID, questionID).Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, mldomain.ErrQuestionNotFound
	}
	return toQuestionDomain(&record), nil
}

func (r *questionRepository) FindByQuestionIDs(ctx context.Context, accountID string, questionIDs []string) ([]*mldomain.Question, error) {
	unique := shared.UniqueNonEmpty(questionIDs)
	out := make([]*mldomain.Question, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.MercadoLivreQuestion
		if err := r.db.WithContext(ctx).Where("account_id = ? AND question_id IN ?", accountID, unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toQuestionDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *questionRepository) Forget(ctx context.Context, accountID, questionID string) error {
	return r.db.WithContext(ctx).Where("account_id = ? AND question_id = ?", accountID, questionID).Delete(&schema.MercadoLivreQuestion{}).Error
}

func (r *questionRepository) List(ctx context.Context, in mldomain.ListQuestionsInput) (*mldomain.QuestionList, error) {
	limit := mldomain.NormalizeListLimit(in.Limit)
	query := r.model(ctx).Where("account_id = ?", in.AccountID)
	if in.ItemID != "" {
		query = query.Where("item_id = ?", in.ItemID)
	}
	if in.Status != nil {
		query = query.Where("status = ?", string(*in.Status))
		if in.After != nil {
			query = query.Where("(asked_at, id) < (?, ?)", in.After.AskedAt, in.After.ID)
		}
		query = query.Order("asked_at DESC").Order("id DESC")
	} else {
		if in.After != nil {
			query = query.Where("(unanswered, asked_at, id) < (?, ?, ?)", in.After.Unanswered, in.After.AskedAt, in.After.ID)
		}
		query = query.Order("unanswered DESC").Order("asked_at DESC").Order("id DESC")
	}
	var records []schema.MercadoLivreQuestion
	if err := query.Limit(limit + 1).Find(&records).Error; err != nil {
		return nil, err
	}
	out := &mldomain.QuestionList{Items: make([]*mldomain.Question, 0, min(len(records), limit))}
	for i := range records {
		if i == limit {
			next := mldomain.CursorOf(out.Items[limit-1])
			out.Next = &next
			break
		}
		out.Items = append(out.Items, toQuestionDomain(&records[i]))
	}
	return out, nil
}

func (r *questionRepository) RecordAnswer(ctx context.Context, accountID, questionID string, a mldomain.AnswerRecord) error {
	if !a.AnsweredBy.Valid() {
		return mldomain.ErrInvalidResource
	}
	at := a.AnsweredAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return database.AffectedOne(r.model(ctx).Where("account_id = ? AND question_id = ?", accountID, questionID).Updates(map[string]any{
		"status":              string(mldomain.QuestionAnswered),
		"unanswered":          false,
		"answer_text":         a.Text,
		"answer_status":       string(a.Status),
		"answered_by":         string(a.AnsweredBy),
		"answered_by_user_id": a.AnsweredByUserID,
		"rule_id":             a.RuleID,
		"answered_at":         at,
	}), mldomain.ErrQuestionNotFound)
}

func (r *questionRepository) SetStatus(ctx context.Context, accountID, questionID string, status mldomain.QuestionStatus) error {
	if _, err := mldomain.ParseQuestionStatus(string(status)); err != nil {
		return err
	}
	return database.AffectedOne(r.model(ctx).Where("account_id = ? AND question_id = ?", accountID, questionID).Updates(map[string]any{
		"status":     string(status),
		"unanswered": status == mldomain.QuestionUnanswered,
	}), mldomain.ErrQuestionNotFound)
}

func toQuestionDomain(r *schema.MercadoLivreQuestion) *mldomain.Question {
	return &mldomain.Question{
		ID:               r.ID,
		WorkspaceID:      r.WorkspaceID,
		AccountID:        r.AccountID,
		ItemID:           r.ItemID,
		QuestionID:       r.QuestionID,
		ContactID:        r.ContactID,
		Text:             r.Text,
		Status:           mldomain.QuestionStatus(r.Status),
		AnswerText:       r.AnswerText,
		AnswerStatus:     mldomain.AnswerStatus(r.AnswerStatus),
		AnsweredBy:       mldomain.AnswerSource(r.AnsweredBy),
		AnsweredByUserID: r.AnsweredByUserID,
		RuleID:           r.RuleID,
		AskedAt:          r.AskedAt,
		AnsweredAt:       r.AnsweredAt,
		SuspectedSpam:    r.SuspectedSpam,
		LastFetchedAt:    r.LastFetchedAt,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}
