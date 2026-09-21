package inbox_assignment_usecase

import (
	"log"
	"time"

	conversation "vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

const (
	PoolReasonOnline              = "online"
	PoolReasonLastSeen            = "last_seen"
	PoolReasonLastSeenEmpty       = "last_seen_empty_fallback"
	PoolReasonLastSeenError       = "last_seen_error_fallback"
	PoolReasonLastSeenUnavailable = "last_seen_unwired_fallback"
)

type Pool struct {
	Ring   []string
	Resume ia.ResumePolicy
	Mode   string
	Reason string
}

type CandidateResolver struct {
	online conversation.EligibleUserProvider
	roster ia.RosterProvider
	seen   ia.LastSeenReader
	now    func() time.Time
}

func NewCandidateResolver(online conversation.EligibleUserProvider) *CandidateResolver {
	return &CandidateResolver{online: online, now: time.Now}
}

func (r *CandidateResolver) SetRoster(roster ia.RosterProvider) { r.roster = roster }

func (r *CandidateResolver) SetPresence(seen ia.LastSeenReader) { r.seen = seen }

func (r *CandidateResolver) SetClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
}

func (r *CandidateResolver) Resolve(workspaceID, departmentID string, skipAdmins bool, cfg *wsc.WorkspaceConfig) Pool {
	if r == nil {
		return Pool{Mode: wsc.RouletteModeOnline, Reason: PoolReasonOnline}
	}

	mode := cfg.EffectiveRouletteMode()
	if mode != wsc.RouletteModeLastSeen {
		return r.onlinePool(workspaceID, departmentID, skipAdmins, PoolReasonOnline)
	}
	if r.roster == nil || r.seen == nil {
		log.Printf("[InboxAssignment] workspace %s asks for last_seen mode but the roster/presence readers are not wired; using the online pool", workspaceID)
		return r.onlinePool(workspaceID, departmentID, skipAdmins, PoolReasonLastSeenUnavailable)
	}

	members, err := r.roster.ListRouletteMembers(workspaceID, departmentID, skipAdmins)
	if err != nil {
		log.Printf("[InboxAssignment] roster error for workspace %s department %q: %v; falling back to the online pool", workspaceID, departmentID, err)
		return r.onlinePool(workspaceID, departmentID, skipAdmins, PoolReasonLastSeenError)
	}
	if len(members) == 0 {
		log.Printf("[InboxAssignment] last_seen pool empty for workspace %s department %q (roster=0); falling back to the online pool", workspaceID, departmentID)
		return r.onlinePool(workspaceID, departmentID, skipAdmins, PoolReasonLastSeenEmpty)
	}

	lastSeen, err := r.seen.LastSeen(workspaceID, members)
	if err != nil {
		log.Printf("[InboxAssignment] presence read error for workspace %s: %v; falling back to the online pool", workspaceID, err)
		return r.onlinePool(workspaceID, departmentID, skipAdmins, PoolReasonLastSeenError)
	}

	onlineNow := make(map[string]bool)
	for _, uid := range r.onlineUsers(workspaceID, departmentID, skipAdmins) {
		onlineNow[uid] = true
	}

	now := r.now().UTC()
	window := cfg.EffectiveRouletteLastSeenWindow()

	cands := make([]ia.Candidate, 0, len(members))
	for _, uid := range members {
		c := ia.Candidate{UserID: uid, Online: onlineNow[uid]}
		if c.Online {
			c.LastSeen = now
		} else if seenAt, ok := lastSeen[uid]; ok {
			if seenAt.After(now) {
				seenAt = now
			}
			c.LastSeen = seenAt
		}
		cands = append(cands, c)
	}

	ring := ia.BuildLastSeenRing(cands, now, window)
	if len(ring) == 0 {
		log.Printf("[InboxAssignment] last_seen pool empty for workspace %s department %q (roster=%d, window=%s, nobody seen inside it); falling back to the online pool",
			workspaceID, departmentID, len(members), window)
		return r.onlinePool(workspaceID, departmentID, skipAdmins, PoolReasonLastSeenEmpty)
	}

	return Pool{Ring: ring, Resume: ia.ResumeAtHead, Mode: wsc.RouletteModeLastSeen, Reason: PoolReasonLastSeen}
}

func (r *CandidateResolver) onlinePool(workspaceID, departmentID string, skipAdmins bool, reason string) Pool {
	return Pool{
		Ring:   ia.BuildOnlineRing(r.onlineUsers(workspaceID, departmentID, skipAdmins)),
		Resume: ia.ResumeAtInsertionPoint,
		Mode:   wsc.RouletteModeOnline,
		Reason: reason,
	}
}

func (r *CandidateResolver) onlineUsers(workspaceID, departmentID string, skipAdmins bool) []string {
	if r.online == nil {
		return nil
	}
	if departmentID != "" {
		return r.online.GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID, skipAdmins)
	}
	return r.online.GetEligibleUsersForWorkspace(workspaceID, skipAdmins)
}
