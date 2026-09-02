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
	repo              ia.Repository
	history           ia.HistoryRepository // optional; prefer telemetry pub for hot path
	telemetry         crm_telemetry.Publisher
	events            ce.Logger
	eligibleUsers     conversation.EligibleUserProvider
	workspaceResolver conversation.CampaignWorkspaceResolver
	workspaceConfig   WorkspaceConfigProvider
	// candidates builds the roulette ring. Always non-nil: the constructor
	// gives it the connected-user provider, which is enough to serve the
	// default (online) mode, and SetRoster/SetPresence upgrade the same
	// instance to also serve last_seen. One implementation, no second copy of
	// the online path to drift.
	candidates *CandidateResolver
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

// SetRoster enables the last_seen mode's membership-based pool. Without it a
// workspace configured for last_seen degrades to the online pool with a logged
// reason rather than stopping distribution.
func (s *AssignmentService) SetRoster(roster ia.RosterProvider) { s.candidates.SetRoster(roster) }

// SetPresence enables the last_seen mode's presence reader.
func (s *AssignmentService) SetPresence(seen ia.LastSeenReader) { s.candidates.SetPresence(seen) }

// Candidates exposes the resolver so the rescue sweep can rebuild the same ring
// this service assigns from — the alternative, a second resolver built from the
// same parts, is exactly the drift this seam exists to prevent.
func (s *AssignmentService) Candidates() *CandidateResolver { return s.candidates }

// workspaceConfigFor reads the workspace policy once per assignment. A missing
// provider or a failed read yields nil, and every consumer of the result treats
// nil as "the defaults", which are the historical behaviour.
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

	cfg := s.workspaceConfigFor(workspaceID)
	skipAdmins := false
	if cfg != nil {
		skipAdmins = cfg.SkipAdminAssignment
	}

	log.Printf("[InboxAssignment] workspace %s: skipAdmins=%v for entry %s (%s)", workspaceID, skipAdmins, entryID, entryType)

	departmentID, err := s.workspaceResolver.GetEntryDepartmentID(entryID, entryType)
	if err != nil {
		log.Printf("[InboxAssignment] cannot resolve department for entry %s (%s): %v", entryID, entryType, err)
		return ""
	}

	pool := s.candidates.Resolve(workspaceID, departmentID, skipAdmins, cfg)
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

// UnassignSystem drops ownership and records it.
//
// It exists for the one case where keeping an owner is worse than having none:
// the rescue sweep has walked the whole ring and nobody took the conversation.
// Unassigned means "visible to the whole department", so the conversation stops
// being one away agent's private backlog and someone can pick it up.
//
// reason is stamped on the timeline event so the customer-facing history says
// why ownership disappeared, rather than showing an unexplained gap.
func (s *AssignmentService) UnassignSystem(entryID, entryType, workspaceID, reason string) error {
	existing, err := s.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	prev := existing.AssignedUserID

	if err := s.repo.Unassign(workspaceID, entryID, entryType); err != nil {
		return err
	}

	dept := ""
	if s.workspaceResolver != nil {
		if d, err := s.workspaceResolver.GetEntryDepartmentID(entryID, entryType); err == nil {
			dept = d
		}
	}

	// An empty AssignedUserID is what tells the history writer to close the
	// open interval without opening a new one — see the consumer.
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
	return nil
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
			DepartmentID:      in.DepartmentID,
			StartedAt:         now,
		})
	} else if s.history != nil {
		if err := s.history.CloseOpen(in.WorkspaceID, in.EntryID, in.EntryType, now); err != nil {
			log.Printf("[InboxAssignment] history CloseOpen: %v", err)
		}
		// Unassignment closes the interval and opens nothing — an owner-less
		// open interval would read as "still assigned" in every report. Mirrors
		// the same guard in the telemetry consumer.
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

	if s.events != nil {
		details := map[string]string{"trigger": in.Trigger}
		if in.AssignedUserID != "" {
			details["to_user_id"] = in.AssignedUserID
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

// channelForEntryType named the channel an assignment event belongs to. It
// listed voice and support and defaulted everything else to "whatsapp", so an
// Instagram or Telegram assignment was filed under WhatsApp on the timeline.
// EventChannel keeps the same fallback for an unrecognised type.
func channelForEntryType(entryType string) string {
	return shared.EntryType(entryType).EventChannel()
}

// maxRoundRobinAttempts bounds the compare-and-swap retry. Three is generous:
// contention here is two webhooks landing in the same millisecond, and each
// retry re-reads a pointer that has just been written.
const maxRoundRobinAttempts = 3

// claimNextInRing picks the next owner and claims the round-robin pointer in
// the same breath.
//
// The pointer now advances BEFORE the assignment row is written, which is the
// deliberate half of this trade: if the assignment write then fails, one
// position of the rotation is skipped. Skipping a turn costs one agent one
// conversation's worth of fairness; the alternative — the unguarded
// read-modify-write this replaces — handed two simultaneous conversations to
// the same agent and skipped somebody entirely.
//
// When the retries are spent it assigns anyway against the last pointer it
// read. A slightly unfair assignment is better than a conversation nobody owns.
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
			// A failed pointer WRITE has always been non-fatal: the rotation
			// loses a step, the conversation still gets an owner. Only a failed
			// READ aborts, because without the pointer there is no pick to make.
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
