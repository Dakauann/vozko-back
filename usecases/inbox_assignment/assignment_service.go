package inbox_assignment_usecase

import (
	"context"
	"log"

	"time"

	"vozko/domain/actor"
	conversation "vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	wsc "vozko/domain/workspace_config"
)

type WorkspaceConfigProvider interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type AssignmentService struct {
	repo               ia.Repository
	history            ia.HistoryRepository
	telemetry          crm_telemetry.Publisher
	events             ce.Logger
	eligibleUsers      conversation.EligibleUserProvider
	workspaceResolver  conversation.CampaignWorkspaceResolver
	workspaceConfig    WorkspaceConfigProvider
	candidates         *CandidateResolver
	automationProfiles conversation.EntryAutomationReader
	entryUpdates       EntryAnnouncer
	pauser             AutomationPauser
	aiSessions         AISessionEnder
	departments        DepartmentLookup
	accounts           EntryAccountReader
	receivers          ConversationReceivers
}

func NewAssignmentService(
	repo ia.Repository,
	eligibleUsers conversation.EligibleUserProvider,
	workspaceResolver conversation.CampaignWorkspaceResolver,
	workspaceConfig WorkspaceConfigProvider,
) *AssignmentService {
	return &AssignmentService{
		repo:              repo,
		eligibleUsers:     eligibleUsers,
		workspaceResolver: workspaceResolver,
		workspaceConfig:   workspaceConfig,
		candidates:        NewCandidateResolver(eligibleUsers),
	}
}

func (s *AssignmentService) SetRoster(roster ia.RosterProvider) { s.candidates.SetRoster(roster) }

func (s *AssignmentService) SetPresence(seen ia.LastSeenReader) { s.candidates.SetPresence(seen) }

func (s *AssignmentService) Candidates() *CandidateResolver { return s.candidates }

func (s *AssignmentService) workspaceConfigFor(workspaceID string) *wsc.WorkspaceConfig {
	if s.workspaceConfig == nil {
		return nil
	}
	cfg, err := s.workspaceConfig.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil {
		return nil
	}
	return cfg
}

func (s *AssignmentService) SetHistory(h ia.HistoryRepository) { s.history = h }

func (s *AssignmentService) SetTelemetry(p crm_telemetry.Publisher) { s.telemetry = p }

func (s *AssignmentService) SetEventLogger(l ce.Logger) { s.events = l }

func (s *AssignmentService) EnsureAssignment(entryID, entryType, businessPhoneID string) string {

	workspaceID, err := s.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || workspaceID == "" {
		log.Printf("[InboxAssignment] cannot resolve workspace for entry %s (%s): %v", entryID, entryType, err)
		return ""
	}

	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		log.Printf("[InboxAssignment] error checking existing assignment for %s: %v", entryID, err)
		return ""
	}
	if existing != nil {
		return existing.AssignedUserID
	}

	departmentID, err := s.workspaceResolver.GetEntryDepartmentID(entryID, entryType)
	if err != nil {
		log.Printf("[InboxAssignment] cannot resolve department for entry %s (%s): %v", entryID, entryType, err)
		return ""
	}

	governor, governed, err := s.governingAutomation(entryID, entryType)
	if err != nil {
		log.Printf("[InboxAssignment] cannot tell whether an agent or workflow governs entry %s (%s), leaving it for the next message: %v", entryID, entryType, err)
		return ""
	}
	if governed {
		return s.assignToAutomation(workspaceID, entryID, entryType, businessPhoneID, departmentID, governor)
	}

	pool := s.humanPool(workspaceID, departmentID)
	if len(pool.Ring) == 0 {
		if departmentID != "" {
			log.Printf("[InboxAssignment] no connected eligible users for workspace %s department %s, entry %s stays unassigned", workspaceID, departmentID, entryID)
		} else {
			log.Printf("[InboxAssignment] no connected eligible users for workspace %s, entry %s stays unassigned (visible to all)", workspaceID, entryID)
		}
		return ""
	}

	assignedUserID, nextIndex, err := s.claimNextInRing(workspaceID, businessPhoneID, departmentID, pool)
	if err != nil {
		log.Printf("[InboxAssignment] error getting round-robin state: %v", err)
		return ""
	}

	assignment := &ia.InboxAssignment{
		WorkspaceID:     workspaceID,
		BusinessPhoneID: businessPhoneID,
		EntryID:         entryID,
		EntryType:       entryType,
		AssignedUserID:  assignedUserID,
	}
	if err := s.repo.Assign(assignment); err != nil {
		log.Printf("[InboxAssignment] error creating assignment for entry %s → user %s: %v", entryID, assignedUserID, err)
		return ""
	}

	log.Printf("[InboxAssignment] assigned entry %s (%s) → user %s (index %d/%d, mode=%s reason=%s, phone %s)",
		entryID, entryType, assignedUserID, nextIndex, len(pool.Ring), pool.Mode, pool.Reason, businessPhoneID)

	s.recordHistoryAndEvent(recordInput{
		WorkspaceID:       workspaceID,
		EntryID:           entryID,
		EntryType:         entryType,
		AssignedUserID:    assignedUserID,
		PreviousUserID:    "",
		Trigger:           ia.TriggerInboundRR,
		AssignedByActorID: actor.SystemID,
		BusinessPhoneID:   businessPhoneID,
		DepartmentID:      departmentID,
		EventType:         ce.EventAutoAssigned,
		Channel:           channelForEntryType(entryType),
	})

	return assignedUserID
}

// humanPool is the ring of people who may receive a conversation in this
// workspace and department, under the workspace's roulette settings.
func (s *AssignmentService) humanPool(workspaceID, departmentID string) Pool {
	cfg := s.workspaceConfigFor(workspaceID)
	skipAdmins := false
	if cfg != nil {
		skipAdmins = cfg.SkipAdminAssignment
	}
	log.Printf("[InboxAssignment] workspace %s: skipAdmins=%v department=%q", workspaceID, skipAdmins, departmentID)
	return s.candidates.Resolve(workspaceID, departmentID, skipAdmins, cfg)
}

func (s *AssignmentService) GetAssignedUserID(workspaceID, entryID, entryType string) string {
	a, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil || a == nil {
		return ""
	}
	return a.AssignedUserID
}

func (s *AssignmentService) IsAssignedToUser(workspaceID, entryID, entryType, userID string) bool {
	ok, err := s.repo.IsAssignedToUser(workspaceID, entryID, entryType, userID)
	if err != nil {
		return false
	}
	return ok
}

func (s *AssignmentService) Reassign(entryID, entryType, businessPhoneID, workspaceID, userID string) error {
	return s.AssignManual(entryID, entryType, businessPhoneID, workspaceID, userID, userID, ia.TriggerManual)
}

// ownerMove is how a reassignment went: whether the owner changed, and from whom.
type ownerMove struct {
	changed  bool
	previous string
}

// AssignManual gives the conversation to toUserID and announces it: whoever
// can see it now gets the fresh row, whoever lost it drops it.
func (s *AssignmentService) AssignManual(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger string) error {
	moved, err := s.reassign(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger)
	if err != nil {
		return err
	}
	if moved.changed {
		s.announceOwner(workspaceID, entryID, entryType, moved.previous)
	}
	return nil
}

func (s *AssignmentService) announceOwner(workspaceID, entryID, entryType, previousOwner string) {
	if s.entryUpdates != nil {
		s.entryUpdates.AnnounceOwnerChange(workspaceID, entryID, entryType, previousOwner)
	}
}

// reassign records the new owner without announcing it, for flows that announce
// once they are complete.
func (s *AssignmentService) reassign(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger string) (ownerMove, error) {
	prev := ""
	if existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType); err == nil && existing != nil {
		if existing.AssignedUserID == toUserID {
			return ownerMove{}, nil
		}
		prev = existing.AssignedUserID
	}

	if err := s.repo.Assign(&ia.InboxAssignment{
		WorkspaceID:     workspaceID,
		BusinessPhoneID: businessPhoneID,
		EntryID:         entryID,
		EntryType:       entryType,
		AssignedUserID:  toUserID,
	}); err != nil {
		return ownerMove{}, err
	}

	evType := ce.EventAssigned
	if trigger == ia.TriggerOpen || trigger == ia.TriggerInboundRR {
		evType = ce.EventAutoAssigned
	}

	dept := ""
	if s.workspaceResolver != nil {
		if d, err := s.workspaceResolver.GetEntryDepartmentID(entryID, entryType); err == nil {
			dept = d
		}
	}

	s.recordHistoryAndEvent(recordInput{
		WorkspaceID:       workspaceID,
		EntryID:           entryID,
		EntryType:         entryType,
		AssignedUserID:    toUserID,
		PreviousUserID:    prev,
		Trigger:           trigger,
		AssignedByActorID: assignedBy,
		BusinessPhoneID:   businessPhoneID,
		DepartmentID:      dept,
		EventType:         evType,
		Channel:           channelForEntryType(entryType),
	})
	return ownerMove{changed: true, previous: prev}, nil
}

func (s *AssignmentService) AssignOnOpen(entryID, entryType, businessPhoneID, workspaceID, userID string) (bool, error) {
	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	if err := s.AssignManual(entryID, entryType, businessPhoneID, workspaceID, userID, userID, ia.TriggerOpen); err != nil {
		return false, err
	}
	return true, nil
}

// UnassignSystem puts the conversation in the team queue and announces it.
func (s *AssignmentService) UnassignSystem(entryID, entryType, workspaceID, reason string) error {
	moved, err := s.unassign(entryID, entryType, workspaceID, reason)
	if err != nil {
		return err
	}
	if moved.changed {
		s.announceOwner(workspaceID, entryID, entryType, moved.previous)
	}
	return nil
}

// unassign records the release without announcing it.
func (s *AssignmentService) unassign(entryID, entryType, workspaceID, reason string) (ownerMove, error) {
	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return ownerMove{}, err
	}
	if existing == nil {
		return ownerMove{}, nil
	}
	prev := existing.AssignedUserID

	if err := s.repo.Unassign(workspaceID, entryID, entryType); err != nil {
		return ownerMove{}, err
	}

	dept := ""
	if s.workspaceResolver != nil {
		if d, err := s.workspaceResolver.GetEntryDepartmentID(entryID, entryType); err == nil {
			dept = d
		}
	}

	s.recordHistoryAndEvent(recordInput{
		WorkspaceID:       workspaceID,
		EntryID:           entryID,
		EntryType:         entryType,
		AssignedUserID:    "",
		PreviousUserID:    prev,
		Trigger:           reason,
		AssignedByActorID: actor.SystemID,
		BusinessPhoneID:   existing.BusinessPhoneID,
		DepartmentID:      dept,
		EventType:         ce.EventUnassigned,
		Channel:           channelForEntryType(entryType),
	})
	return ownerMove{changed: true, previous: prev}, nil
}

type recordInput struct {
	WorkspaceID       string
	EntryID           string
	EntryType         string
	AssignedUserID    string
	PreviousUserID    string
	Trigger           string
	AssignedByActorID string
	BusinessPhoneID   string
	DepartmentID      string
	EventType         ce.EventType
	Channel           string
}

func (s *AssignmentService) recordHistoryAndEvent(in recordInput) {
	now := time.Now().UTC()
	actorKind := string(actor.KindHuman)
	if actor.IsAutomation(in.AssignedUserID) {
		actorKind = string(actor.KindOf(in.AssignedUserID))
	}

	if s.telemetry != nil {
		histID := ""
		_ = histID
		_ = s.telemetry.Publish(crm_telemetry.KindAssignmentHistory, crm_telemetry.AssignmentHistoryPayload{
			WorkspaceID:       in.WorkspaceID,
			EntryID:           in.EntryID,
			EntryType:         in.EntryType,
			ActorKind:         actorKind,
			AssignedActorID:   in.AssignedUserID,
			PreviousActorID:   in.PreviousUserID,
			Trigger:           in.Trigger,
			AssignedByActorID: in.AssignedByActorID,
			BusinessPhoneID:   in.BusinessPhoneID,
			DepartmentID:      in.DepartmentID,
			StartedAt:         now,
		})
	} else if s.history != nil {
		s.appendHistory(in, actorKind, now)
	}

	if s.events != nil {
		details := map[string]string{"trigger": in.Trigger}
		if in.AssignedUserID != "" {
			details["to_user_id"] = in.AssignedUserID
		}
		if in.PreviousUserID != "" {
			details["from_user_id"] = in.PreviousUserID
		}
		s.events.Log(ce.New(in.WorkspaceID, in.EntryID, in.EntryType, in.EventType).
			WithChannel(in.Channel).
			WithDetails(details).
			WithActor(in.AssignedByActorID).
			Build())
	}
}

// appendHistory closes the open interval and, unless the conversation was just
// unassigned, opens the next one.
func (s *AssignmentService) appendHistory(in recordInput, actorKind string, now time.Time) {
	if err := s.history.CloseOpen(in.WorkspaceID, in.EntryID, in.EntryType, now); err != nil {
		log.Printf("[InboxAssignment] history CloseOpen: %v", err)
	}
	if in.AssignedUserID == "" {
		return
	}
	h := &ia.AssignmentHistory{
		WorkspaceID:       in.WorkspaceID,
		EntryID:           in.EntryID,
		EntryType:         in.EntryType,
		ActorKind:         actorKind,
		AssignedActorID:   in.AssignedUserID,
		PreviousActorID:   in.PreviousUserID,
		Trigger:           in.Trigger,
		AssignedByActorID: in.AssignedByActorID,
		BusinessPhoneID:   in.BusinessPhoneID,
		DepartmentID:      in.DepartmentID,
		StartedAt:         now,
	}
	if err := s.history.Append(h); err != nil {
		log.Printf("[InboxAssignment] history Append: %v", err)
	}
}

func channelForEntryType(entryType string) string {
	return shared.EntryType(entryType).EventChannel()
}

const maxRoundRobinAttempts = 3

func (s *AssignmentService) claimNextInRing(workspaceID, businessPhoneID, departmentID string, pool Pool) (string, int, error) {
	var (
		userID string
		idx    int
	)
	for attempt := 1; attempt <= maxRoundRobinAttempts; attempt++ {
		state, err := s.repo.GetRoundRobinState(workspaceID, businessPhoneID, departmentID)
		if err != nil {
			return "", 0, err
		}

		lastAssigned, stateID := "", ""
		if state != nil {
			lastAssigned = state.LastAssignedUserID
			stateID = state.ID
		}

		idx = ia.NextIndex(pool.Ring, lastAssigned, pool.Resume)
		userID = pool.Ring[idx]

		claimed, err := s.repo.CompareAndSwapRoundRobinState(&ia.RoundRobinState{
			ID:                 stateID,
			WorkspaceID:        workspaceID,
			BusinessPhoneID:    businessPhoneID,
			DepartmentID:       departmentID,
			LastAssignedUserID: userID,
		}, lastAssigned)
		if err != nil {
			log.Printf("[InboxAssignment] error saving round-robin state: %v", err)
			return userID, idx, nil
		}
		if claimed {
			return userID, idx, nil
		}
		log.Printf("[InboxAssignment] round-robin pointer moved under us for workspace %s (phone %s, department %q, attempt %d/%d); recomputing",
			workspaceID, businessPhoneID, departmentID, attempt, maxRoundRobinAttempts)
	}

	log.Printf("[InboxAssignment] round-robin contention unresolved for workspace %s after %d attempts; assigning %s anyway",
		workspaceID, maxRoundRobinAttempts, userID)
	return userID, idx, nil
}
