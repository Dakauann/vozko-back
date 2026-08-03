package call_roulette_usecase

import (
	"log"
	"sort"
	"time"

	"vozko/domain/actor"
	"vozko/domain/call_roulette"
	"vozko/domain/crm_telemetry"
	ia "vozko/domain/inbox_assignment"
)

type WorkspaceConfigProvider interface {
	GetByWorkspaceID(workspaceID string) (bool, error)
}

type CampaignResolver interface {
	GetEntryWorkspaceID(entryID string) (string, error)
	GetEntryDepartmentID(entryID string) (string, error)
	GetEntrySIPTrunkID(entryID string) (string, error)
}

type AssignmentService struct {
	repo             call_roulette.Repository
	eligibleUsers    call_roulette.EligibleUserProvider
	campaignResolver CampaignResolver
	workspaceConfig  WorkspaceConfigProvider
	telemetry        crm_telemetry.Publisher
}

func NewAssignmentService(
	repo call_roulette.Repository,
	eligibleUsers call_roulette.EligibleUserProvider,
	campaignResolver CampaignResolver,
	workspaceConfig WorkspaceConfigProvider,
) *AssignmentService {
	return &AssignmentService{
		repo:             repo,
		eligibleUsers:    eligibleUsers,
		campaignResolver: campaignResolver,
		workspaceConfig:  workspaceConfig,
	}
}

// SetTelemetry enables async assignment_history for voice roulette (optional).
func (s *AssignmentService) SetTelemetry(p crm_telemetry.Publisher) {
	if s != nil {
		s.telemetry = p
	}
}

func (s *AssignmentService) EnsureAssignment(entryID, sipTrunkID string) string {
	workspaceID, err := s.campaignResolver.GetEntryWorkspaceID(entryID)
	if err != nil || workspaceID == "" {
		log.Printf("[CallRoulette] cannot resolve workspace for entry %s: %v", entryID, err)
		return ""
	}

	existing, err := s.repo.FindByEntry(workspaceID, entryID, call_roulette.EntryTypeVoice)
	if err != nil {
		log.Printf("[CallRoulette] error checking existing assignment for %s: %v", entryID, err)
		return ""
	}
	if existing != nil {
		return existing.AssignedUserID
	}

	skipAdmins := false
	if s.workspaceConfig != nil {
		if val, err := s.workspaceConfig.GetByWorkspaceID(workspaceID); err == nil {
			skipAdmins = val
		}
	}

	departmentID, err := s.campaignResolver.GetEntryDepartmentID(entryID)
	if err != nil {
		log.Printf("[CallRoulette] cannot resolve department for entry %s: %v", entryID, err)
		return ""
	}

	var connectedUsers []string
	if departmentID != "" {
		connectedUsers = s.eligibleUsers.GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID, skipAdmins)
	} else {
		connectedUsers = s.eligibleUsers.GetEligibleUsersForWorkspace(workspaceID, skipAdmins)
	}
	if len(connectedUsers) == 0 {
		log.Printf("[CallRoulette] no connected eligible users for workspace %s, entry %s stays unassigned", workspaceID, entryID)
		return ""
	}

	sort.Strings(connectedUsers)

	state, err := s.repo.GetState(workspaceID, sipTrunkID, departmentID)
	if err != nil {
		log.Printf("[CallRoulette] error getting round-robin state: %v", err)
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

	if err := s.repo.Assign(entryID, call_roulette.EntryTypeVoice, workspaceID, sipTrunkID, departmentID, assignedUserID); err != nil {
		log.Printf("[CallRoulette] error creating assignment for entry %s → user %s: %v", entryID, assignedUserID, err)
		return ""
	}

	newState := &call_roulette.RouletteState{
		WorkspaceID:        workspaceID,
		SIPTrunkID:         sipTrunkID,
		DepartmentID:       departmentID,
		LastAssignedUserID: assignedUserID,
	}
	if state != nil {
		newState.ID = state.ID
	}
	if err := s.repo.SaveState(newState); err != nil {
		log.Printf("[CallRoulette] error saving round-robin state: %v", err)
	}

	log.Printf("[CallRoulette] assigned entry %s → user %s (index %d/%d, trunk %s)", entryID, assignedUserID, nextIndex, len(connectedUsers), sipTrunkID)

	if s.telemetry != nil {
		_ = s.telemetry.Publish(crm_telemetry.KindAssignmentHistory, crm_telemetry.AssignmentHistoryPayload{
			WorkspaceID:       workspaceID,
			EntryID:           entryID,
			EntryType:         string(call_roulette.EntryTypeVoice),
			ActorKind:         string(actor.KindHuman),
			AssignedActorID:   assignedUserID,
			Trigger:           ia.TriggerCallRoulette,
			AssignedByActorID: actor.SystemID,
			SIPTrunkID:        sipTrunkID,
			DepartmentID:      departmentID,
			StartedAt:         time.Now().UTC(),
		})
	}

	return assignedUserID
}

func (s *AssignmentService) GetAssignedUserID(workspaceID, entryID string) string {
	a, err := s.repo.FindByEntry(workspaceID, entryID, call_roulette.EntryTypeVoice)
	if err != nil || a == nil {
		return ""
	}
	return a.AssignedUserID
}

func (s *AssignmentService) Unassign(workspaceID, entryID string) {
	if err := s.repo.Unassign(workspaceID, entryID, call_roulette.EntryTypeVoice); err != nil {
		log.Printf("[CallRoulette] error unassigning entry %s: %v", entryID, err)
	}
}

func (s *AssignmentService) ListByUser(workspaceID, userID string) []*call_roulette.CallAssignment {
	result, err := s.repo.ListByUser(workspaceID, userID)
	if err != nil {
		log.Printf("[CallRoulette] error listing assignments for user %s: %v", userID, err)
		return nil
	}
	return result
}
