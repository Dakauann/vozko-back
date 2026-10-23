package emailhygiene

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	h "vozko/domain/emailhygiene"
)

const KeepDisposableLists = 3

type DisposableDeps struct {
	Store  h.DisposableListStore
	Source h.DisposableSource
}

type Disposable struct {
	d       DisposableDeps
	current atomic.Pointer[h.DisposableSet]
	loaded  atomic.Pointer[time.Time]
	now     func() time.Time
}

func NewDisposable(d DisposableDeps, snapshot string) (*Disposable, error) {
	set, err := h.ParseDisposableList(snapshot)
	if err != nil {
		return nil, fmt.Errorf("emailhygiene: the embedded disposable snapshot: %w", err)
	}
	out := &Disposable{d: d, now: func() time.Time { return time.Now().UTC() }}
	out.current.Store(set)
	return out, nil
}

func (s *Disposable) Set() *h.DisposableSet {
	return s.current.Load()
}

func (s *Disposable) Load(ctx context.Context) error {
	stored, err := s.d.Store.Latest(ctx)
	if err != nil {
		return fmt.Errorf("emailhygiene: reading the stored disposable list: %w", err)
	}
	if stored == nil {
		return nil
	}
	if loaded := s.loaded.Load(); loaded != nil && !stored.FetchedAt.After(*loaded) {
		return nil
	}
	set, err := h.ParseDisposableList(stored.Content)
	if err != nil {
		return fmt.Errorf("emailhygiene: the stored disposable list of %s: %w", stored.FetchedAt.Format(time.RFC3339), err)
	}
	s.current.Store(set)
	fetched := stored.FetchedAt
	s.loaded.Store(&fetched)
	return nil
}

func (s *Disposable) Reload(ctx context.Context) {
	if err := s.Load(ctx); err != nil {
		log.Printf("[emailhygiene] disposable list reload kept the current list: %v", err)
	}
}

func (s *Disposable) Execute(ctx context.Context) error {
	return s.Refresh(ctx)
}

func (s *Disposable) Refresh(ctx context.Context) error {
	content, err := s.d.Source.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("emailhygiene: downloading the disposable list: %w", err)
	}
	set, err := h.ParseDisposableList(content)
	if err != nil {
		return fmt.Errorf("emailhygiene: the downloaded disposable list was refused: %w", err)
	}
	now := s.now()
	if err := s.d.Store.Save(ctx, content, set.Len(), now); err != nil {
		return err
	}
	s.current.Store(set)
	s.loaded.Store(&now)
	if _, err := s.d.Store.KeepNewest(ctx, KeepDisposableLists); err != nil {
		log.Printf("[emailhygiene] old disposable lists were not pruned: %v", err)
	}
	return nil
}
