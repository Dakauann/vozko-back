package cache

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	domainCache "vozko/domain/cache"
)

func redisSharedStateForTest(t *testing.T) domainCache.SharedState {
	t.Helper()
	addr := os.Getenv("VOZKO_TEST_REDIS")
	if addr == "" {
		t.Skip("set VOZKO_TEST_REDIS to run against Redis")
	}
	provider := NewRedisProvider(addr, os.Getenv("VOZKO_TEST_REDIS_PASSWORD"))
	return provider.SharedState()
}

func TestAnOwnedKeyIsDeletedOnlyByItsOwnerAgainstRedis(t *testing.T) {
	state := redisSharedStateForTest(t)
	deleter, ok := state.(domainCache.OwnedKeyDeleter)
	if !ok {
		t.Fatal("the Redis shared state must offer a guarded delete")
	}
	key := "test:owned:" + uuid.NewString()
	t.Cleanup(func() { _ = state.Del(key) })

	if acquired, err := state.SetNX(key, "newer-holder", time.Minute); err != nil || !acquired {
		t.Fatalf("SetNX = %v, %v", acquired, err)
	}

	released, err := deleter.DelIfValue(key, "overrunning-holder")
	if err != nil || released {
		t.Fatalf("released = %v, err = %v; another holder's token must not free the key", released, err)
	}
	if exists, _ := state.Exists(key); !exists {
		t.Fatal("the newer holder lost its lock")
	}

	released, err = deleter.DelIfValue(key, "newer-holder")
	if err != nil || !released {
		t.Fatalf("released = %v, err = %v; the owner must free its own key", released, err)
	}
	if exists, _ := state.Exists(key); exists {
		t.Fatal("the key survived its owner's release")
	}
}
