package workspace_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
	"vozko/domain/workspace"
	wsc "vozko/domain/workspace_config"
)

type stubWorkspaceRepo struct {
	workspace.Repository

	defaultWS     *workspace.Workspace
	defaultErr    error
	createdWS     *workspace.Workspace
	createErr     error
	addedMember   *workspace.Member
	addMemberErr  error
	getDefaultHit int
}

func (s *stubWorkspaceRepo) GetDefaultWorkspace(ownerID string) (*workspace.Workspace, error) {
	s.getDefaultHit++
	return s.defaultWS, s.defaultErr
}

func (s *stubWorkspaceRepo) CreateWorkspace(ws *workspace.Workspace) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.createdWS = ws
	return nil
}

func (s *stubWorkspaceRepo) AddMember(m *workspace.Member) error {
	if s.addMemberErr != nil {
		return s.addMemberErr
	}
	s.addedMember = m
	return nil
}

type stubConfigRepo struct {
	ensureCalls []string
	ensureErr   error
	getByWSFn   func(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
	upsertCalls int
}

func (s *stubConfigRepo) GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error) {
	if s.getByWSFn != nil {
		return s.getByWSFn(ctx, workspaceID)
	}
	return nil, nil
}

func (s *stubConfigRepo) Upsert(ctx context.Context, cfg *wsc.WorkspaceConfig) error {
	s.upsertCalls++
	return nil
}

func (s *stubConfigRepo) EnsureExists(ctx context.Context, workspaceID string) error {
	s.ensureCalls = append(s.ensureCalls, workspaceID)
	return s.ensureErr
}

func (s *stubConfigRepo) GetIncludedUnofficialInstancesByWorkspaceIDs(context.Context, []string) (map[string]int, error) {
	return map[string]int{}, nil
}

type stubTrackReferral struct {
	calls  []affiliate.TrackReferralInput
	result *affiliate.Referral
	err    error
}

func (s *stubTrackReferral) Execute(ctx context.Context, input affiliate.TrackReferralInput) (*affiliate.Referral, error) {
	s.calls = append(s.calls, input)
	return s.result, s.err
}

func TestEnsureDefaultWorkspace_ExistingWorkspace_DoesNotTrackReferral(t *testing.T) {
	existing := &workspace.Workspace{ID: "ws-existing", OwnerID: "u1", IsDefault: true}
	repo := &stubWorkspaceRepo{defaultWS: existing}
	configRepo := &stubConfigRepo{}
	track := &stubTrackReferral{}

	uc := NewEnsureDefaultWorkspaceUseCase(repo, configRepo, track)
	out, err := uc.Execute("u1", "u1@example.com", "REF123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != existing {
		t.Fatalf("expected existing workspace returned, got %+v", out)
	}
	if repo.createdWS != nil {
		t.Fatalf("must NOT create a new workspace when one already exists")
	}
	if len(track.calls) != 0 {
		t.Fatalf("must NOT track referral for pre-existing workspace (got %d calls)", len(track.calls))
	}
}

func TestEnsureDefaultWorkspace_NewWorkspace_EmptyCode_DoesNotTrack(t *testing.T) {
	repo := &stubWorkspaceRepo{}
	configRepo := &stubConfigRepo{}
	track := &stubTrackReferral{}

	uc := NewEnsureDefaultWorkspaceUseCase(repo, configRepo, track)
	ws, err := uc.Execute("u1", "u1@example.com", "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws == nil || repo.createdWS == nil {
		t.Fatalf("expected new workspace created")
	}
	if len(track.calls) != 0 {
		t.Fatalf("must NOT track referral when code is blank")
	}
}

func TestEnsureDefaultWorkspace_NewWorkspace_ValidCode_Tracks(t *testing.T) {
	repo := &stubWorkspaceRepo{}
	configRepo := &stubConfigRepo{}
	track := &stubTrackReferral{result: &affiliate.Referral{}}

	uc := NewEnsureDefaultWorkspaceUseCase(repo, configRepo, track)
	_, err := uc.Execute("user-abc", "u@e.com", "  REFCODE  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(track.calls) != 1 {
		t.Fatalf("expected 1 track call, got %d", len(track.calls))
	}
	got := track.calls[0]
	if got.Code != "REFCODE" {
		t.Fatalf("expected trimmed code REFCODE, got %q", got.Code)
	}
	if got.WorkspaceID != repo.createdWS.ID {
		t.Fatalf("expected workspace id %s, got %s", repo.createdWS.ID, got.WorkspaceID)
	}
	if got.WorkspaceOwnerUserID != "user-abc" {
		t.Fatalf("expected owner user id user-abc, got %s", got.WorkspaceOwnerUserID)
	}
}

func TestEnsureDefaultWorkspace_TrackingFailure_Swallowed(t *testing.T) {
	repo := &stubWorkspaceRepo{}
	configRepo := &stubConfigRepo{}
	track := &stubTrackReferral{err: affiliate.ErrInvalidReferralCode}

	uc := NewEnsureDefaultWorkspaceUseCase(repo, configRepo, track)
	ws, err := uc.Execute("u", "e@x.com", "BADCODE")
	if err != nil {
		t.Fatalf("tracking failure must be swallowed, got: %v", err)
	}
	if ws == nil || repo.createdWS == nil {
		t.Fatalf("workspace must still be created")
	}
}

func TestEnsureDefaultWorkspace_NilTrackReferral_NoOp(t *testing.T) {
	repo := &stubWorkspaceRepo{}
	configRepo := &stubConfigRepo{}

	uc := NewEnsureDefaultWorkspaceUseCase(repo, configRepo, nil)
	_, err := uc.Execute("u", "e@x.com", "REF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureDefaultWorkspace_CreateError_Returned(t *testing.T) {
	repo := &stubWorkspaceRepo{createErr: errors.New("db down")}
	uc := NewEnsureDefaultWorkspaceUseCase(repo, &stubConfigRepo{}, nil)
	if _, err := uc.Execute("u", "e@x.com", ""); err == nil {
		t.Fatal("expected error when CreateWorkspace fails")
	}
}

func TestEnsureDefaultWorkspace_GetDefaultError_Returned(t *testing.T) {
	repo := &stubWorkspaceRepo{defaultErr: errors.New("lookup failed")}
	uc := NewEnsureDefaultWorkspaceUseCase(repo, &stubConfigRepo{}, nil)
	if _, err := uc.Execute("u", "e@x.com", "REF"); err == nil {
		t.Fatal("expected error when GetDefaultWorkspace fails")
	}
	if repo.createdWS != nil {
		t.Fatal("must not create workspace when default lookup fails")
	}
}
