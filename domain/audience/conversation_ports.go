package audience

import (
	"context"
	"time"
)

type Transcript struct {
	Text          string
	MessageCount  int
	LastMessageAt time.Time
}

type ConversationAdapter interface {
	ReadTranscripts(ctx context.Context, ref ContainerRef, entryIDs []string) (map[string]Transcript, error)

	ReadContainerContext(ctx context.Context, ref ContainerRef) (ContainerContext, error)
}

type ConversationReader interface {
	LatestByEntries(ctx context.Context, workspaceID string, source Source, entryIDs []string) (map[string]*Analysis, error)

	LatestByEntry(ctx context.Context, workspaceID string, source Source, entryID string) (*Analysis, error)

	PendingByEntries(ctx context.Context, workspaceID string, source Source, entryIDs []string) (map[string]bool, error)
}
