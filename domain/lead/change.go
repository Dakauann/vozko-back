package lead

import (
	"context"

	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

type Change struct {
	WorkspaceID string
	LeadID      string
	Version     int64
	Fields      []string
}

type ChangeNotifier interface {
	LeadChanged(change Change)
}

type Store interface {
	FindByID(workspaceID, id string) (*Lead, error)
	Load(ctx context.Context, workspaceID, id string) (*Lead, error)
	Insert(ctx context.Context, l *Lead, events []recordevent.Event) error
	Save(ctx context.Context, l *Lead, expectedVersion int64, events []recordevent.Event) error
}

type EntryDirectory interface {
	EntryRefs(ctx context.Context, workspaceID, leadID string) ([]shared.EntryRef, error)
}

type EntryUsage interface {
	HasEntries(ctx context.Context, workspaceID, leadID string) (bool, error)
}

type DuplicateFinder interface {
	FindByNumbersOrAddresses(ctx context.Context, workspaceID string, numbers, fingerprints []string) ([]*Lead, error)
}

type EntryLeads interface {
	LeadOfEntry(ctx context.Context, workspaceID string, ref shared.EntryRef) (string, error)
}

type ContactDetails interface {
	AttachContactDetails(ctx context.Context, workspaceID string, leads []*Lead) error
}

type WhatsAppBlock interface {
	Apply(number string, block bool) error
}
