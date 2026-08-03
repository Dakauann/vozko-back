package user_usecase

import (
	"errors"
	"testing"

	"vozko/domain/user"
)

// updateUserRepoStub embeds the repository interface so only Update needs an
// implementation; the rest stay inert.
type updateUserRepoStub struct {
	user.UserRepository
	gotID   string
	gotUser *user.User
	err     error
}

func (s *updateUserRepoStub) Update(id string, u *user.User) error {
	s.gotID = id
	s.gotUser = u
	return s.err
}

// The usecase must call repo.Update with the exact same args the handler used to
// pass directly, this is the behavior-preservation guarantee for the swap.
func TestUpdateUserUseCaseDelegatesToRepo(t *testing.T) {
	repo := &updateUserRepoStub{}
	uc := NewUpdateUserUseCase(repo)

	u := &user.User{ID: "u1", Username: "alice"}
	if err := uc.Execute("u1", u); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.gotID != "u1" || repo.gotUser != u {
		t.Errorf("repo.Update called with (%q, %p), want (u1, %p)", repo.gotID, repo.gotUser, u)
	}
}

func TestUpdateUserUseCasePropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	uc := NewUpdateUserUseCase(&updateUserRepoStub{err: sentinel})
	if err := uc.Execute("u1", &user.User{}); !errors.Is(err, sentinel) {
		t.Fatalf("expected error to propagate, got %v", err)
	}
}
