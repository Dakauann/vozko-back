package branch

import (
	"errors"
	"testing"
)

type fakeBranchRepo struct {
	bySIPUser map[string]*Branch // key: workspaceID + "|" + sipUser
}

func (r *fakeBranchRepo) FindBySIPUser(workspaceID, sipUser string) (*Branch, error) {
	if b, ok := r.bySIPUser[workspaceID+"|"+sipUser]; ok {
		return b, nil
	}
	return nil, ErrBranchNotFound
}

// Unused Repository methods for the resolver's narrow need.
func (r *fakeBranchRepo) Create(*Branch) error             { return nil }
func (r *fakeBranchRepo) Update(*Branch) error             { return nil }
func (r *fakeBranchRepo) Delete(string) error              { return nil }
func (r *fakeBranchRepo) FindByID(string) (*Branch, error) { return nil, ErrBranchNotFound }
func (r *fakeBranchRepo) FindByWorkspace(string, int, int) ([]*Branch, int64, error) {
	return nil, 0, nil
}
func (r *fakeBranchRepo) FindByUser(string, string) ([]*Branch, error) { return nil, nil }
func (r *fakeBranchRepo) FindByGlobalSIPUser(string) (*Branch, error)  { return nil, ErrBranchNotFound }
func (r *fakeBranchRepo) CountByWorkspace(string) (int64, error)       { return 0, nil }
func (r *fakeBranchRepo) UpdateRegistrationStatus(string, RegistrationStatus) error {
	return nil
}
func (r *fakeBranchRepo) ResetLiveRegistrations() (int64, error) { return 0, nil }

func referrer1002() *Branch {
	return &Branch{ID: "b-1002", WorkspaceID: "ws-1", UserID: "u-1002", SIPUser: "1002", Enabled: true}
}

func TestReferResolve_HappyPath(t *testing.T) {
	target := &Branch{ID: "b-1003", WorkspaceID: "ws-1", UserID: "u-1003", SIPUser: "1003", Enabled: true}
	repo := &fakeBranchRepo{bySIPUser: map[string]*Branch{"ws-1|1003": target}}
	r := NewReferTargetResolver(repo)

	got, err := r.Resolve(referrer1002(), "1003", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Branch.UserID != "u-1003" {
		t.Fatalf("resolved to %q, want u-1003", got.Branch.UserID)
	}
}

func TestReferResolve_Errors(t *testing.T) {
	target := &Branch{ID: "b-1003", WorkspaceID: "ws-1", UserID: "u-1003", SIPUser: "1003", Enabled: true}
	disabled := &Branch{ID: "b-1004", WorkspaceID: "ws-1", SIPUser: "1004", Enabled: false}
	dnd := &Branch{ID: "b-1005", WorkspaceID: "ws-1", SIPUser: "1005", Enabled: true, DND: true}
	other := &Branch{ID: "b-9", WorkspaceID: "ws-OTHER", SIPUser: "1003", Enabled: true}
	repo := &fakeBranchRepo{bySIPUser: map[string]*Branch{
		"ws-1|1003":     target,
		"ws-1|1004":     disabled,
		"ws-1|1005":     dnd,
		"ws-OTHER|1003": other,
	}}
	r := NewReferTargetResolver(repo)

	cases := []struct {
		name        string
		extension   string
		hasReplaces bool
		want        error
	}{
		{"attended REFER (Replaces) unsupported", "1003", true, ErrReferAttendedUnsupported},
		{"empty extension", "", false, ErrReferNoTarget},
		{"whitespace extension", "   ", false, ErrReferNoTarget},
		{"self-transfer", "1002", false, ErrReferSelf},
		{"unknown extension", "9999", false, ErrReferUnknownExtension},
		{"disabled target", "1004", false, ErrReferUnknownExtension},
		{"DND target", "1005", false, ErrReferUnknownExtension},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.Resolve(referrer1002(), c.extension, c.hasReplaces)
			if !errors.Is(err, c.want) {
				t.Fatalf("Resolve(%q, replaces=%v) = %v, want %v", c.extension, c.hasReplaces, err, c.want)
			}
		})
	}
}

// A referrer in ws-1 that dials "1003" must resolve to the ws-1 target, NEVER a
// same-numbered extension in another workspace (cross-tenant isolation).
func TestReferResolve_WorkspaceScoped(t *testing.T) {
	ws1Target := &Branch{ID: "b-1003", WorkspaceID: "ws-1", UserID: "u-ws1", SIPUser: "1003", Enabled: true}
	ws2Target := &Branch{ID: "b-x", WorkspaceID: "ws-2", UserID: "u-ws2", SIPUser: "1003", Enabled: true}
	repo := &fakeBranchRepo{bySIPUser: map[string]*Branch{
		"ws-1|1003": ws1Target,
		"ws-2|1003": ws2Target,
	}}
	r := NewReferTargetResolver(repo)

	got, err := r.Resolve(referrer1002(), "1003", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Branch.UserID != "u-ws1" {
		t.Fatalf("resolved to %q, want the same-workspace target u-ws1", got.Branch.UserID)
	}
}
