package branch_usecase

import (
	"time"

	"vozko/domain/branch"
)

// fakeRepo implements branch.Repository by embedding the interface (nil) and
// overriding only the methods the registrar use cases call. Any other call would
// panic, which is the intent: the tests must not depend on unrelated methods.
type fakeRepo struct {
	branch.Repository
	bySIP    map[string]*branch.Branch
	statuses map[string]branch.RegistrationStatus
}

func newFakeRepo(branches ...*branch.Branch) *fakeRepo {
	m := make(map[string]*branch.Branch, len(branches))
	for _, b := range branches {
		m[b.SIPUser] = b
	}
	return &fakeRepo{bySIP: m, statuses: map[string]branch.RegistrationStatus{}}
}

func (f *fakeRepo) FindByGlobalSIPUser(sipUser string) (*branch.Branch, error) {
	if b, ok := f.bySIP[sipUser]; ok {
		return b, nil
	}
	return nil, branch.ErrBranchNotFound
}

func (f *fakeRepo) UpdateRegistrationStatus(id string, status branch.RegistrationStatus) error {
	f.statuses[id] = status
	return nil
}

// fakeStore records mutations and serves a controllable live set.
type fakeStore struct {
	upserts []branch.RegistrationBinding
	removes [][2]string
	live    map[string][]branch.RegistrationBinding
}

func newFakeStore() *fakeStore { return &fakeStore{live: map[string][]branch.RegistrationBinding{}} }

func (s *fakeStore) Upsert(b branch.RegistrationBinding) error {
	s.upserts = append(s.upserts, b)
	return nil
}
func (s *fakeStore) Remove(sipUser, callID string) error {
	s.removes = append(s.removes, [2]string{sipUser, callID})
	return nil
}
func (s *fakeStore) ListLive(sipUser string) ([]branch.RegistrationBinding, error) {
	return s.live[sipUser], nil
}
func (s *fakeStore) CountLive(sipUser string) (int, error) { return len(s.live[sipUser]), nil }
func (s *fakeStore) ReapExpired(time.Time) []branch.RegistrationBinding {
	return nil
}

// fakePresence records the reachable/unreachable notifications the register use
// case emits (which is what wires a branch into the routing registry).
type fakePresence struct {
	reachable   []branch.RegisteredBranch
	unreachable []string
}

func (p *fakePresence) OnBranchReachable(b branch.RegisteredBranch) {
	p.reachable = append(p.reachable, b)
}
func (p *fakePresence) OnBranchUnreachable(branchID string) {
	p.unreachable = append(p.unreachable, branchID)
}

// enabledBranch builds a validated, enabled branch with a known HA1 for
// sip_user under realm/password.
func enabledBranch(sipUser, workspaceID, realm, password string) *branch.Branch {
	b := branch.NewBranch("branch-"+sipUser, workspaceID, "member-"+sipUser, "user-"+sipUser, sipUser, "Desk "+sipUser)
	_ = b.Validate() // normalizes sip_user
	b.SetSecret(realm, password)
	b.Enabled = true
	return b
}
