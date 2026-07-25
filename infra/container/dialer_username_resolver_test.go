package container

import (
	"testing"

	"vozko/domain/user"
)

type countingUserRepo struct {
	user.UserRepository
	calls int
	names map[string]string
}

func (r *countingUserRepo) FindByIDs(ids []string) ([]*user.User, error) {
	r.calls++
	out := make([]*user.User, 0, len(ids))
	for _, id := range ids {
		if n, ok := r.names[id]; ok {
			out = append(out, &user.User{ID: id, Username: n})
		}
	}
	return out, nil
}

// The presence panel resolves usernames on every broadcast, so the resolver must serve
// repeats from cache and only hit the DB for ids it has not seen (or that expired).
func TestDialerUsernameResolver_CachesAndOnlyQueriesMisses(t *testing.T) {
	repo := &countingUserRepo{names: map[string]string{"u1": "Alice", "u2": "Bob"}}
	r := newDialerUsernameResolver(repo)

	got := r.ResolveUsernames([]string{"u1", "u2"})
	if got["u1"] != "Alice" || got["u2"] != "Bob" {
		t.Fatalf("first resolve = %v", got)
	}
	if repo.calls != 1 {
		t.Fatalf("first resolve DB calls = %d, want 1", repo.calls)
	}

	// Repeats are fully cache-served: no more DB hits (this is the whole point).
	for i := 0; i < 10; i++ {
		if got := r.ResolveUsernames([]string{"u1", "u2"}); got["u1"] != "Alice" {
			t.Fatalf("cached resolve %d = %v", i, got)
		}
	}
	if repo.calls != 1 {
		t.Fatalf("cache miss: DB called %d times across 11 resolves, want 1", repo.calls)
	}

	// A new id triggers exactly one more query, for the missing id only.
	repo.names["u3"] = "Carol"
	got = r.ResolveUsernames([]string{"u1", "u2", "u3"})
	if got["u3"] != "Carol" {
		t.Fatalf("u3 not resolved: %v", got)
	}
	if repo.calls != 2 {
		t.Fatalf("mixed hit/miss DB calls = %d, want 2 (only u3 queried)", repo.calls)
	}
}
