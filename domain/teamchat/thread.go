package teamchat

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

type Kind string

const (
	KindDirect Kind = "direct"
	KindGroup  Kind = "group"
)

const (
	MaxTitleRunes   = 80
	MaxGroupMembers = 256
)

type Thread struct {
	ID            string
	WorkspaceID   string
	Kind          Kind
	Title         string
	DirectKey     string
	CreatedBy     string
	LastSeq       int64
	LastMessageAt time.Time
	CreatedAt     time.Time
}

func DirectKey(a, b string) string {
	pair := []string{strings.TrimSpace(a), strings.TrimSpace(b)}
	sort.Strings(pair)
	return pair[0] + ":" + pair[1]
}

func NewDirect(id, workspaceID, me, other string, now time.Time) (Thread, []Member, error) {
	me, other = strings.TrimSpace(me), strings.TrimSpace(other)
	switch {
	case strings.TrimSpace(workspaceID) == "":
		return Thread{}, nil, ErrWorkspaceRequired
	case me == "" || other == "":
		return Thread{}, nil, ErrPersonRequired
	case me == other:
		return Thread{}, nil, ErrSamePerson
	}
	thread := newThread(id, workspaceID, KindDirect, me, now)
	thread.DirectKey = DirectKey(me, other)
	return thread, []Member{
		thread.member(me, RoleMember, now),
		thread.member(other, RoleMember, now),
	}, nil
}

func NewGroup(id, workspaceID, creator, title string, others []string, now time.Time) (Thread, []Member, error) {
	creator = strings.TrimSpace(creator)
	switch {
	case strings.TrimSpace(workspaceID) == "":
		return Thread{}, nil, ErrWorkspaceRequired
	case creator == "":
		return Thread{}, nil, ErrPersonRequired
	}
	clean, err := NormalizeTitle(title)
	if err != nil {
		return Thread{}, nil, err
	}
	people := withoutPerson(shared.DistinctTrimmed(others), creator)
	if len(people) == 0 {
		return Thread{}, nil, ErrMembersRequired
	}
	if len(people)+1 > MaxGroupMembers {
		return Thread{}, nil, ErrTooManyMembers
	}
	thread := newThread(id, workspaceID, KindGroup, creator, now)
	thread.Title = clean
	members := make([]Member, 0, len(people)+1)
	members = append(members, thread.member(creator, RoleAdmin, now))
	for _, person := range people {
		members = append(members, thread.member(person, RoleMember, now))
	}
	return thread, members, nil
}

func NormalizeTitle(title string) (string, error) {
	clean := strings.TrimSpace(title)
	if clean == "" {
		return "", ErrTitleRequired
	}
	if utf8.RuneCountInString(clean) > MaxTitleRunes {
		return "", ErrTitleTooLong
	}
	return clean, nil
}

func (t Thread) IsGroup() bool {
	return t.Kind == KindGroup
}

func (t Thread) ManagedBy(by Member) error {
	if !t.IsGroup() {
		return ErrDirectThread
	}
	if by.Role != RoleAdmin {
		return ErrNotAdmin
	}
	return nil
}

func (t Thread) Renamed(title string) (Thread, error) {
	if !t.IsGroup() {
		return Thread{}, ErrDirectThread
	}
	clean, err := NormalizeTitle(title)
	if err != nil {
		return Thread{}, err
	}
	t.Title = clean
	return t, nil
}

func newThread(id, workspaceID string, kind Kind, creator string, now time.Time) Thread {
	at := now.UTC()
	return Thread{
		ID:            strings.TrimSpace(id),
		WorkspaceID:   strings.TrimSpace(workspaceID),
		Kind:          kind,
		CreatedBy:     creator,
		LastMessageAt: at,
		CreatedAt:     at,
	}
}

func (t Thread) member(userID string, role Role, now time.Time) Member {
	return Member{
		ThreadID:    t.ID,
		WorkspaceID: t.WorkspaceID,
		UserID:      userID,
		Role:        role,
		LastReadSeq: t.LastSeq,
		JoinedAt:    now.UTC(),
	}
}

func withoutPerson(ids []string, person string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != person {
			out = append(out, id)
		}
	}
	return out
}
