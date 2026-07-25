package sip_trunk_usecase

import (
	"errors"
	"testing"

	"vozko/domain/sip_trunk"
)

// fakeRepo implements sip_trunk.Repository for use-case tests. Only the methods
// the create use case exercises carry behavior; the rest satisfy the interface.
type fakeRepo struct {
	count     int64
	countErr  error
	created   []*sip_trunk.SIPTrunk
	createErr error
}

func (f *fakeRepo) Create(t *sip_trunk.SIPTrunk) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, t)
	return nil
}
func (f *fakeRepo) Update(*sip_trunk.SIPTrunk) error { return nil }
func (f *fakeRepo) Delete(string) error              { return nil }
func (f *fakeRepo) FindByID(string) (*sip_trunk.SIPTrunk, error) {
	return nil, sip_trunk.ErrTrunkNotFound
}
func (f *fakeRepo) FindByIDs([]string) ([]*sip_trunk.SIPTrunk, error)         { return nil, nil }
func (f *fakeRepo) FindAll(int, int) ([]*sip_trunk.SIPTrunk, int64, error)    { return nil, 0, nil }
func (f *fakeRepo) FindEnabled() ([]*sip_trunk.SIPTrunk, error)               { return nil, nil }
func (f *fakeRepo) UpdateStatus(string, sip_trunk.RegistrationStatus, string) error {
	return nil
}
func (f *fakeRepo) FindAccessible(string, []string, int, int) ([]*sip_trunk.SIPTrunk, int64, error) {
	return nil, 0, nil
}
func (f *fakeRepo) CountByWorkspace(string) (int64, error) { return f.count, f.countErr }

func validInput(actor sip_trunk.Actor) sip_trunk.CreateTrunkInput {
	return sip_trunk.CreateTrunkInput{
		WorkspaceID:  "ws-1",
		Name:         "Trunk",
		Host:         "sip.example.com",
		Port:         5060,
		Username:     "alice",
		Password:     "s3cret",
		Transport:    sip_trunk.TransportUDP,
		TrunkType:    sip_trunk.TrunkTypeMobile,
		IsRotational: true,
		Actor:        actor,
	}
}

func TestCreate_WorkspaceAtCap_Rejected(t *testing.T) {
	repo := &fakeRepo{count: 20}
	uc := NewCreateUseCase(repo, nil, 20, nil)

	_, err := uc.Execute(validInput(sip_trunk.Actor{WorkspaceID: "ws-1"}))
	if !errors.Is(err, sip_trunk.ErrTrunkLimitReached) {
		t.Fatalf("expected ErrTrunkLimitReached, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("trunk must not be persisted when at cap")
	}
}

func TestCreate_WorkspaceUnderCap_OK(t *testing.T) {
	repo := &fakeRepo{count: 19}
	uc := NewCreateUseCase(repo, nil, 20, nil)

	trunk, err := uc.Execute(validInput(sip_trunk.Actor{WorkspaceID: "ws-1"}))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if trunk.IsGloballyVisible {
		t.Fatalf("workspace-created trunk must not be global")
	}
	if len(repo.created) != 1 {
		t.Fatalf("trunk should be persisted")
	}
}

func TestCreate_AdminBypassesCap(t *testing.T) {
	repo := &fakeRepo{count: 9999}
	uc := NewCreateUseCase(repo, nil, 20, nil)

	if _, err := uc.Execute(validInput(sip_trunk.Actor{IsAdmin: true})); err != nil {
		t.Fatalf("admin must bypass the per-workspace cap, got %v", err)
	}
}

func TestCreate_NonAdminCannotPublishGlobally(t *testing.T) {
	repo := &fakeRepo{count: 0}
	uc := NewCreateUseCase(repo, nil, 20, nil)

	in := validInput(sip_trunk.Actor{WorkspaceID: "ws-1"})
	in.IsGloballyVisible = true
	if _, err := uc.Execute(in); !errors.Is(err, sip_trunk.ErrTrunkGlobalForbidden) {
		t.Fatalf("expected ErrTrunkGlobalForbidden, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("trunk must not be persisted when global publish is forbidden")
	}
}

func TestCreate_AdminCanPublishGlobally(t *testing.T) {
	repo := &fakeRepo{count: 0}
	uc := NewCreateUseCase(repo, nil, 20, nil)

	in := validInput(sip_trunk.Actor{IsAdmin: true})
	in.IsGloballyVisible = true
	trunk, err := uc.Execute(in)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !trunk.IsGloballyVisible {
		t.Fatalf("admin global trunk must be globally visible")
	}
}

// blockHostGuard reports the configured host as resolving to a blocked address.
type blockHostGuard struct{ blocked string }

func (g blockHostGuard) ResolvesToBlocked(host string) bool { return host == g.blocked }

func TestCreate_HostGuardBlocksInternalResolution(t *testing.T) {
	repo := &fakeRepo{count: 0}
	uc := NewCreateUseCase(repo, nil, 20, blockHostGuard{blocked: "sip.example.com"})

	if _, err := uc.Execute(validInput(sip_trunk.Actor{WorkspaceID: "ws-1"})); !errors.Is(err, sip_trunk.ErrTrunkHostBlocked) {
		t.Fatalf("expected ErrTrunkHostBlocked, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("trunk must not be persisted when host is blocked")
	}
}

func TestCreate_HostGuardAllowsPublicResolution(t *testing.T) {
	repo := &fakeRepo{count: 0}
	uc := NewCreateUseCase(repo, nil, 20, blockHostGuard{blocked: "evil.internal"})

	if _, err := uc.Execute(validInput(sip_trunk.Actor{WorkspaceID: "ws-1"})); err != nil {
		t.Fatalf("public host should be allowed, got %v", err)
	}
}

func TestCreate_CapDisabledWhenZero(t *testing.T) {
	repo := &fakeRepo{count: 9999}
	uc := NewCreateUseCase(repo, nil, 0, nil)

	if _, err := uc.Execute(validInput(sip_trunk.Actor{WorkspaceID: "ws-1"})); err != nil {
		t.Fatalf("cap of 0 disables enforcement, got %v", err)
	}
}
