package entrypurge

import (
	"context"
	"fmt"
	"strings"

	domain "vozko/domain/entrypurge"
	"vozko/domain/shared"
)

type Purger struct {
	store domain.Store
	files domain.Files
}

func New(store domain.Store, files domain.Files) *Purger {
	if store == nil || files == nil {
		panic("entry purge: the store and the file storage are required")
	}
	return &Purger{store: store, files: files}
}

func (p *Purger) PurgeEntries(ctx context.Context, entryType shared.EntryType, entryIDs []string) error {
	ids, err := batch(entryType, entryIDs)
	if err != nil || len(ids) == 0 {
		return err
	}
	urls, err := p.store.MediaURLs(ctx, entryType, ids)
	if err != nil {
		return fmt.Errorf("entry purge: list the media of %s entries: %w", entryType, err)
	}
	keys := make([]string, 0, len(urls))
	for _, url := range urls {
		if key, ours := p.files.KeyFromURL(url); ours {
			keys = append(keys, key)
		}
	}
	if err := p.DeleteFiles(ctx, keys); err != nil {
		return err
	}
	return p.store.DeleteEntries(ctx, entryType, ids)
}

func (p *Purger) PurgeContacts(ctx context.Context, contactType shared.EntryType, contactIDs []string) error {
	ids, err := batch(contactType, contactIDs)
	if err != nil || len(ids) == 0 {
		return err
	}
	return p.store.DeleteContacts(ctx, contactType, ids)
}

func (p *Purger) DeleteFiles(ctx context.Context, keys []string) error {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if err := p.files.DeleteFile(ctx, key); err != nil {
			return fmt.Errorf("entry purge: delete file %s: %w", key, err)
		}
	}
	return nil
}

func batch(entryType shared.EntryType, raw []string) ([]string, error) {
	if !entryType.IsKnown() {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnknownType, entryType)
	}
	seen := make(map[string]struct{}, len(raw))
	ids := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) > domain.MaxBatch {
		return nil, fmt.Errorf("%w: %d", domain.ErrBatchTooLarge, len(ids))
	}
	return ids, nil
}
