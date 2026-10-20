package entrypurge

import (
	"context"
	"errors"

	"vozko/domain/shared"
)

const MaxBatch = 1000

var (
	ErrBatchTooLarge = errors.New("entry purge: a batch holds at most 1000 ids")
	ErrUnknownType   = errors.New("entry purge: unknown entry type")
)

type Store interface {
	MediaURLs(ctx context.Context, entryType shared.EntryType, entryIDs []string) ([]string, error)
	DeleteEntries(ctx context.Context, entryType shared.EntryType, entryIDs []string) error
	DeleteContacts(ctx context.Context, contactType shared.EntryType, contactIDs []string) error
}

type Files interface {
	KeyFromURL(url string) (string, bool)
	DeleteFile(ctx context.Context, key string) error
}
