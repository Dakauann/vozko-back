package workspace

import (
	"errors"
	"fmt"
	"strings"
)

var ErrUnknownCapability = errors.New("workspace: the capability is missing from the access catalog")

type PermissionChecker interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

func CapabilitiesRequire(keys []CapabilityKey) ([]PermissionEntry, error) {
	seen := map[string]bool{}
	var out []PermissionEntry
	for _, key := range keys {
		requires, ok := CapabilityRequires(key)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCapability, key)
		}
		for _, p := range requires {
			if seen[p.Key()] {
				continue
			}
			seen[p.Key()] = true
			out = append(out, p)
		}
	}
	return out, nil
}

func HoldsAll(checker PermissionChecker, workspaceID, userID string, isAdmin bool, entries []PermissionEntry) bool {
	if checker == nil || strings.TrimSpace(userID) == "" || len(entries) == 0 {
		return false
	}
	for _, p := range entries {
		if !checker.HasWorkspacePermission(userID, workspaceID, string(p.Resource), string(p.Action), isAdmin) {
			return false
		}
	}
	return true
}
