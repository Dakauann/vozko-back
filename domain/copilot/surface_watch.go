package copilot

import "context"

type SurfaceWatch interface {
	ViewSnapshot(ctx context.Context, cc Context) string
	ClosingNote(ctx context.Context, cc Context, reply string) string
}
