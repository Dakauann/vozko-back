package inbox_assignment_usecase

import (
	"log"
	"time"

	conversation "vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

// Reasons a pool ended up the way it did. They are logged with every
// assignment so "why did this conversation go to João?" is answerable from
// logs alone, and so the two fallbacks below are alertable rather than silent.
const (
	PoolReasonOnline              = "online"
	PoolReasonLastSeen            = "last_seen"
	PoolReasonLastSeenEmpty       = "last_seen_empty_fallback"
	PoolReasonLastSeenError       = "last_seen_error_fallback"
	PoolReasonLastSeenUnavailable = "last_seen_unwired_fallback"
)

// Pool is an ordered ring plus the rule for resuming when the round-robin
// pointer names somebody who is no longer in it.
type Pool struct {
	Ring   []string
	Resume ia.ResumePolicy
	// Mode actually used, which may differ from the configured one when a
	// fallback fired.
	Mode   string
	Reason string
}

// CandidateResolver builds the roulette ring.
//
// It is the single seam between the two modes. EnsureAssignment asks for a
// pool and does not know which strategy produced it, which is what keeps the
// online path — every existing workspace — on exactly one implementation
// instead of a copy that can drift.
type CandidateResolver struct {
	online conversation.EligibleUserProvider
	roster ia.RosterProvider
	seen   ia.LastSeenReader
	now    func() time.Time
}

func NewCandidateResolver(online conversation.EligibleUserProvider) *CandidateResolver {
	return &CandidateResolver{online: online, now: time.Now}
}

// SetRoster wires the membership-based pool. Until it is set (and SetPresence
// with it) the resolver can only serve the online mode, and a workspace
// configured for last_seen degrades to online with a logged reason rather than
// stopping distribution.
func (r *CandidateResolver) SetRoster(roster ia.RosterProvider) { r.roster = roster }

// SetPresence wires the last-seen reader.
func (r *CandidateResolver) SetPresence(seen ia.LastSeenReader) { r.seen = seen }

// SetClock is for tests. Production uses time.Now.
func (r *CandidateResolver) SetClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
}

// Resolve returns the ring for this workspace/department under the given
// config. It never returns an error: a roulette that refuses to pick because a
// read failed is an outage, so every failure path degrades to the online pool,
// which is the behaviour that predates this feature and therefore cannot be a
// regression.
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

	// The live connected set is read through the same provider the online mode
	// uses, so "online" means one thing in both modes and a connected agent
	// cannot be eligible in one and not the other.
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
			// Connected right now beats whatever the presence table says: the
			// telemetry queue can lag, and an open interval left by a crashed
			// replica is deliberately read at its started_at (see the
			// agent_presence repository), so this overlay is what keeps a
			// genuinely-online agent at the head of the ring.
			c.LastSeen = now
		} else if seenAt, ok := lastSeen[uid]; ok {
			// Clamp a skewed clock rather than trusting a timestamp from the
			// future, which would pin one agent to the head forever.
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
