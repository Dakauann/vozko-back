package inbox_assignment_usecase

import (
	"context"
	"log"
	"sort"
	"time"

	"vozko/domain/actor"
	conversation "vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

type WorkspaceConfigProvider interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type AssignmentService struct {
	repo              ia.Repository
	history           ia.HistoryRepository // optional; prefer telemetry pub for hot path
	telemetry         crm_telemetry.Publisher
	events            ce.Logger
	eligibleUsers     conversation.EligibleUserProvider
	workspaceResolver conversation.CampaignWorkspaceResolver
	workspaceConfig   WorkspaceConfigProvider
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
	}
}

// SetHistory enables direct ownership interval recording (tests / consumer only).
// Prefer SetTelemetry for production hot paths.
func (s *AssignmentService) SetHistory(h ia.HistoryRepository) { s.history = h }

// SetTelemetry enqueues assignment_history (and relies on events logger for timeline).
func (s *AssignmentService) SetTelemetry(p crm_telemetry.Publisher) { s.telemetry = p }

// SetEventLogger enables timeline events for assignment mutations (should be queue-backed).
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

	skipAdmins := false
	if s.workspaceConfig != nil {
		if cfg, err := s.workspaceConfig.GetByWorkspaceID(context.Background(), workspaceID); err == nil && cfg != nil {
			skipAdmins = cfg.SkipAdminAssignment
		}
	}

	log.Printf("[InboxAssignment] workspace %s: skipAdmins=%v for entry %s (%s)", workspaceID, skipAdmins, entryID, entryType)

	departmentID, err := s.workspaceResolver.GetEntryDepartmentID(entryID, entryType)
	if err != nil {
		log.Printf("[InboxAssignment] cannot resolve department for entry %s (%s): %v", entryID, entryType, err)
		return ""
	}

	var connectedUsers []string
	if departmentID != "" {
		connectedUsers = s.eligibleUsers.GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID, skipAdmins)
	} else {
		connectedUsers = s.eligibleUsers.GetEligibleUsersForWorkspace(workspaceID, skipAdmins)
	}
	if len(connectedUsers) == 0 {
		if departmentID != "" {
			log.Printf("[InboxAssignment] no connected eligible users for workspace %s department %s, entry %s stays unassigned", workspaceID, departmentID, entryID)
		} else {
			log.Printf("[InboxAssignment] no connected eligible users for workspace %s, entry %s stays unassigned (visible to all)", workspaceID, entryID)
		}
		return ""
	}

	sort.Strings(connectedUsers)

	state, err := s.repo.GetRoundRobinState(workspaceID, businessPhoneID, departmentID)
	if err != nil {
		log.Printf("[InboxAssignment] error getting round-robin state: %v", err)
		return ""
	}

	nextIndex := 0
	if state != nil && state.LastAssignedUserID != "" {
		found := false

		for i, u := range connectedUsers {
			if u == state.LastAssignedUserID {
				nextIndex = (i + 1) % len(connectedUsers)
				found = true
				break
			}
		}
		if !found {

			nextIndex = sort.SearchStrings(connectedUsers, state.LastAssignedUserID) % len(connectedUsers)
		}
	}

	assignedUserID := connectedUsers[nextIndex]

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

	newState := &ia.RoundRobinState{
		WorkspaceID:        workspaceID,
		BusinessPhoneID:    businessPhoneID,
		DepartmentID:       departmentID,
		LastAssignedUserID: assignedUserID,
	}
	if state != nil {
		newState.ID = state.ID
	}
	if err := s.repo.SaveRoundRobinState(newState); err != nil {
		log.Printf("[InboxAssignment] error saving round-robin state: %v", err)

	}

	log.Printf("[InboxAssignment] assigned entry %s (%s) → user %s (index %d/%d, phone %s)",
		entryID, entryType, assignedUserID, nextIndex, len(connectedUsers), businessPhoneID)

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

// AssignManual is the single choke point for manual / open / bulk assignment.
// assignedBy is the actor who caused the assignment (user id or system).
func (s *AssignmentService) AssignManual(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger string) error {
	prev := ""
	if existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType); err == nil && existing != nil {
		if existing.AssignedUserID == toUserID {
			return nil
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
		return err
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
	return nil
}

// AssignOnOpen claims an unassigned entry for the user who opened it.
// Returns true if assignment was written.
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
	SIPTrunkID        string
	EventType         ce.EventType
	Channel           string
}

func (s *AssignmentService) recordHistoryAndEvent(in recordInput) {
	now := time.Now().UTC()
	actorKind := string(actor.KindHuman)
	if actor.IsAI(in.AssignedUserID) {
		actorKind = string(actor.KindAI)
	}

	// Prefer queue (production). Direct history is only for unit tests without Rabbit.
	if s.telemetry != nil {
		histID := ""
		// Stable id for idempotent redelivery of the same assignment action.
		// uuid per mutation is correct (each assign is a new interval).
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
			SIPTrunkID:        in.SIPTrunkID,
			DepartmentID:      in.DepartmentID,
			StartedAt:         now,
		})
	} else if s.history != nil {
		if err := s.history.CloseOpen(in.WorkspaceID, in.EntryID, in.EntryType, now); err != nil {
			log.Printf("[InboxAssignment] history CloseOpen: %v", err)
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
			SIPTrunkID:        in.SIPTrunkID,
			DepartmentID:      in.DepartmentID,
			StartedAt:         now,
		}
		if err := s.history.Append(h); err != nil {
			log.Printf("[InboxAssignment] history Append: %v", err)
		}
	}

	if s.events != nil {
		details := map[string]string{
			"to_user_id": in.AssignedUserID,
			"trigger":    in.Trigger,
		}
		if in.PreviousUserID != "" {
			details["from_user_id"] = in.PreviousUserID
		}
		b := ce.New(in.WorkspaceID, in.EntryID, in.EntryType, in.EventType).
			WithChannel(in.Channel).
			WithDetails(details)
		if in.AssignedByActorID == actor.SystemID || in.AssignedByActorID == "" {
			b = b.WithActorSystem()
		} else if actor.IsAI(in.AssignedByActorID) {
			b = b.WithActorAI(actor.ParseAI(in.AssignedByActorID))
		} else {
			b = b.WithActorHuman(in.AssignedByActorID)
		}
		s.events.Log(b.Build())
	}
}

func channelForEntryType(entryType string) string {
	switch entryType {
	case "voice":
		return "voice"
	case "support":
		return "support"
	default:
		return "whatsapp"
	}
}
