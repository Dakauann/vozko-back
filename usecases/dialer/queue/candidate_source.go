package queue

import (
	"context"
	"sort"
	"strings"

	dialer "vozko/domain/dialer"
	dept "vozko/domain/workspace/workspace_department"
)

// AvailabilitySource is the reservation-aware "who is free right now" the session
// registry already computes (ListAvailable returns only members with no attached
// call and no live ring reservation). A narrow port so this adapter carries no
// registry implementation detail.
type AvailabilitySource interface {
	ListAvailable(workspaceID string) []dialer.DialerSession
}

// DepartmentMemberLister lists a department's member user ids. The concrete
// workspace_department repository satisfies it (same port the roulette uses).
type DepartmentMemberLister interface {
	ListMembers(departmentID string) ([]dept.DepartmentMember, error)
}

// TargetCandidateSource resolves the agents currently available to take a queued
// caller for a target, reusing the SAME reservation-aware availability the roulette
// and inbound routing use. It implements CandidateSource.
type TargetCandidateSource struct {
	avail   AvailabilitySource
	members DepartmentMemberLister // optional; only needed for department targets
}

func NewCandidateSource(avail AvailabilitySource, members DepartmentMemberLister) *TargetCandidateSource {
	return &TargetCandidateSource{avail: avail, members: members}
}

var _ CandidateSource = (*TargetCandidateSource)(nil)

func (s *TargetCandidateSource) AvailableForTarget(ctx context.Context, workspaceID string, target dialer.QueueTarget) ([]string, error) {
	if s == nil || s.avail == nil {
		return nil, nil
	}
	// The reservation-aware available pool, minus AI owners, de-duplicated (a member
	// may hold both a browser and a branch contact).
	avail := make([]string, 0)
	seen := make(map[string]bool)
	for _, sess := range s.avail.ListAvailable(workspaceID) {
		if sess == nil {
			continue
		}
		uid := sess.UserID()
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		avail = append(avail, uid)
	}

	switch target.Kind {
	case dialer.QueueTargetWorkspace:
		sort.Strings(avail)
		return avail, nil
	case dialer.QueueTargetAgent:
		// Wait for one specific agent: they are a candidate only when free.
		for _, id := range avail {
			if id == target.ID {
				return []string{id}, nil
			}
		}
		return nil, nil
	case dialer.QueueTargetDepartment:
		if s.members == nil {
			return nil, dialer.ErrQueueTargetInvalid
		}
		members, err := s.members.ListMembers(target.ID)
		if err != nil {
			return nil, err
		}
		allowed := make(map[string]bool, len(members))
		for _, m := range members {
			if uid := strings.TrimSpace(m.UserID); uid != "" {
				allowed[uid] = true
			}
		}
		scoped := make([]string, 0, len(avail))
		for _, id := range avail {
			if allowed[id] {
				scoped = append(scoped, id)
			}
		}
		sort.Strings(scoped)
		return scoped, nil
	}
	return nil, nil
}
