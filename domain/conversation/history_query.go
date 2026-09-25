package conversation

import (
	"time"

	"vozko/domain/shared"
)

type Viewer struct {
	UserID      string
	WorkspaceID string
	IsAdmin     bool
}

type HistoryQuery struct {
	Viewer    Viewer
	EntryID   string
	EntryType shared.EntryType
	Before    *time.Time
	Limit     int
}

type HistoryPage struct {
	Messages []*Message
	HasMore  bool
	Total    int64
}

type HistoryReader interface {
	ReadHistory(q HistoryQuery) (HistoryPage, error)
}

func HistoryPageSize(requested int) int {
	if requested <= 0 {
		return DefaultHistoryPageSize
	}
	if requested > MaxHistoryPageSize {
		return MaxHistoryPageSize
	}
	return requested
}

type EntryLookup interface {
	LookupEntry(viewer Viewer, entryID string, entryType shared.EntryType) (*InboxEntry, error)
}
