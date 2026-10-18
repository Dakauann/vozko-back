package notification

import (
	"strings"

	"vozko/domain/workspace"
)

type AudienceKind string

const (
	AudienceEveryone   AudienceKind = "everyone"
	AudienceManagers   AudienceKind = "managers"
	AudienceUsers      AudienceKind = "users"
	AudiencePermission AudienceKind = "permission"
)

const MaxAudienceUsers = 500

type Audience struct {
	Kind       AudienceKind               `json:"kind"`
	UserIDs    []string                   `json:"userIds,omitempty"`
	Permission *workspace.PermissionEntry `json:"permission,omitempty"`
}

type Member struct {
	UserID  string
	Email   string
	Name    string
	Manager bool
}

func Everyone() Audience { return Audience{Kind: AudienceEveryone} }

func Managers() Audience { return Audience{Kind: AudienceManagers} }

func Users(ids ...string) Audience { return Audience{Kind: AudienceUsers, UserIDs: ids} }

func Holding(resource workspace.Resource, action workspace.Action) Audience {
	return Audience{Kind: AudiencePermission, Permission: &workspace.PermissionEntry{Resource: resource, Action: action}}
}

func (a Audience) Normalize() Audience {
	if a.Kind != AudienceUsers {
		a.UserIDs = nil
		return a
	}
	a.UserIDs = uniqueTrimmed(a.UserIDs)
	return a
}

func (a Audience) Validate() error {
	switch a.Kind {
	case AudienceEveryone, AudienceManagers:
		return nil
	case AudienceUsers:
		ids := uniqueTrimmed(a.UserIDs)
		if len(ids) == 0 || len(a.UserIDs) > MaxAudienceUsers {
			return ErrAudienceInvalid
		}
		return nil
	case AudiencePermission:
		if a.Permission == nil || strings.TrimSpace(string(a.Permission.Resource)) == "" || strings.TrimSpace(string(a.Permission.Action)) == "" {
			return ErrAudienceInvalid
		}
		return nil
	}
	return ErrAudienceInvalid
}

func (a Audience) Equal(b Audience) bool {
	if a.Kind != b.Kind {
		return false
	}
	if (a.Permission == nil) != (b.Permission == nil) || (a.Permission != nil && *a.Permission != *b.Permission) {
		return false
	}
	left, right := uniqueTrimmed(a.UserIDs), uniqueTrimmed(b.UserIDs)
	if len(left) != len(right) {
		return false
	}
	listed := make(map[string]bool, len(left))
	for _, id := range left {
		listed[id] = true
	}
	for _, id := range right {
		if !listed[id] {
			return false
		}
	}
	return true
}

func (a Audience) Select(members []Member, holds func(userID string) bool) []Member {
	listed := map[string]bool{}
	for _, id := range a.UserIDs {
		listed[strings.TrimSpace(id)] = true
	}
	out := make([]Member, 0, len(members))
	for _, m := range members {
		if a.includes(m, listed, holds) {
			out = append(out, m)
		}
	}
	return out
}

func (a Audience) includes(m Member, listed map[string]bool, holds func(string) bool) bool {
	switch a.Kind {
	case AudienceEveryone:
		return true
	case AudienceManagers:
		return m.Manager
	case AudienceUsers:
		return listed[m.UserID]
	case AudiencePermission:
		return holds != nil && holds(m.UserID)
	}
	return false
}

func uniqueTrimmed(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
