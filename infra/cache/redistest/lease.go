package redistest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	LeaseKey        = "vozko:test:redis-lease"
	undatedLease    = time.Hour
	leaseAfterLimit = time.Minute
)

var (
	ErrPoolLeased = errors.New("redistest: every Redis database of the pool is leased by another test run")
	ErrLeaseLost  = errors.New("redistest: the Redis lease expired and the database may have been used by another test run")
)

type Lease struct {
	DB     int
	client *redis.Client
	holder string
}

func Acquire(ctx context.Context, addr, password string, pool []int, ttl time.Duration) (*Lease, error) {
	holder := uuid.NewString()
	for _, db := range pool {
		client := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
		taken, err := claim(ctx, client, holder, ttl)
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("redistest: lease database %d at %s: %w", db, addr, err)
		}
		if taken {
			return &Lease{DB: db, client: client, holder: holder}, nil
		}
		_ = client.Close()
	}
	return nil, fmt.Errorf("%w: %v at %s", ErrPoolLeased, pool, addr)
}

func claim(ctx context.Context, client *redis.Client, holder string, ttl time.Duration) (bool, error) {
	taken := false
	err := client.Watch(ctx, func(tx *redis.Tx) error {
		err := tx.Get(ctx, LeaseKey).Err()
		if err == nil {
			return nil
		}
		if !errors.Is(err, redis.Nil) {
			return err
		}
		if _, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.FlushDB(ctx)
			pipe.Set(ctx, LeaseKey, holder, ttl)
			return nil
		}); err != nil {
			return err
		}
		taken = true
		return nil
	}, LeaseKey)
	if errors.Is(err, redis.TxFailedErr) {
		return false, nil
	}
	return taken, err
}

func (l *Lease) Client() *redis.Client {
	return l.client
}

func (l *Lease) Release(ctx context.Context) error {
	defer l.client.Close()
	err := l.client.Watch(ctx, func(tx *redis.Tx) error {
		current, err := tx.Get(ctx, LeaseKey).Result()
		switch {
		case errors.Is(err, redis.Nil):
			return ErrLeaseLost
		case err != nil:
			return err
		case current != l.holder:
			return ErrLeaseLost
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.FlushDB(ctx)
			return nil
		})
		return err
	}, LeaseKey)
	if errors.Is(err, redis.TxFailedErr) {
		return ErrLeaseLost
	}
	return err
}

func ExclusiveDB(t testing.TB, addr, password string, pool []int) int {
	t.Helper()
	lease, err := Acquire(context.Background(), addr, password, pool, leaseTTL(t))
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Cleanup(func() {
		if err := lease.Release(context.Background()); err != nil {
			t.Errorf("redis database %d: %v", lease.DB, err)
		}
	})
	return lease.DB
}

func leaseTTL(t testing.TB) time.Duration {
	if timed, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if deadline, ok := timed.Deadline(); ok {
			return time.Until(deadline) + leaseAfterLimit
		}
	}
	return undatedLease
}
