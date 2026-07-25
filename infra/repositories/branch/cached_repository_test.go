package branch_repository

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/branch"
	"vozko/domain/cache"
)

// countingRepo is a fake inner branch.Repository that records FindByGlobalSIPUser reads
// so the test can prove cache hits skip the DB, and serves a mutable branch.
type countingRepo struct {
	branch.Repository
	b        *branch.Branch
	notFound bool
	sipReads int
}

func (c *countingRepo) FindByGlobalSIPUser(string) (*branch.Branch, error) {
	c.sipReads++
	if c.notFound {
		return nil, branch.ErrBranchNotFound
	}
	return c.b, nil
}
func (c *countingRepo) FindByID(string) (*branch.Branch, error) { return c.b, nil }
func (c *countingRepo) UpdateRegistrationStatus(_ string, s branch.RegistrationStatus) error {
	c.b.RegistrationStatus = s
	return nil
}
func (c *countingRepo) Update(*branch.Branch) error { return nil }

// mapShared is a minimal in-memory SharedState (only the 3 ops the cache uses).
type mapShared struct {
	cache.SharedState
	m map[string]string
}

func newMapShared() *mapShared { return &mapShared{m: map[string]string{}} }
func (s *mapShared) SetString(k, v string, _ time.Duration) error {
	s.m[k] = v
	return nil
}
func (s *mapShared) GetString(k string) (string, error) { return s.m[k], nil }
func (s *mapShared) Del(keys ...string) error {
	for _, k := range keys {
		delete(s.m, k)
	}
	return nil
}

func testBranch() *branch.Branch {
	b := branch.NewBranch("branch-1001", "ws-1", "member-1001", "user-1001", "1001", "Desk 1001")
	_ = b.Validate()
	b.SetSecret("vozko", "s3cret")
	b.Enabled = true
	b.RegistrationStatus = branch.RegistrationStatusRegistered
	return b
}

func TestCachedRepo_HitSkipsDB(t *testing.T) {
	inner := &countingRepo{b: testBranch()}
	repo := NewCachedRepository(inner, newMapShared())

	// First read populates the cache (1 DB read); the HA1 credential must survive.
	got, _ := repo.FindByGlobalSIPUser("1001")
	if got == nil || got.SecretHA1 != inner.b.SecretHA1 {
		t.Fatalf("first read lost the HA1 secret: got=%+v", got)
	}
	// Subsequent reads are served from cache: no more DB reads.
	for i := 0; i < 5; i++ {
		_, _ = repo.FindByGlobalSIPUser("1001")
	}
	if inner.sipReads != 1 {
		t.Fatalf("sipReads = %d, want 1 (5 later reads should hit cache)", inner.sipReads)
	}
}

func TestCachedRepo_NegativeCacheForUnknown(t *testing.T) {
	inner := &countingRepo{notFound: true}
	repo := NewCachedRepository(inner, newMapShared())

	// First unknown lookup hits the DB and returns not-found (1 read).
	if _, err := repo.FindByGlobalSIPUser("9999"); !errors.Is(err, branch.ErrBranchNotFound) {
		t.Fatalf("first lookup: want ErrBranchNotFound, got %v", err)
	}
	// Repeated probes for the same unknown user are absorbed by the negative cache, so
	// an unknown-user REGISTER spray can't hammer Postgres.
	for i := 0; i < 5; i++ {
		if _, err := repo.FindByGlobalSIPUser("9999"); !errors.Is(err, branch.ErrBranchNotFound) {
			t.Fatalf("repeat %d: want ErrBranchNotFound, got %v", i, err)
		}
	}
	if inner.sipReads != 1 {
		t.Fatalf("sipReads = %d, want 1 (negative cache should absorb the repeats)", inner.sipReads)
	}
}

func TestCachedRepo_StatusChangeInvalidates(t *testing.T) {
	inner := &countingRepo{b: testBranch()}
	repo := NewCachedRepository(inner, newMapShared())

	_, _ = repo.FindByGlobalSIPUser("1001") // populate (read #1)
	// A status transition must invalidate the cache, so the next read reflects reality
	// (and does not fight the register use case's write-suppression).
	if err := repo.UpdateRegistrationStatus("branch-1001", branch.RegistrationStatusUnreachable); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByGlobalSIPUser("1001") // read #2 (cache was invalidated)
	if inner.sipReads != 2 {
		t.Fatalf("sipReads = %d, want 2 (status change should have invalidated the cache)", inner.sipReads)
	}
	if got.RegistrationStatus != branch.RegistrationStatusUnreachable {
		t.Fatalf("post-invalidation status = %q, want unreachable", got.RegistrationStatus)
	}
}
