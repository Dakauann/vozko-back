package inbox_assignment_usecase

import (
	"log"
	"strings"
	"time"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

const (
	// MaxRosterSize bounds how many members one roulette pool may hold.
	//
	// CanReceiveRoulette is a per-user permission question, and while the
	// workspace repository caches members and permission sets, a cold cache on
	// a 2000-member tenant would put 2000 round-trips on an inbound-message
	// path — a webhook that takes four seconds is a channel outage. Truncation
	// is logged, so it is visible; a slow webhook is not.
	MaxRosterSize = 500

	// rosterCacheTTL is short on purpose: membership, department and permission
	// changes must reach distribution quickly, and the workspace repository
	// already caches the expensive parts underneath.
	rosterCacheTTL = 60 * time.Second
)

// workspaceMemberLister is the slice of the workspace repository the roster
// needs. Narrow, so the roster does not import the 40-method interface for two
// calls.
type workspaceMemberLister interface {
	ListMembers(workspaceID string) ([]*workspace.Member, error)
}

type departmentMemberLister interface {
	ListMembers(departmentID string) ([]workspace_department.DepartmentMember, error)
}

// rosterCache is the two-method slice of shared state the roster needs.
// Declared here rather than taking cache.SharedState so the roster depends on
// what it uses, and so a test fake is two methods instead of twenty-five.
// Satisfied by the Redis shared state.
type rosterCache interface {
	GetString(key string) (string, error)
	SetString(key, value string, ttl time.Duration) error
}

// RosterService answers "who may receive a conversation right now, whether or
// not they are connected".
//
// It is the last_seen mode's pool source, and the deliberate counterpart to the
// WS hub: same permission predicate (ia.CanReceiveRoulette), same department
// narrowing (the department's member list), different population — workspace
// membership instead of open sockets.
type RosterService struct {
	members     workspaceMemberLister
	departments departmentMemberLister
	authz       ia.RoulettePermissionChecker
	shared      rosterCache
}

func NewRosterService(
	members workspaceMemberLister,
	departments departmentMemberLister,
	authz ia.RoulettePermissionChecker,
	shared rosterCache,
) *RosterService {
	return &RosterService{members: members, departments: departments, authz: authz, shared: shared}
}

var _ ia.RosterProvider = (*RosterService)(nil)

func (s *RosterService) ListRouletteMembers(workspaceID, departmentID string, skipAdmins bool) ([]string, error) {
	if s == nil || s.members == nil || workspaceID == "" {
		return nil, nil
	}
	departmentID = strings.TrimSpace(departmentID)

	if cached, ok := s.fromCache(workspaceID, departmentID, skipAdmins); ok {
		return cached, nil
	}

	members, err := s.members.ListMembers(workspaceID)
	if err != nil {
		return nil, err
	}

	var allowed map[string]bool
	if departmentID != "" {
		if s.departments == nil {
			log.Printf("[InboxAssignment] roster: no department repository wired for workspace %s department %s", workspaceID, departmentID)
			return nil, nil
		}
		deptMembers, err := s.departments.ListMembers(departmentID)
		if err != nil {
			return nil, err
		}
		allowed = make(map[string]bool, len(deptMembers))
		for _, dm := range deptMembers {
			if dm.UserID != "" {
				allowed[dm.UserID] = true
			}
		}
	}

	result := make([]string, 0, len(members))
	seen := make(map[string]bool, len(members))
	truncated := 0
	for _, m := range members {
		if m == nil || m.UserID == "" || seen[m.UserID] {
			continue
		}
		if allowed != nil && !allowed[m.UserID] {
			continue
		}
		if !ia.CanReceiveRoulette(s.authz, m.UserID, workspaceID, skipAdmins) {
			continue
		}
		if len(result) >= MaxRosterSize {
			truncated++
			continue
		}
		seen[m.UserID] = true
		result = append(result, m.UserID)
	}
	if truncated > 0 {
		log.Printf("[InboxAssignment] roster for workspace %s department %q truncated at %d, dropped %d eligible member(s)",
			workspaceID, departmentID, MaxRosterSize, truncated)
	}

	s.toCache(workspaceID, departmentID, skipAdmins, result)
	return result, nil
}

func (s *RosterService) cacheKey(workspaceID, departmentID string, skipAdmins bool) string {
	skip := "0"
	if skipAdmins {
		skip = "1"
	}
	return "roulette:roster:" + workspaceID + ":" + departmentID + ":" + skip
}

// fromCache never reports an error: a cache that cannot be read is a cache
// miss, not a distribution failure.
func (s *RosterService) fromCache(workspaceID, departmentID string, skipAdmins bool) ([]string, bool) {
	if s.shared == nil {
		return nil, false
	}
	raw, err := s.shared.GetString(s.cacheKey(workspaceID, departmentID, skipAdmins))
	if err != nil || raw == "" {
		return nil, false
	}
	// An empty roster is cached as the sentinel below rather than as "" so it
	// stays distinguishable from a miss — otherwise a workspace with nobody
	// eligible re-runs the full scan on every inbound message.
	if raw == emptyRosterSentinel {
		return []string{}, true
	}
	return strings.Split(raw, ","), true
}

const emptyRosterSentinel = "-"

func (s *RosterService) toCache(workspaceID, departmentID string, skipAdmins bool, userIDs []string) {
	if s.shared == nil {
		return
	}
	raw := emptyRosterSentinel
	if len(userIDs) > 0 {
		raw = strings.Join(userIDs, ",")
	}
	if err := s.shared.SetString(s.cacheKey(workspaceID, departmentID, skipAdmins), raw, rosterCacheTTL); err != nil {
		log.Printf("[InboxAssignment] roster cache write failed for workspace %s: %v", workspaceID, err)
	}
}
