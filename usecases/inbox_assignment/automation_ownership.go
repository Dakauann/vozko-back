package inbox_assignment_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/actor"
	aa "vozko/domain/ai_attendance"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	dept "vozko/domain/workspace/workspace_department"
)

var ErrHandOffTargetNotHuman = errors.New("inbox assignment: hand-off target must be a human")

var ErrNothingToReturnTo = errors.New("inbox assignment: no agent or workflow governs this conversation")

var ErrAutomationStillActive = errors.New("inbox assignment: handed off but the automation could not be paused")

type AutomationPauser interface {
	SetAutomation(ctx context.Context, entryID string, entryType shared.EntryType, enabled *bool) error
}

type AISessionEnder = aa.SessionEnder

type EntryAnnouncer interface {
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
	AnnounceOwnerChange(workspaceID, entryID, entryType, previousOwner string)
}

func (s *AssignmentService) SetAutomationGovernance(r conversation.EntryAutomationReader) {
	s.automationProfiles = r
}

func (s *AssignmentService) SetEntryBroadcaster(b EntryAnnouncer) { s.entryUpdates = b }

func (s *AssignmentService) SetAutomationPauser(p AutomationPauser) { s.pauser = p }

type DepartmentLookup interface {
	GetDepartmentByID(id string) (*dept.Department, error)
}

type EntryAccountReader interface {
	EntryAccountID(entryID, entryType string) (string, error)
}

type ConversationReceivers interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
	ia.RoulettePermissionChecker
}

func (s *AssignmentService) SetDepartmentLookup(d DepartmentLookup) { s.departments = d }

func (s *AssignmentService) SetEntryAccountReader(r EntryAccountReader) { s.accounts = r }

func (s *AssignmentService) SetConversationReceivers(r ConversationReceivers) { s.receivers = r }

func (s *AssignmentService) SetAISessionEnder(e AISessionEnder) { s.aiSessions = e }

func (s *AssignmentService) governingAutomation(entryID, entryType string) (conversation.Automation, bool, error) {
	if s.automationProfiles == nil {
		return conversation.Automation{}, false, nil
	}
	profile, err := s.automationProfiles.EntryAutomation(entryID, entryType)
	if err != nil {
		return conversation.Automation{}, false, err
	}
	automation, governed := profile.Governing()
	return automation, governed, nil
}

func (s *AssignmentService) assignToAutomation(workspaceID, entryID, entryType, businessPhoneID, departmentID string, automation conversation.Automation) string {
	ownerID := automation.ActorID()
	if err := s.repo.Assign(&ia.InboxAssignment{
		WorkspaceID:     workspaceID,
		BusinessPhoneID: businessPhoneID,
		EntryID:         entryID,
		EntryType:       entryType,
		AssignedUserID:  ownerID,
	}); err != nil {
		log.Printf("[InboxAssignment] error assigning entry %s (%s) to %s: %v", entryID, entryType, ownerID, err)
		return ""
	}

	log.Printf("[InboxAssignment] assigned entry %s (%s) → %s (governed by its %s, phone %s)", entryID, entryType, ownerID, automation.Kind, businessPhoneID)

	s.recordHistoryAndEvent(recordInput{
		WorkspaceID:       workspaceID,
		EntryID:           entryID,
		EntryType:         entryType,
		AssignedUserID:    ownerID,
		Trigger:           ia.TriggerAutomationGoverned,
		AssignedByActorID: actor.SystemID,
		BusinessPhoneID:   businessPhoneID,
		DepartmentID:      departmentID,
		EventType:         ce.EventAutoAssigned,
		Channel:           channelForEntryType(entryType),
	})
	return ownerID
}

func (s *AssignmentService) HandOffToHuman(workspaceID, entryID, entryType, toUserID string) error {
	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" || actor.IsAutomation(toUserID) {
		return ErrHandOffTargetNotHuman
	}
	if !s.canReceive(toUserID, workspaceID) {
		return ia.ErrHandOffTargetNoAccess
	}

	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return fmt.Errorf("hand-off %s (%s): %w", entryID, entryType, err)
	}

	businessPhoneID := ""
	if existing != nil {
		businessPhoneID = existing.BusinessPhoneID
	}
	moved, err := s.reassign(entryID, entryType, businessPhoneID, workspaceID, toUserID, handOffActor(existing), ia.TriggerAutomationHandoff)
	if err != nil {
		return fmt.Errorf("hand-off %s (%s) to %s: %w", entryID, entryType, toUserID, err)
	}
	stepErr := s.stepOut(workspaceID, entryID, entryType, toUserID, handOffActor(existing), aa.EndReasonAutomationHandoff)
	s.announceAfterStepOut(workspaceID, entryID, entryType, moved)
	return stepErr
}

func (s *AssignmentService) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	departmentID, err := s.handOffDepartment(in)
	if err != nil {
		return "", err
	}

	existing, err := s.repo.FindByEntry(in.WorkspaceID, in.EntryID, in.EntryType)
	if err != nil {
		return "", fmt.Errorf("hand-off %s (%s): %w", in.EntryID, in.EntryType, err)
	}
	by := strings.TrimSpace(in.ByActorID)
	if by == "" {
		by = handOffActor(existing)
	}
	if existing != nil && !existing.HeldByAutomation() && in.DepartmentID == "" {
		stepErr := s.stepOut(in.WorkspaceID, in.EntryID, in.EntryType, existing.AssignedUserID, by, aa.EndReasonAutomationHandoff)
		s.announceAfterStepOut(in.WorkspaceID, in.EntryID, in.EntryType, ownerMove{})
		return existing.AssignedUserID, stepErr
	}

	businessPhoneID := s.handOffAccount(existing, in)
	pool := s.humanPool(in.WorkspaceID, departmentID)
	if len(pool.Ring) == 0 || businessPhoneID == "" {
		log.Printf("[InboxAssignment] hand-off of entry %s (%s): no ring to draw from in department %q, releasing it to the team", in.EntryID, in.EntryType, departmentID)
		moved, err := s.unassign(in.EntryID, in.EntryType, in.WorkspaceID, ia.TriggerAutomationHandoff)
		if err != nil {
			return "", fmt.Errorf("hand-off %s (%s): release: %w", in.EntryID, in.EntryType, err)
		}
		stepErr := s.stepOut(in.WorkspaceID, in.EntryID, in.EntryType, "", by, aa.EndReasonAutomationHandoff)
		s.announceAfterStepOut(in.WorkspaceID, in.EntryID, in.EntryType, moved)
		return "", stepErr
	}

	userID, _, err := s.claimNextInRing(in.WorkspaceID, businessPhoneID, departmentID, pool)
	if err != nil {
		return "", fmt.Errorf("hand-off %s (%s): ring: %w", in.EntryID, in.EntryType, err)
	}
	moved, err := s.reassign(in.EntryID, in.EntryType, businessPhoneID, in.WorkspaceID, userID, by, ia.TriggerAutomationHandoffRoulette)
	if err != nil {
		return "", fmt.Errorf("hand-off %s (%s) to %s: %w", in.EntryID, in.EntryType, userID, err)
	}
	stepErr := s.stepOut(in.WorkspaceID, in.EntryID, in.EntryType, userID, by, aa.EndReasonAutomationHandoff)
	s.announceAfterStepOut(in.WorkspaceID, in.EntryID, in.EntryType, moved)
	return userID, stepErr
}

func (s *AssignmentService) handOffDepartment(in ia.RouletteHandOff) (string, error) {
	departmentID := strings.TrimSpace(in.DepartmentID)
	if departmentID == "" {
		d, err := s.workspaceResolver.GetEntryDepartmentID(in.EntryID, in.EntryType)
		if err != nil {
			return "", fmt.Errorf("hand-off %s (%s): department: %w", in.EntryID, in.EntryType, err)
		}
		return d, nil
	}
	if s.departments == nil {
		return "", ia.ErrDepartmentOutOfScope
	}
	d, err := s.departments.GetDepartmentByID(departmentID)
	if err != nil || d == nil || d.WorkspaceID != in.WorkspaceID {
		return "", fmt.Errorf("hand-off %s (%s) to department %s: %w", in.EntryID, in.EntryType, departmentID, ia.ErrDepartmentOutOfScope)
	}
	return departmentID, nil
}

func (s *AssignmentService) handOffAccount(existing *ia.InboxAssignment, in ia.RouletteHandOff) string {
	if existing != nil && existing.BusinessPhoneID != "" {
		return existing.BusinessPhoneID
	}
	if s.accounts == nil {
		return ""
	}
	account, err := s.accounts.EntryAccountID(in.EntryID, in.EntryType)
	if err != nil {
		log.Printf("[InboxAssignment] hand-off of entry %s (%s): account: %v", in.EntryID, in.EntryType, err)
		return ""
	}
	return account
}

func (s *AssignmentService) mayTakeOver(userID, workspaceID string) bool {
	if !s.canReceive(userID, workspaceID) {
		return false
	}
	skipAdmins := false
	if cfg := s.workspaceConfigFor(workspaceID); cfg != nil {
		skipAdmins = cfg.SkipAdminAssignment
	}
	return ia.CanReceiveRoulette(s.receivers, userID, workspaceID, skipAdmins)
}

func (s *AssignmentService) canReceive(userID, workspaceID string) bool {
	if s.receivers == nil {
		return false
	}
	_, allowed := s.receivers.GetDepartmentScope(userID, workspaceID, false)
	return allowed
}

func (s *AssignmentService) TakeOverFromAutomation(entryID, entryType, actorUserID string) (string, error) {
	workspaceID, err := s.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || workspaceID == "" {
		return "", fmt.Errorf("take over %s (%s): workspace: %w", entryID, entryType, errors.Join(err, conversation.ErrConversationNotFound))
	}
	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return "", fmt.Errorf("take over %s (%s): %w", entryID, entryType, err)
	}
	if existing != nil && !existing.HeldByAutomation() {
		return existing.AssignedUserID, nil
	}

	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID != "" && !actor.IsAutomation(actorUserID) && s.mayTakeOver(actorUserID, workspaceID) {
		businessPhoneID := ""
		if existing != nil {
			businessPhoneID = existing.BusinessPhoneID
		}
		if err := s.AssignManual(entryID, entryType, businessPhoneID, workspaceID, actorUserID, actorUserID, ia.TriggerAutomationTakenOver); err != nil {
			return "", fmt.Errorf("take over %s (%s) by %s: %w", entryID, entryType, actorUserID, err)
		}
		return actorUserID, nil
	}

	if existing == nil {
		return "", nil
	}
	if err := s.UnassignSystem(entryID, entryType, workspaceID, ia.TriggerAutomationReleased); err != nil {
		return "", fmt.Errorf("release %s (%s): %w", entryID, entryType, err)
	}
	return "", nil
}

func (s *AssignmentService) ReturnToAutomation(entryID, entryType, actorUserID string) (string, error) {
	workspaceID, err := s.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || workspaceID == "" {
		return "", fmt.Errorf("return %s (%s): workspace: %w", entryID, entryType, errors.Join(err, conversation.ErrConversationNotFound))
	}
	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return "", fmt.Errorf("return %s (%s): %w", entryID, entryType, err)
	}
	current := ""
	businessPhoneID := ""
	if existing != nil {
		current = existing.AssignedUserID
		businessPhoneID = existing.BusinessPhoneID
	}

	automation, governed, err := s.governingAutomation(entryID, entryType)
	if err != nil {
		return current, fmt.Errorf("return %s (%s): %w", entryID, entryType, err)
	}
	if !governed {
		return current, ErrNothingToReturnTo
	}

	ownerID := automation.ActorID()
	if current == ownerID {
		return ownerID, nil
	}
	if err := s.AssignManual(entryID, entryType, businessPhoneID, workspaceID, ownerID, actorUserID, ia.TriggerAutomationResumed); err != nil {
		return current, fmt.Errorf("return %s (%s) to %s: %w", entryID, entryType, ownerID, err)
	}
	return ownerID, nil
}

func handOffActor(existing *ia.InboxAssignment) string {
	if existing != nil && existing.HeldByAutomation() {
		return existing.AssignedUserID
	}
	return actor.SystemID
}

func (s *AssignmentService) stepOut(workspaceID, entryID, entryType, toUserID, by, reason string) error {
	s.endAISession(workspaceID, entryID, entryType, toUserID, by, reason)
	if s.pauser == nil {
		return nil
	}
	off := false
	if err := s.pauser.SetAutomation(context.Background(), entryID, shared.EntryType(entryType), &off); err != nil {
		log.Printf("[InboxAssignment] entry %s (%s) was handed off but the automation could not be paused: %v", entryID, entryType, err)
		return fmt.Errorf("%w: %s (%s): %v", ErrAutomationStillActive, entryID, entryType, err)
	}
	return nil
}

func (s *AssignmentService) endAISession(workspaceID, entryID, entryType, toUserID, by, reason string) {
	if s.aiSessions == nil || workspaceID == "" {
		return
	}
	s.aiSessions.End(aa.EndRequest{
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   entryType,
		Outcome:     aa.OutcomeHandedOff,
		Reason:      reason,
		HandoffTo:   toUserID,
		EndedBy:     by,
	})
}

func (s *AssignmentService) announceAfterStepOut(workspaceID, entryID, entryType string, moved ownerMove) {
	if moved.changed {
		s.announceOwner(workspaceID, entryID, entryType, moved.previous)
		return
	}
	if s.entryUpdates != nil {
		s.entryUpdates.BroadcastEntryUpdate(entryID, entryType, nil)
	}
}
