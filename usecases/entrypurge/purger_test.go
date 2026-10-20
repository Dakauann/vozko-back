package entrypurge

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	domain "vozko/domain/entrypurge"
	"vozko/domain/shared"
)

type fakeStore struct {
	urls         []string
	urlErr       error
	deleteErr    error
	deleted      [][]string
	deletedTypes []shared.EntryType
	contacts     [][]string
	contactTypes []shared.EntryType
	deleteCalls  int
}

func (f *fakeStore) MediaURLs(_ context.Context, _ shared.EntryType, _ []string) ([]string, error) {
	return f.urls, f.urlErr
}

func (f *fakeStore) DeleteEntries(_ context.Context, t shared.EntryType, ids []string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted, f.deletedTypes = append(f.deleted, ids), append(f.deletedTypes, t)
	return nil
}

func (f *fakeStore) DeleteContacts(_ context.Context, t shared.EntryType, ids []string) error {
	f.contacts, f.contactTypes = append(f.contacts, ids), append(f.contactTypes, t)
	return nil
}

type fakeFiles struct {
	deleted []string
	failOn  string
}

func (f *fakeFiles) KeyFromURL(url string) (string, bool) {
	const prefix = "https://files.example/"
	if !strings.HasPrefix(url, prefix) {
		return "", false
	}
	return strings.TrimPrefix(url, prefix), true
}

func (f *fakeFiles) DeleteFile(_ context.Context, key string) error {
	if key == f.failOn {
		return errors.New("storage down")
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func TestEntriesLoseTheirFilesBeforeTheirRows(t *testing.T) {
	store := &fakeStore{urls: []string{"https://files.example/a.jpg", "https://elsewhere.example/b.jpg", "https://files.example/c.pdf"}}
	files := &fakeFiles{}
	p := New(store, files)
	if err := p.PurgeEntries(context.Background(), shared.EntryTypeMercadoLivre, []string{"e1", "e2", "e1"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(files.deleted, ",") != "a.jpg,c.pdf" {
		t.Fatalf("files deleted = %v", files.deleted)
	}
	if len(store.deleted) != 1 || strings.Join(store.deleted[0], ",") != "e1,e2" || store.deletedTypes[0] != shared.EntryTypeMercadoLivre {
		t.Fatalf("rows deleted = %v %v", store.deleted, store.deletedTypes)
	}
}

func TestAFileThatCannotBeDeletedKeepsTheRowsForTheNextRun(t *testing.T) {
	store := &fakeStore{urls: []string{"https://files.example/a.jpg"}}
	p := New(store, &fakeFiles{failOn: "a.jpg"})
	if err := p.PurgeEntries(context.Background(), shared.EntryTypeMercadoLivre, []string{"e1"}); err == nil {
		t.Fatal("a storage failure must fail the batch")
	}
	if store.deleteCalls != 0 {
		t.Fatal("rows must outlive their files, or the file keys are lost")
	}

	listing := &fakeStore{urlErr: errors.New("db down")}
	if err := New(listing, &fakeFiles{}).PurgeEntries(context.Background(), shared.EntryTypeMercadoLivre, []string{"e1"}); err == nil || listing.deleteCalls != 0 {
		t.Fatalf("an unreadable media list must stop the batch: %v", err)
	}
}

func TestPurgeBatchesAreBoundedAndTyped(t *testing.T) {
	p := New(&fakeStore{}, &fakeFiles{})
	ctx := context.Background()
	if err := p.PurgeEntries(ctx, "unknown_channel", []string{"e1"}); !errors.Is(err, domain.ErrUnknownType) {
		t.Fatalf("unknown type: %v", err)
	}
	if err := p.PurgeContacts(ctx, "unknown_channel", []string{"c1"}); !errors.Is(err, domain.ErrUnknownType) {
		t.Fatalf("unknown contact type: %v", err)
	}
	big := make([]string, domain.MaxBatch+1)
	for i := range big {
		big[i] = "e" + strconv.Itoa(i)
	}
	if err := p.PurgeEntries(ctx, shared.EntryTypeMercadoLivre, big); !errors.Is(err, domain.ErrBatchTooLarge) {
		t.Fatalf("oversized batch: %v", err)
	}
	if err := p.PurgeEntries(ctx, shared.EntryTypeMercadoLivre, []string{" ", ""}); err != nil {
		t.Fatalf("an empty batch is a no-op: %v", err)
	}
}

func TestContactsArePurgedByTheirChannel(t *testing.T) {
	store := &fakeStore{}
	if err := New(store, &fakeFiles{}).PurgeContacts(context.Background(), shared.EntryTypeMercadoLivre, []string{"c1", "c2"}); err != nil {
		t.Fatal(err)
	}
	if len(store.contacts) != 1 || store.contactTypes[0] != shared.EntryTypeMercadoLivre || len(store.contacts[0]) != 2 {
		t.Fatalf("contacts = %v", store.contacts)
	}
}

func TestStoredFilesAreDeletedByKey(t *testing.T) {
	files := &fakeFiles{}
	if err := New(&fakeStore{}, files).DeleteFiles(context.Background(), []string{"k1", "", "k2", "k1"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(files.deleted, ",") != "k1,k2" {
		t.Fatalf("deleted = %v", files.deleted)
	}
}

func TestThePurgerRefusesMissingDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a purger without storage must not be built")
		}
	}()
	New(&fakeStore{}, nil)
}
