package workspace

import (
	"fmt"
	"sort"
	"strings"
)

func (e PermissionEntry) Key() string {
	return string(e.Resource) + ":" + string(e.Action)
}

func ParsePermissionEntries(raw []string) ([]PermissionEntry, error) {
	out := make([]PermissionEntry, 0, len(raw))
	for _, item := range raw {
		resource, action, found := strings.Cut(strings.TrimSpace(item), ":")
		if !found {
			return nil, fmt.Errorf("%w: %q", ErrInvalidResource, item)
		}
		entry := PermissionEntry{Resource: Resource(resource), Action: Action(action)}
		if _, known := ResourceActions[entry.Resource]; !known {
			return nil, fmt.Errorf("%w: %q", ErrInvalidResource, item)
		}
		if !ValidActionForResource(entry.Resource, entry.Action) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidAction, item)
		}
		out = append(out, entry)
	}
	return out, nil
}

func ApplyPermissionChanges(current, grant, revoke []PermissionEntry) []PermissionEntry {
	set := make(map[string]PermissionEntry, len(current)+len(grant))
	for _, e := range current {
		set[e.Key()] = e
	}
	for _, e := range grant {
		set[e.Key()] = e
	}
	for _, e := range revoke {
		delete(set, e.Key())
	}
	out := make([]PermissionEntry, 0, len(set))
	for _, e := range set {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func UnmetRequirements(permissions []PermissionEntry) map[PermissionEntry][]PermissionEntry {
	held := make(map[string]bool, len(permissions))
	for _, p := range permissions {
		held[p.Key()] = true
	}
	unmet := map[PermissionEntry][]PermissionEntry{}
	for _, p := range permissions {
		for _, required := range RequirementsOf(p) {
			if !held[required.Key()] {
				unmet[p] = append(unmet[p], required)
			}
		}
	}
	return unmet
}

func RequirementsOf(p PermissionEntry) []PermissionEntry {
	for _, def := range ResourceActions[p.Resource] {
		if def.ActionName == p.Action {
			return def.Requires
		}
	}
	return nil
}

func DescribePermission(p PermissionEntry) string {
	for _, def := range ResourceActions[p.Resource] {
		if def.ActionName == p.Action && def.Description != "" {
			return def.Description
		}
	}
	return p.Key()
}
