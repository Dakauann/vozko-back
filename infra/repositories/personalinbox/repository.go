package personalinbox_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/personalinbox"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

var _ personalinbox.Repository = (*Repository)(nil)

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Find(ctx context.Context, userID string, entry shared.EntryRef) (personalinbox.Mark, error) {
	mark, err := personalinbox.NewMark(userID, entry)
	if err != nil {
		return personalinbox.Mark{}, err
	}
	var row schema.ConversationPersonalMark
	err = r.db.WithContext(ctx).Where(keyOf(mark)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mark, nil
	}
	if err != nil {
		return personalinbox.Mark{}, err
	}
	return markOf(row), nil
}

func (r *Repository) Change(ctx context.Context, userID string, entry shared.EntryRef, change personalinbox.Change) (before, after personalinbox.Mark, err error) {
	start, err := personalinbox.NewMark(userID, entry)
	if err != nil {
		return personalinbox.Mark{}, personalinbox.Mark{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := keyOf(start)
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&key).Error; err != nil {
			return err
		}
		var row schema.ConversationPersonalMark
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(keyOf(start)).Take(&row).Error; err != nil {
			return err
		}
		before = markOf(row)
		next, err := change(before)
		if err != nil {
			return err
		}
		after = next
		return store(tx, start, after)
	})
	if err != nil {
		return personalinbox.Mark{}, personalinbox.Mark{}, err
	}
	return before, after, nil
}

func store(tx *gorm.DB, key, mark personalinbox.Mark) error {
	if mark.Empty() {
		return tx.Where(keyOf(key)).Delete(&schema.ConversationPersonalMark{}).Error
	}
	return tx.Model(&schema.ConversationPersonalMark{}).Where(keyOf(key)).Updates(map[string]interface{}{
		"pinned_at":        mark.PinnedAt,
		"unread_marked_at": mark.UnreadMarkedAt,
		"updated_at":       time.Now().UTC(),
	}).Error
}

func (r *Repository) ForEntries(ctx context.Context, userID string, entries []shared.EntryRef) ([]personalinbox.Mark, error) {
	if userID == "" || len(entries) == 0 {
		return nil, nil
	}
	pairs := make([][]interface{}, 0, len(entries))
	for _, e := range entries {
		pairs = append(pairs, []interface{}{e.EntryID, string(e.EntryType)})
	}
	var rows []schema.ConversationPersonalMark
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND (entry_id, entry_type) IN ?", userID, pairs).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return marksOf(rows), nil
}

func keyOf(m personalinbox.Mark) schema.ConversationPersonalMark {
	return schema.ConversationPersonalMark{UserID: m.UserID, EntryType: string(m.EntryType), EntryID: m.EntryID}
}

func markOf(row schema.ConversationPersonalMark) personalinbox.Mark {
	return personalinbox.Mark{
		UserID:         row.UserID,
		EntryType:      shared.EntryType(row.EntryType),
		EntryID:        row.EntryID,
		PinnedAt:       inUTC(row.PinnedAt),
		UnreadMarkedAt: inUTC(row.UnreadMarkedAt),
	}
}

func marksOf(rows []schema.ConversationPersonalMark) []personalinbox.Mark {
	out := make([]personalinbox.Mark, 0, len(rows))
	for _, row := range rows {
		out = append(out, markOf(row))
	}
	return out
}

func inUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
