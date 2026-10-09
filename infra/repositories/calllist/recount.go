package calllist_repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/calls/calllist"
	"vozko/infra/database"
)

const firstListID = "00000000-0000-0000-0000-000000000000"

var errRecountCursor = errors.New("call list recount: the cursor must be a list id and the page size positive")

const (
	recountPageSQL = "SELECT id::text FROM call_lists WHERE id > ?::uuid ORDER BY id LIMIT ?"

	lockListSQL = "SELECT id FROM call_lists WHERE id = ?::uuid FOR UPDATE"

	recountSQL = "UPDATE call_lists SET called_count = s.called, callback_count = s.callbacks FROM (" +
		"SELECT count(*) FILTER (WHERE last_call_id IS NOT NULL) AS called," +
		" count(*) FILTER (WHERE state <> '" + string(calllist.StateClosed) + "' AND disposition = '" + calllist.DispositionCallback + "') AS callbacks" +
		" FROM call_list_items WHERE list_id = ?::uuid) s" +
		" WHERE call_lists.id = ?::uuid AND (call_lists.called_count, call_lists.callback_count) IS DISTINCT FROM (s.called, s.callbacks)"
)

func (s *Store) RecountProgress(ctx context.Context, after string, limit int) (string, int, error) {
	if after == "" {
		after = firstListID
	}
	if limit <= 0 || len(database.UUIDArray([]string{after})) != 1 {
		return "", 0, errRecountCursor
	}
	var ids []string
	if err := s.db.WithContext(ctx).Raw(recountPageSQL, after, limit).Scan(&ids).Error; err != nil {
		return "", 0, fmt.Errorf("call lists to recount: %w", err)
	}
	recounted := 0
	for _, id := range ids {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(lockListSQL, id).Error; err != nil {
				return err
			}
			result := tx.Exec(recountSQL, id, id)
			if result.Error != nil {
				return result.Error
			}
			recounted += int(result.RowsAffected)
			return nil
		})
		if err != nil {
			return "", recounted, fmt.Errorf("recount call list %s: %w", id, err)
		}
	}
	if len(ids) == 0 {
		return "", recounted, nil
	}
	return ids[len(ids)-1], recounted, nil
}
