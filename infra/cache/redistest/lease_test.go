package redistest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"vozko/infra/cache/redistest"
	"vozko/infra/testenv"
)

var leaseTestPool = []int{13, 14, 15}

func redisAt(t *testing.T) (string, string) {
	t.Helper()
	target := testenv.Redis(t)
	return target.Addr, target.Password
}

func acquire(t *testing.T, pool []int) *redistest.Lease {
	t.Helper()
	addr, password := redisAt(t)
	lease, err := redistest.Acquire(context.Background(), addr, password, pool, time.Minute)
	if err != nil {
		t.Fatalf("acquire from %v: %v", pool, err)
	}
	t.Cleanup(func() { _ = lease.Release(context.Background()) })
	return lease
}

func clientOn(t *testing.T, db int) *redis.Client {
	t.Helper()
	addr, password := redisAt(t)
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func expireLease(t *testing.T, db int) {
	t.Helper()
	if err := clientOn(t, db).Del(context.Background(), redistest.LeaseKey).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestTwoRunsLeaseDifferentDatabasesAndAHeldDatabaseIsNeverHandedOut(t *testing.T) {
	first := acquire(t, leaseTestPool)
	second := acquire(t, leaseTestPool)
	if first.DB == second.DB || !slices.Contains(leaseTestPool, first.DB) || !slices.Contains(leaseTestPool, second.DB) {
		t.Fatalf("leased %d and %d from %v", first.DB, second.DB, leaseTestPool)
	}
	addr, password := redisAt(t)
	if _, err := redistest.Acquire(context.Background(), addr, password, []int{first.DB}, time.Minute); !errors.Is(err, redistest.ErrPoolLeased) {
		t.Fatalf("a held database was handed out again: %v", err)
	}
}

func TestALeaseStartsOnAnEmptyDatabaseEvenAfterARunDiedHoldingIt(t *testing.T) {
	dead := acquire(t, leaseTestPool)
	ctx := context.Background()
	if err := dead.Client().Set(ctx, "email:inbound:sender:198.51.100.9", 299, 0).Err(); err != nil {
		t.Fatal(err)
	}
	expireLease(t, dead.DB)

	next := acquire(t, []int{dead.DB})
	if size, err := next.Client().DBSize(ctx).Result(); err != nil || size != 1 {
		t.Fatalf("keys on the new lease = %d (%v), want only the lease itself", size, err)
	}
}

func TestReleaseEmptiesTheDatabaseAndFreesIt(t *testing.T) {
	ctx := context.Background()
	lease := acquire(t, leaseTestPool)
	db := lease.DB
	if err := lease.Client().Set(ctx, "email:ses:quota", "x", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if size, err := clientOn(t, db).DBSize(ctx).Result(); err != nil || size != 0 {
		t.Fatalf("keys after release = %d (%v)", size, err)
	}
	if again := acquire(t, []int{db}); again.DB != db {
		t.Fatalf("leased %d, want the freed %d", again.DB, db)
	}
}

func TestAReleaseAfterTheLeaseWasTakenOverLeavesTheNewHolderAlone(t *testing.T) {
	ctx := context.Background()
	stale := acquire(t, leaseTestPool)
	expireLease(t, stale.DB)
	holder := acquire(t, []int{stale.DB})
	if err := holder.Client().Set(ctx, "email:ses:quota", "x", 0).Err(); err != nil {
		t.Fatal(err)
	}

	if err := stale.Release(ctx); !errors.Is(err, redistest.ErrLeaseLost) {
		t.Fatalf("release of a lost lease = %v, want ErrLeaseLost", err)
	}
	if got, err := holder.Client().Get(ctx, "email:ses:quota").Result(); err != nil || got != "x" {
		t.Fatalf("the new holder's key = %q (%v), the stale release flushed it", got, err)
	}
}
