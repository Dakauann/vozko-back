package conversation

import (
	"errors"
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/inbox_assignment"
	"vozko/domain/shared"
)

var errPlacementsMissing = errors.New("conversation authorizer: the entry placement reader is not wired")

func (a *Authorizer) EntryVisibilityFor(userID, workspaceID string, isAdmin bool) conversation.EntryVisibility {
	return a.actor(userID, workspaceID, isAdmin)
}

func (x *actorAccess) VisibleEntries(refs []shared.EntryRef) (map[shared.EntryRef]bool, error) {
	visible := make(map[shared.EntryRef]bool, len(refs))
	var whatsapp []string
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		visible[ref] = false
		if x.userID == "" || ref.EntryID == "" || (!x.isAdmin && x.workspaceID == "") {
			continue
		}
		if ref.EntryType != shared.EntryTypeWhatsApp {
			visible[ref] = x.CanAccess(ref.EntryID, string(ref.EntryType))
			continue
		}
		if !seen[ref.EntryID] {
			seen[ref.EntryID] = true
			whatsapp = append(whatsapp, ref.EntryID)
		}
	}
	if len(whatsapp) == 0 {
		return visible, nil
	}

	allowed, err := x.visibleWhatsAppEntries(whatsapp)
	if err != nil {
		return nil, err
	}
	for ref := range visible {
		if ref.EntryType == shared.EntryTypeWhatsApp && allowed[ref.EntryID] {
			visible[ref] = true
		}
	}
	return visible, nil
}

func (x *actorAccess) visibleWhatsAppEntries(entryIDs []string) (map[string]bool, error) {
	a := x.authorizer
	if a.placements == nil {
		return nil, errPlacementsMissing
	}
	placements, err := a.placements.EntryPlacements(entryIDs)
	if err != nil {
		return nil, fmt.Errorf("placements of %d entries: %w", len(entryIDs), err)
	}
	allowed := make(map[string]bool, len(entryIDs))
	if x.isAdmin {
		for id := range placements {
			allowed[id] = true
		}
		return allowed, nil
	}

	assignments, err := x.whatsAppAssignments(entryIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range entryIDs {
		placement, ok := placements[id]
		if !ok || placement.WorkspaceID != x.workspaceID {
			continue
		}
		allowed[id] = x.reachesPlacedEntry(placement, assignments[id])
	}
	return allowed, nil
}

func (x *actorAccess) whatsAppAssignments(entryIDs []string) (map[string]*inbox_assignment.InboxAssignment, error) {
	byEntry := make(map[string]*inbox_assignment.InboxAssignment, len(entryIDs))
	if x.authorizer.assignmentRepo == nil {
		return byEntry, nil
	}
	found, err := x.authorizer.assignmentRepo.FindByEntries(x.workspaceID, entryIDs)
	if err != nil {
		return nil, fmt.Errorf("assignments of %d entries: %w", len(entryIDs), err)
	}
	for _, assignment := range found {
		if assignment != nil && assignment.EntryType == string(shared.EntryTypeWhatsApp) {
			byEntry[assignment.EntryID] = assignment
		}
	}
	return byEntry, nil
}

func (x *actorAccess) reachesPlacedEntry(placement conversation.EntryPlacement, assignment *inbox_assignment.InboxAssignment) bool {
	if !assignment.VisibleTo(x.userID) && !x.canViewOthers() {
		return false
	}
	scope, allowed := x.departmentScope()
	if !allowed {
		return false
	}
	if !scope.Restrict {
		return true
	}
	if placement.DepartmentID != "" && containsString(scope.DepartmentIDs, placement.DepartmentID) {
		return true
	}
	return assignment.AssignedTo(x.userID)
}
