package googlesheet

import (
	"context"
	"time"

	"vozko/domain/googlesheet"
)

type Clock func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

type Alerts interface {
	NeedsAttention(ctx context.Context, sheet *googlesheet.Sheet) error
	Resolved(ctx context.Context, sheet *googlesheet.Sheet) error
}

type Marker interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
	Del(keys ...string) error
}

type Nudger interface {
	Nudge(sheetID string)
}

type NameLookup func(ctx context.Context, workspaceID, id string) (string, error)

func headerMarkKey(sheetID string) string { return "gs:hdr:" + sheetID }
