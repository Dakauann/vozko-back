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

func TestCallSessionUsernameResolver_CachesAndOnlyQueriesMisses(t *testing.T) {
	repo := &countingUserRepo{names: map[string]string{"u1": "Alice", "u2": "Bob"}}
	r := newCallSessionUsernameResolver(repo)

	got := r.ResolveUsernames([]string{"u1", "u2"})
	if got["u1"] != "Alice" || got["u2"] != "Bob" {
		t.Fatalf("first resolve = %v", got)
	}
	if repo.calls != 1 {
		t.Fatalf("first resolve DB calls = %d, want 1", repo.calls)
	}

	for i := 0; i < 10; i++ {
		if got := r.ResolveUsernames([]string{"u1", "u2"}); got["u1"] != "Alice" {
			t.Fatalf("cached resolve %d = %v", i, got)
		}
	}
	if repo.calls != 1 {
		t.Fatalf("cache miss: DB called %d times across 11 resolves, want 1", repo.calls)
	}

	repo.names["u3"] = "Carol"
	got = r.ResolveUsernames([]string{"u1", "u2", "u3"})
	if got["u3"] != "Carol" {
		t.Fatalf("u3 not resolved: %v", got)
	}
	if repo.calls != 2 {
		t.Fatalf("mixed hit/miss DB calls = %d, want 2 (only u3 queried)", repo.calls)
	}
}
