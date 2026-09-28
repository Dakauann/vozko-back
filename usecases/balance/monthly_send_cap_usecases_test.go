package balance_usecase

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/workspace"
)

var capNow = time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)

var (
	systemAdmin = balance.SendCapActor{UserID: "admin-1", Email: "ops@vozkoia.com", SystemAdmin: true}
	superAdmin  = balance.SendCapActor{UserID: "root-1", Email: "dakauannc@gmail.com", SystemAdmin: true}
	regularUser = balance.SendCapActor{UserID: "user-1", Email: "dakauannc@gmail.com"}
)

type memoryCaps struct {
	caps    map[string]balance.MonthlySendCap
	usages  []balance.SendCapUsage
	since   time.Time
	getErr  error
	deleted []string
}

func newMemoryCaps(caps ...balance.MonthlySendCap) *memoryCaps {
	m := &memoryCaps{caps: map[string]balance.MonthlySendCap{}}
	for _, c := range caps {
		m.caps[c.WorkspaceID] = c
	}
	return m
}

func (m *memoryCaps) GetMonthlySendCap(workspaceID string) (*balance.MonthlySendCap, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	c, ok := m.caps[workspaceID]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

func (m *memoryCaps) UpsertMonthlySendCap(c balance.MonthlySendCap) error {
	m.caps[c.WorkspaceID] = c
	return nil
}

func (m *memoryCaps) DeleteMonthlySendCap(workspaceID string) error {
	m.deleted = append(m.deleted, workspaceID)
	delete(m.caps, workspaceID)
	return nil
}

func (m *memoryCaps) ListMonthlySendCapUsage(since time.Time) ([]balance.SendCapUsage, error) {
	m.since = since
	return m.usages, nil
}

type knownWorkspaces map[string]bool

func (k knownWorkspaces) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	if !k[id] {
		return nil, workspace.ErrWorkspaceNotFound
	}
	return &workspace.Workspace{ID: id}, nil
}

func fixedClock() time.Time { return capNow }

func validUnlockCode(t *testing.T) string {
	t.Helper()
	for i := 0; i < 10000; i++ {
		code := fmt.Sprintf("%04d", i)
		if balance.VerifySendCapUnlockCode(code) == nil {
			return code
		}
	}
	t.Fatal("no four digit code verifies")
	return ""
}

func TestListMonthlySendCaps_OnlySystemAdmins(t *testing.T) {
	uc := NewListMonthlySendCapsUseCase(newMemoryCaps(), fixedClock)
	if _, err := uc.Execute(regularUser, ""); !errors.Is(err, balance.ErrSendCapForbidden) {
		t.Fatalf("want ErrSendCapForbidden, got %v", err)
	}
}

func TestListMonthlySendCaps_FiltersSortsAndTellsWhoCanUnlock(t *testing.T) {
	caps := newMemoryCaps()
	caps.usages = []balance.SendCapUsage{
		{WorkspaceName: "Calm", Cap: balance.MonthlySendCap{WorkspaceID: "a", Limit: 100}, Used: 5},
		{WorkspaceName: "Near", Cap: balance.MonthlySendCap{WorkspaceID: "b", Limit: 100}, Used: 85},
		{WorkspaceName: "Full", Cap: balance.MonthlySendCap{WorkspaceID: "c", Limit: 100}, Used: 100},
	}
	uc := NewListMonthlySendCapsUseCase(caps, fixedClock)

	all, err := uc.Execute(systemAdmin, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all.Items) != 3 || all.Items[0].WorkspaceName != "Full" || all.Items[2].WorkspaceName != "Calm" {
		t.Fatalf("unexpected order %+v", all.Items)
	}
	if all.CanUnlock {
		t.Fatal("a system admin off the allowlist cannot unlock")
	}
	if !caps.since.Equal(balance.SendCapMonthStart(capNow)) || !all.MonthStart.Equal(caps.since) {
		t.Fatalf("usage must be counted from the start of the month, got %v", caps.since)
	}

	near, err := uc.Execute(superAdmin, balance.SendCapLevelNear)
	if err != nil {
		t.Fatalf("list near: %v", err)
	}
	if len(near.Items) != 1 || near.Items[0].WorkspaceName != "Near" || !near.CanUnlock {
		t.Fatalf("unexpected near listing %+v", near)
	}
}

func TestSetMonthlySendCap_CreatesAndLowers(t *testing.T) {
	caps := newMemoryCaps()
	uc := NewSetMonthlySendCapUseCase(caps, knownWorkspaces{"ws-1": true}, fixedClock)

	created, err := uc.Execute(systemAdmin, "ws-1", 1000)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Limit != 1000 || created.UpdatedBy != "admin-1" || !created.UpdatedAt.Equal(capNow) {
		t.Fatalf("unexpected cap %+v", created)
	}

	lowered, err := uc.Execute(systemAdmin, "ws-1", 400)
	if err != nil || lowered.Limit != 400 || caps.caps["ws-1"].Limit != 400 {
		t.Fatalf("lowering must be allowed, got %+v, %v", lowered, err)
	}
}

func TestSetMonthlySendCap_RaisingRequiresAnUnlock(t *testing.T) {
	caps := newMemoryCaps(balance.MonthlySendCap{WorkspaceID: "ws-1", Limit: 100})
	uc := NewSetMonthlySendCapUseCase(caps, knownWorkspaces{"ws-1": true}, fixedClock)

	for _, actor := range []balance.SendCapActor{systemAdmin, superAdmin} {
		if _, err := uc.Execute(actor, "ws-1", 101); !errors.Is(err, balance.ErrSendCapUnlockRequired) {
			t.Fatalf("raising through set must require an unlock even for %s, got %v", actor.Email, err)
		}
	}
	if caps.caps["ws-1"].Limit != 100 {
		t.Fatal("the cap must stay untouched")
	}
}

func TestSetMonthlySendCap_Refusals(t *testing.T) {
	cases := []struct {
		name  string
		caps  *memoryCaps
		actor balance.SendCapActor
		ws    string
		limit int64
		want  error
	}{
		{"regular user", newMemoryCaps(), regularUser, "ws-1", 10, balance.ErrSendCapForbidden},
		{"unknown workspace", newMemoryCaps(), systemAdmin, "ws-missing", 10, workspace.ErrWorkspaceNotFound},
		{"zero limit", newMemoryCaps(), systemAdmin, "ws-1", 0, balance.ErrInvalidSendCapLimit},
		{"negative limit on an existing cap", newMemoryCaps(balance.MonthlySendCap{WorkspaceID: "ws-1", Limit: 10}), systemAdmin, "ws-1", -1, balance.ErrInvalidSendCapLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewSetMonthlySendCapUseCase(tc.caps, knownWorkspaces{"ws-1": true}, fixedClock)
			if _, err := uc.Execute(tc.actor, tc.ws, tc.limit); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestSetMonthlySendCap_ReadFailureChangesNothing(t *testing.T) {
	caps := newMemoryCaps()
	caps.getErr = errors.New("db down")
	uc := NewSetMonthlySendCapUseCase(caps, knownWorkspaces{"ws-1": true}, fixedClock)

	if _, err := uc.Execute(systemAdmin, "ws-1", 10); !errors.Is(err, caps.getErr) {
		t.Fatalf("want the read error, got %v", err)
	}
	if len(caps.caps) != 0 {
		t.Fatal("nothing may be written when the current cap is unknown")
	}
}

func TestUnlockMonthlySendCap_RaisesWithTheCode(t *testing.T) {
	caps := newMemoryCaps(balance.MonthlySendCap{WorkspaceID: "ws-1", Limit: 100, UpdatedBy: "admin-1"})
	uc := NewUnlockMonthlySendCapUseCase(caps, fixedClock)
	limit := int64(5000)

	raised, err := uc.Execute(superAdmin, balance.UnlockMonthlySendCapInput{WorkspaceID: "ws-1", Limit: &limit, Code: validUnlockCode(t)})
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if raised.Limit != 5000 || raised.UnlockedBy == nil || *raised.UnlockedBy != "root-1" || caps.caps["ws-1"].Limit != 5000 {
		t.Fatalf("unexpected cap %+v", raised)
	}
}

func TestUnlockMonthlySendCap_RemovesTheCap(t *testing.T) {
	caps := newMemoryCaps(balance.MonthlySendCap{WorkspaceID: "ws-1", Limit: 100})
	uc := NewUnlockMonthlySendCapUseCase(caps, fixedClock)

	removed, err := uc.Execute(superAdmin, balance.UnlockMonthlySendCapInput{WorkspaceID: "ws-1", Code: validUnlockCode(t)})
	if err != nil || removed != nil {
		t.Fatalf("want the cap removed, got %+v, %v", removed, err)
	}
	if len(caps.deleted) != 1 || caps.deleted[0] != "ws-1" {
		t.Fatalf("deleted = %v", caps.deleted)
	}
}

func TestUnlockMonthlySendCap_Refusals(t *testing.T) {
	existing := balance.MonthlySendCap{WorkspaceID: "ws-1", Limit: 100}
	raise := int64(500)
	zero := int64(0)
	cases := []struct {
		name      string
		actor     balance.SendCapActor
		limit     *int64
		rightCode bool
		caps      *memoryCaps
		want      error
	}{
		{"system admin off the allowlist, even with the right code", systemAdmin, &raise, true, newMemoryCaps(existing), balance.ErrSendCapForbidden},
		{"allowlisted email without the admin role", regularUser, &raise, true, newMemoryCaps(existing), balance.ErrSendCapForbidden},
		{"wrong code", superAdmin, &raise, false, newMemoryCaps(existing), balance.ErrInvalidUnlockCode},
		{"no cap to unlock", superAdmin, &raise, true, newMemoryCaps(), balance.ErrMonthlySendCapNotFound},
		{"non positive limit", superAdmin, &zero, true, newMemoryCaps(existing), balance.ErrInvalidSendCapLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := "abcd"
			if tc.rightCode {
				code = validUnlockCode(t)
			}
			uc := NewUnlockMonthlySendCapUseCase(tc.caps, fixedClock)
			if _, err := uc.Execute(tc.actor, balance.UnlockMonthlySendCapInput{WorkspaceID: "ws-1", Limit: tc.limit, Code: code}); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if c, ok := tc.caps.caps["ws-1"]; ok && c.Limit != 100 {
				t.Fatalf("a refused unlock must not change the cap, got %d", c.Limit)
			}
			if len(tc.caps.deleted) != 0 {
				t.Fatal("a refused unlock must not delete the cap")
			}
		})
	}
}
