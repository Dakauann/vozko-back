package teamchat

import (
	"sort"
	"strings"
	"time"

	"vozko/domain/shared"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Member struct {
	ThreadID    string
	WorkspaceID string
	UserID      string
	Role        Role
	LastReadSeq int64
	Muted       bool
	JoinedAt    time.Time
	LeftAt      *time.Time
}

func (m Member) IsActive() bool {
	return m.LeftAt == nil
}

func (m Member) Unread(lastSeq int64) int64 {
	if lastSeq <= m.LastReadSeq {
		return 0
	}
	return lastSeq - m.LastReadSeq
}

func ReadUpTo(current, requested, lastSeq int64) int64 {
	target := min(requested, lastSeq)
	return max(current, target)
}

type Roster []Member

func (r Roster) Active(userID string) (Member, bool) {
	userID = strings.TrimSpace(userID)
	for _, m := range r {
		if m.UserID == userID && m.IsActive() {
			return m, true
		}
	}
	return Member{}, false
}

func (r Roster) ActiveUserIDs() []string {
	ids := make([]string, 0, len(r))
	for _, m := range r {
		if m.IsActive() {
			ids = append(ids, m.UserID)
		}
	}
	return ids
}

func (r Roster) OthersThan(userID string) []string {
	return withoutPerson(r.ActiveUserIDs(), userID)
}

func (r Roster) Other(userID string) (Member, bool) {
	for _, m := range r {
		if m.UserID != userID {
			return m, true
		}
	}
	return Member{}, false
}

func (r Roster) SuccessorAdmin(leaving string) (string, bool) {
	leaver, ok := r.Active(leaving)
	if !ok || leaver.Role != RoleAdmin {
		return "", false
	}
	candidates := make([]Member, 0, len(r))
	for _, m := range r {
		if !m.IsActive() || m.UserID == leaving {
			continue
		}
		if m.Role == RoleAdmin {
			return "", false
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].JoinedAt.Equal(candidates[j].JoinedAt) {
			return candidates[i].UserID < candidates[j].UserID
		}
		return candidates[i].JoinedAt.Before(candidates[j].JoinedAt)
	})
	return candidates[0].UserID, true
}

func (r Roster) Joining(t Thread, userIDs []string, now time.Time) ([]Member, error) {
	joining := []Member{}
	for _, id := range shared.DistinctTrimmed(userIDs) {
		if _, member := r.Active(id); member {
			continue
		}
		joining = append(joining, t.member(id, RoleMember, now))
	}
	if len(joining) == 0 {
		return nil, ErrMembersRequired
	}
	if len(r.ActiveUserIDs())+len(joining) > MaxGroupMembers {
		return nil, ErrTooManyMembers
	}
	return joining, nil
}

func (r Roster) Leaving(userID string, now time.Time) ([]Member, error) {
	leaver, ok := r.Active(userID)
	if !ok {
		return nil, ErrNotFound
	}
	at := now.UTC()
	successor, promote := r.SuccessorAdmin(userID)
	leaver.LeftAt = &at
	changes := []Member{leaver}
	if promote {
		heir, _ := r.Active(successor)
		heir.Role = RoleAdmin
		changes = append(changes, heir)
	}
	return changes, nil
}
