package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestLRUEvictsTheLeastRecentlyUsedEntryWhenFull(t *testing.T) {
	lru := NewLRU[string, int](2)
	lru.Put("a", 1)
	lru.Put("b", 2)
	if _, ok := lru.Get("a"); !ok {
		t.Fatal("a was just stored")
	}
	lru.Put("c", 3)
	if _, ok := lru.Get("b"); ok {
		t.Fatal("b was the least recently used and must be evicted to stay bounded")
	}
	for _, key := range []string{"a", "c"} {
		if _, ok := lru.Get(key); !ok {
			t.Fatalf("%s must still be cached", key)
		}
	}
	if lru.Len() != 2 {
		t.Fatalf("Len = %d, want the bound 2", lru.Len())
	}
}

func TestLRUReplacesAValueWithoutGrowing(t *testing.T) {
	lru := NewLRU[string, int](2)
	lru.Put("a", 1)
	lru.Put("a", 9)
	if v, _ := lru.Get("a"); v != 9 || lru.Len() != 1 {
		t.Fatalf("value = %d len = %d, want 9 and 1", v, lru.Len())
	}
}

func TestLRUWithoutCapacityStillHoldsOneEntry(t *testing.T) {
	lru := NewLRU[string, int](0)
	lru.Put("a", 1)
	lru.Put("b", 2)
	if lru.Len() != 1 {
		t.Fatalf("Len = %d, want 1", lru.Len())
	}
}

func TestLRUIsSafeForConcurrentUse(t *testing.T) {
	lru := NewLRU[string, int](16)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("%d-%d", g, i%32)
				lru.Put(key, i)
				lru.Get(key)
			}
		}(g)
	}
	wg.Wait()
	if lru.Len() > 16 {
		t.Fatalf("Len = %d, exceeds the bound", lru.Len())
	}
}
