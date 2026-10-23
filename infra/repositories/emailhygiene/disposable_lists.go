package emailhygiene_repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailhygiene"
	"vozko/infra/database/schema"
)

const keepNewestSQL = `DELETE FROM email_disposable_lists WHERE id IN (
SELECT id FROM email_disposable_lists ORDER BY fetched_at DESC, id DESC OFFSET ?)`

type DisposableLists struct {
	db *gorm.DB
}

var _ emailhygiene.DisposableListStore = (*DisposableLists)(nil)

func NewDisposableLists(db *gorm.DB) *DisposableLists {
	return &DisposableLists{db: db}
}

func (s *DisposableLists) Latest(ctx context.Context) (*emailhygiene.StoredList, error) {
	var row schema.EmailDisposableList
	err := s.db.WithContext(ctx).Order("fetched_at DESC, id DESC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(row.Content))
	if hex.EncodeToString(sum[:]) != row.SHA256 {
		return nil, emailhygiene.ErrDisposableListInvalid
	}
	return &emailhygiene.StoredList{Content: row.Content, FetchedAt: row.FetchedAt}, nil
}

func (s *DisposableLists) Save(ctx context.Context, content string, domains int, at time.Time) error {
	sum := sha256.Sum256([]byte(content))
	return s.db.WithContext(ctx).Create(&schema.EmailDisposableList{
		ID: uuid.NewString(), FetchedAt: at.UTC(), Domains: domains, SHA256: hex.EncodeToString(sum[:]), Content: content,
	}).Error
}

func (s *DisposableLists) KeepNewest(ctx context.Context, keep int) (int64, error) {
	res := s.db.WithContext(ctx).Exec(keepNewestSQL, keep)
	return res.RowsAffected, res.Error
}
