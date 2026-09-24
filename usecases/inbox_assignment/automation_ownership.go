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

// ErrHandOffTargetNotHuman: a hand-off must land on a person. A conversation
// goes to an agent or workflow only through the roulette, from its own settings.
var ErrHandOffTargetNotHuman = errors.New("inbox assignment: hand-off target must be a human")

// ErrNothingToReturnTo: no agent or workflow would answer the conversation
// (none configured, or the conversation is paused), so it cannot be handed back.
var ErrNothingToReturnTo = errors.New("inbox assignment: no agent or workflow governs this conversation")

// ErrAutomationStillActive: the hand-off stands, but the automation could not
// be paused for the conversation and may keep replying to the person's contact.
var ErrAutomationStillActive = errors.New("inbox assignment: handed off but the automation could not be paused")

// AutomationPauser is the existing per-conversation automation switch. Every
// agent reply path and workflow trigger honours it; none of them reads the
// assignee, which is why a hand-off must flip it.
type AutomationPauser interface {
	SetAutomation(ctx context.Context, entryID string, entryType shared.EntryType, enabled *bool) error
}

// AISessionEnder closes the conversation's open AI attendance session. The
// outcome decides the AI metrics: a hand-off is not containment.
type AISessionEnder interface {
	EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string)
}

// Why an open AI session ended, as recorded on the session.
const (
	sessionEndHandoff = "automation_handoff"
	sessionEndPaused  = "automation_paused"
)

// EntryAnnouncer tells open screens about a conversation: a change to its card,
// or a change of owner, where whoever can see it now gets the fresh row and
// whoever just lost it drops it. Every reassignment is announced from this
// service, so callers (workflow nodes, the agent tool, the rescue sweep, bulk
// actions) need no socket of their own.
type EntryAnnouncer interface {
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
	AnnounceOwnerChange(workspaceID, entryID, entryType, previousOwner string)
}

// SetAutomationGovernance makes the roulette give a conversation governed by an
// agent or workflow to that automation. Without it every conversation goes to
// the human ring, as before.
func (s *AssignmentService) SetAutomationGovernance(r conversation.EntryAutomationReader) {
	s.automationProfiles = r
}

func (s *AssignmentService) SetEntryBroadcaster(b EntryAnnouncer) { s.entryUpdates = b }

// SetAutomationPauser makes every hand-off take the automation out of the conversation.
func (s *AssignmentService) SetAutomationPauser(p AutomationPauser) { s.pauser = p }

// DepartmentLookup reads a department, to check it belongs to the workspace
// before its ring deals a conversation.
type DepartmentLookup interface {
	GetDepartmentByID(id string) (*dept.Department, error)
}

// EntryAccountReader names the channel account a conversation came in on,
// which keys the roulette pointer when no assignment has recorded it yet.
type EntryAccountReader interface {
	EntryAccountID(entryID, entryType string) (string, error)
}

// ConversationReceivers answers whether a member can open conversations in a
// workspace (the conversation authorizer's department scope).
type ConversationReceivers interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

func (s *AssignmentService) SetDepartmentLookup(d DepartmentLookup) { s.departments = d }

func (s *AssignmentService) SetEntryAccountReader(r EntryAccountReader) { s.accounts = r }

// SetConversationReceivers enables named hand-offs; without it they are refused.
func (s *AssignmentService) SetConversationReceivers(r ConversationReceivers) { s.receivers = r }

// SetAISessionEnder makes every hand-off and pause close the AI session as
// handed off, so a person later finishing the conversation is not counted as
// the AI resolving it.
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

// assignToAutomation hands a new conversation to the agent or workflow that
// governs it. The human ring's pointer is untouched: automation is not a member
// and costs nobody a turn.
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

// HandOffToHuman moves a conversation to a named person and takes the
// automation out of it. The person must be able to open conversations in the
// workspace, the same check the manual "assign to" makes. When an agent or
// workflow held it, that automation is recorded as the one who transferred it.
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
	stepErr := s.stepOut(workspaceID, entryID, entryType, toUserID)
	s.announceAfterStepOut(workspaceID, entryID, entryType, moved)
	return stepErr
}

// HandOffToRoulette deals a conversation to the next eligible person in the
// same ring inbound conversations use (the workspace's mode, eligibility and
// shared pointer), from the chosen department or the conversation's own, and
// takes the automation out of it. With nobody eligible the conversation goes to
// the team queue rather than staying hidden and unanswered. It returns the new
// owner, or "" for the team queue.
func (s *AssignmentService) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	departmentID, err := s.handOffDepartment(in)
	if err != nil {
		return "", err
	}

	existing, err := s.repo.FindByEntry(in.WorkspaceID, in.EntryID, in.EntryType)
	if err != nil {
		return "", fmt.Errorf("hand-off %s (%s): %w", in.EntryID, in.EntryType, err)
	}
	if existing != nil && !existing.HeldByAutomation() && in.DepartmentID == "" {
		stepErr := s.stepOut(in.WorkspaceID, in.EntryID, in.EntryType, existing.AssignedUserID)
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
		stepErr := s.stepOut(in.WorkspaceID, in.EntryID, in.EntryType, "")
		s.announceAfterStepOut(in.WorkspaceID, in.EntryID, in.EntryType, moved)
		return "", stepErr
	}

	userID, _, err := s.claimNextInRing(in.WorkspaceID, businessPhoneID, departmentID, pool)
	if err != nil {
		return "", fmt.Errorf("hand-off %s (%s): ring: %w", in.EntryID, in.EntryType, err)
	}
	by := strings.TrimSpace(in.ByActorID)
	if by == "" {
		by = handOffActor(existing)
	}
	moved, err := s.reassign(in.EntryID, in.EntryType, businessPhoneID, in.WorkspaceID, userID, by, ia.TriggerAutomationHandoffRoulette)
	if err != nil {
		return "", fmt.Errorf("hand-off %s (%s) to %s: %w", in.EntryID, in.EntryType, userID, err)
	}
	stepErr := s.stepOut(in.WorkspaceID, in.EntryID, in.EntryType, userID)
	s.announceAfterStepOut(in.WorkspaceID, in.EntryID, in.EntryType, moved)
	return userID, stepErr
}

// handOffDepartment is the department whose ring deals the hand-off: the one
// asked for, after checking it belongs to the workspace, or the conversation's.
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

// handOffAccount is the channel account (business phone for WhatsApp) that
// keys the roulette pointer: the one on record, else the conversation's own.
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

func (s *AssignmentService) canReceive(userID, workspaceID string) bool {
	if s.receivers == nil {
		return false
	}
	_, allowed := s.receivers.GetDepartmentScope(userID, workspaceID, false)
	return allowed
}

// ReleaseFromAutomation returns a conversation an agent or workflow holds to
// the team queue. A paused automation answers nobody, so it must not keep the
// conversation hidden. A person who owns it is left alone.
func (s *AssignmentService) ReleaseFromAutomation(entryID, entryType string) error {
	workspaceID, err := s.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || workspaceID == "" {
		return fmt.Errorf("release %s (%s): workspace: %w", entryID, entryType, errors.Join(err, conversation.ErrConversationNotFound))
	}
	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return fmt.Errorf("release %s (%s): %w", entryID, entryType, err)
	}
	if !existing.HeldByAutomation() {
		return nil
	}
	if err := s.UnassignSystem(entryID, entryType, workspaceID, ia.TriggerAutomationReleased); err != nil {
		return fmt.Errorf("release %s (%s): %w", entryID, entryType, err)
	}
	return nil
}

// ReturnToAutomation hands a conversation back to the agent or workflow that
// governs it now, which is the one that will answer: replies follow the
// channel's configuration, not whoever handed the conversation off. The
// conversation's automation switch must already be on. It returns the owner
// afterwards; with nothing to answer it returns ErrNothingToReturnTo and the
// current owner, untouched.
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
	if existing.HeldByAutomation() {
		return existing.AssignedUserID
	}
	return actor.SystemID
}

// stepOut takes the automation out of a conversation that was just handed off
// to toUserID ("" for the team queue): its AI session ends as handed off and
// its switch is paused. It runs only after the hand-off succeeded: pausing a
// conversation nobody took would leave the contact with no one at all.
func (s *AssignmentService) stepOut(workspaceID, entryID, entryType, toUserID string) error {
	s.endAISession(workspaceID, entryID, entryType, toUserID, sessionEndHandoff)
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

func (s *AssignmentService) endAISession(workspaceID, entryID, entryType, toUserID, reason string) {
	if s.aiSessions == nil || workspaceID == "" {
		return
	}
	s.aiSessions.EndOpenRaw(workspaceID, entryID, entryType, string(aa.OutcomeHandedOff), reason, toUserID)
}

// announceAfterStepOut tells open screens how a hand-off ended, once the
// automation has stepped out so the card shows it paused: a change of owner
// when the conversation moved, otherwise just the fresh card.
func (s *AssignmentService) announceAfterStepOut(workspaceID, entryID, entryType string, moved ownerMove) {
	if moved.changed {
		s.announceOwner(workspaceID, entryID, entryType, moved.previous)
		return
	}
	if s.entryUpdates != nil {
		s.entryUpdates.BroadcastEntryUpdate(entryID, entryType, nil)
	}
}
