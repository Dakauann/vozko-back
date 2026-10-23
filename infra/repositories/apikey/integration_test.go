package apikey_repository

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/apikey"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func newKey(t *testing.T, workspaceID string) (apikey.Key, string) {
	t.Helper()
	secret, err := apikey.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return apikey.Key{
		ID: uuid.NewString(), WorkspaceID: workspaceID, Name: "Produção", LookupID: secret.LookupID, SecretHash: secret.Hash,
		Scopes: []apikey.Scope{apikey.ScopeEmailSend}, CreatedBy: "user-1", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}, secret.Plain
}

func TestIntegrationKeyRoundTripLookupAndRevoke(t *testing.T) {
	db := repotest.IsolatedDB(t, "api_keys_test", &schema.APIKey{})
	repo := New(db)
	ctx := context.Background()
	ws := uuid.NewString()
	k, plain := newKey(t, ws)
	if err := repo.Create(ctx, &k); err != nil {
		t.Fatal(err)
	}
	lookup, _ := apikey.Parse(plain)
	found, err := repo.GetByLookupID(ctx, lookup)
	if err != nil || found.ID != k.ID || !apikey.Matches(found.SecretHash, plain) || found.Scopes[0] != apikey.ScopeEmailSend {
		t.Fatalf("found = %+v %v", found, err)
	}
	if _, err := repo.GetByLookupID(ctx, "zzzzzzzzzzzz"); !errors.Is(err, apikey.ErrNotFound) {
		t.Fatalf("unknown lookup = %v", err)
	}
	if err := repo.Touch(ctx, k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	revoked, err := repo.Revoke(ctx, ws, k.ID, "user-2", time.Now())
	if err != nil || revoked.RevokedAt == nil || revoked.RevokedBy != "user-2" || revoked.LastUsedAt == nil {
		t.Fatalf("revoked = %+v %v", revoked, err)
	}
	if _, err := repo.Revoke(ctx, uuid.NewString(), k.ID, "x", time.Now()); !errors.Is(err, apikey.ErrNotFound) {
		t.Fatalf("revoke from another workspace = %v", err)
	}
	if n, _ := repo.CountActive(ctx, ws); n != 0 {
		t.Fatalf("active = %d", n)
	}
}

func TestIntegrationConcurrentCreatesNeverPassTheActiveLimit(t *testing.T) {
	db := repotest.IsolatedDB(t, "api_keys_test", &schema.APIKey{})
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(20)
	repo := New(db)
	ws := uuid.NewString()
	var created, refused atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < apikey.MaxActivePerWorkspace+15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, _ := newKey(t, ws)
			switch err := repo.Create(context.Background(), &k); {
			case err == nil:
				created.Add(1)
			case errors.Is(err, apikey.ErrLimitReached):
				refused.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if created.Load() != apikey.MaxActivePerWorkspace || refused.Load() != 15 {
		t.Fatalf("created %d, refused %d", created.Load(), refused.Load())
	}
}
