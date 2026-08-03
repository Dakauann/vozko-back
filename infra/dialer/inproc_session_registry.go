package dialer

import (
	"sort"
	"sync"
	"sync/atomic"

	dialer_domain "vozko/domain/dialer"
	"vozko/domain/workspace"
)

// TODO: fix this to be redis based

// InProcSessionRegistry holds, per (workspace, user), the SET of that member's live
// dialer sessions (contacts). A browser softphone and a registered branch coexist as
// separate contacts of one member, exactly like SIP forking to multiple contacts of
// one address of record: registering one does not evict the other, and deregistering
// removes only that one. FindByUser returns a single preferred contact (for callers
// that target one endpoint); FindSessionsByUser returns all contacts so a transfer
// can fork and ring them together.
type InProcSessionRegistry struct {
	mu       sync.RWMutex
	byWS     map[string]map[string]map[string]dialer_domain.DialerSession // ws -> uid -> sid -> session
	byID     map[string]dialer_domain.DialerSession
	seq      uint64
	seqByID  map[string]uint64
	listener atomic.Pointer[dialer_domain.PresenceListener]

	// ringResolver gates which of a member's endpoints are offered a call, per the
	// member's ring-channel selection. Set once at startup; a nil resolver rings
	// every channel (fail open). See RingChannelResolver.
	ringResolver RingChannelResolver
}

// RingChannelResolver reports a member's ring-channel policy so the registry can
// gate which endpoints ring on offers/transfers. A nil resolver (or a member the
// impl cannot resolve, which it treats as fail-open) rings every channel.
type RingChannelResolver interface {
	RingsChannel(workspaceID, userID, channel string) bool
}

// SetRingResolver wires the member ring-channel policy. Call once at startup before
// any session registers.
func (r *InProcSessionRegistry) SetRingResolver(res RingChannelResolver) {
	r.ringResolver = res
}

// channelOf reports the ring channel a session belongs to: a registered SIP
// extension (physical endpoint) is the branch channel; anything else (the web
// dialer) is the browser channel.
func channelOf(s dialer_domain.DialerSession) string {
	if isPhysicalEndpoint(s) {
		return string(workspace.RingChannelBranch)
	}
	return string(workspace.RingChannelBrowser)
}

// sessionRingEligible reports whether a session may currently be rung. Sessions opt
// in via RingEligible(); anything without it is always eligible.
func sessionRingEligible(s dialer_domain.DialerSession) bool {
	if e, ok := s.(dialer_domain.RingEligibleSession); ok {
		return e.RingEligible()
	}
	return true
}

// applyRingEligibility syncs a session's ring flag to the member's current policy.
// Called on Register (before the lock, so a policy lookup never holds it) so a new
// endpoint immediately reflects a member who has already disabled its channel.
func (r *InProcSessionRegistry) applyRingEligibility(s dialer_domain.DialerSession) {
	setter, ok := s.(interface{ SetRingEligible(bool) })
	if !ok {
		return
	}
	eligible := true
	if r.ringResolver != nil {
		eligible = r.ringResolver.RingsChannel(s.WorkspaceID(), s.UserID(), channelOf(s))
	}
	setter.SetRingEligible(eligible)
}

// ApplyRingChannels updates every live endpoint of a member to the given enabled
// channel set (called when the member changes their selection), so the change takes
// effect immediately without a reconnect. It broadcasts presence since eligibility
// is part of who can be offered a call.
func (r *InProcSessionRegistry) ApplyRingChannels(workspaceID, userID string, channels []string) {
	enabled := make(map[string]bool, len(channels))
	for _, c := range channels {
		enabled[c] = true
	}
	r.mu.RLock()
	sessions := make([]dialer_domain.DialerSession, 0)
	for _, s := range r.byWS[workspaceID][userID] {
		sessions = append(sessions, s)
	}
	r.mu.RUnlock()

	for _, s := range sessions {
		setter, ok := s.(interface{ SetRingEligible(bool) })
		if !ok {
			continue
		}
		setter.SetRingEligible(enabled[channelOf(s)])
	}
	r.notifyPresenceChanged(workspaceID)
}

func NewInProcSessionRegistry() *InProcSessionRegistry {
	return &InProcSessionRegistry{
		byWS:    make(map[string]map[string]map[string]dialer_domain.DialerSession),
		byID:    make(map[string]dialer_domain.DialerSession),
		seqByID: make(map[string]uint64),
	}
}

func (r *InProcSessionRegistry) Register(s dialer_domain.DialerSession) (func(), error) {
	if s == nil {
		return func() {}, dialer_domain.ErrSessionNotFound
	}
	ws := s.WorkspaceID()
	uid := s.UserID()
	sid := s.ID()
	if ws == "" {
		return func() {}, dialer_domain.ErrWorkspaceRequired
	}
	if uid == "" {
		return func() {}, dialer_domain.ErrOwnerRequired
	}

	// Sync the new endpoint to the member's ring-channel policy before it enters the
	// pool, so a member who has already disabled this channel is honored at once.
	r.applyRingEligibility(s)

	r.mu.Lock()
	users, ok := r.byWS[ws]
	if !ok {
		users = make(map[string]map[string]dialer_domain.DialerSession)
		r.byWS[ws] = users
	}
	sessions, ok := users[uid]
	if !ok {
		sessions = make(map[string]dialer_domain.DialerSession)
		users[uid] = sessions
	}
	r.seq++
	sessions[sid] = s
	r.byID[sid] = s
	r.seqByID[sid] = r.seq
	r.mu.Unlock()

	r.notifyPresenceChanged(ws)

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if users, ok := r.byWS[ws]; ok {
				if sessions, ok := users[uid]; ok {
					// Remove only THIS contact; a member's other endpoints survive.
					delete(sessions, sid)
					if len(sessions) == 0 {
						delete(users, uid)
					}
				}
				if len(users) == 0 {
					delete(r.byWS, ws)
				}
			}
			delete(r.byID, sid)
			delete(r.seqByID, sid)
			r.mu.Unlock()
			r.notifyPresenceChanged(ws)
		})
	}, nil
}

func (r *InProcSessionRegistry) FindByUser(workspaceID, userID string) (dialer_domain.DialerSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessions := r.byWS[workspaceID][userID]
	if len(sessions) == 0 {
		return nil, false
	}
	return r.preferredLocked(sessions), true
}

func (r *InProcSessionRegistry) FindSessionsByUser(workspaceID, userID string) []dialer_domain.DialerSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessions := r.byWS[workspaceID][userID]
	if len(sessions) == 0 {
		return nil
	}
	out := make([]dialer_domain.DialerSession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s)
	}
	r.sortByRegistrationOrder(out)
	return out
}

// FindRingableSessionsByUser returns a member's contacts that the member allows to
// ring (their ring-channel policy), for the transfer fork. It excludes endpoints on
// a disabled channel so an idle-but-disabled browser is never rung.
func (r *InProcSessionRegistry) FindRingableSessionsByUser(workspaceID, userID string) []dialer_domain.DialerSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessions := r.byWS[workspaceID][userID]
	if len(sessions) == 0 {
		return nil
	}
	out := make([]dialer_domain.DialerSession, 0, len(sessions))
	for _, s := range sessions {
		if sessionRingEligible(s) {
			out = append(out, s)
		}
	}
	r.sortByRegistrationOrder(out)
	return out
}

// ringEligibleSessions keeps only the contacts the member allows to ring. Cheap
// (an atomic flag per session), so it is safe to call under the registry read lock.
func ringEligibleSessions(sessions map[string]dialer_domain.DialerSession) map[string]dialer_domain.DialerSession {
	out := make(map[string]dialer_domain.DialerSession, len(sessions))
	for sid, s := range sessions {
		if sessionRingEligible(s) {
			out[sid] = s
		}
	}
	return out
}

// preferredLocked returns the single contact a one-endpoint caller should target: a
// physical endpoint (a registered branch) wins over a transient browser session, and
// among equals the most recently attached one wins. Caller holds the lock.
func (r *InProcSessionRegistry) preferredLocked(sessions map[string]dialer_domain.DialerSession) dialer_domain.DialerSession {
	var best dialer_domain.DialerSession
	var bestSeq uint64
	bestPhysical := false
	for sid, s := range sessions {
		physical := isPhysicalEndpoint(s)
		seq := r.seqByID[sid]
		switch {
		case best == nil:
			best, bestSeq, bestPhysical = s, seq, physical
		case physical != bestPhysical:
			if physical {
				best, bestSeq, bestPhysical = s, seq, physical
			}
		case seq > bestSeq:
			best, bestSeq = s, seq
		}
	}
	return best
}

// isPhysicalEndpoint reports whether a session is a registered device (a branch),
// which should be preferred over a transient browser session. Optional interface so
// only the branch session opts in.
func isPhysicalEndpoint(s dialer_domain.DialerSession) bool {
	if p, ok := s.(interface{ IsPhysicalEndpoint() bool }); ok {
		return p.IsPhysicalEndpoint()
	}
	return false
}

// representativeLocked collapses a member's contacts to one session for the
// per-user presence/target lists, under a "one human = one call" rule: a member is
// available only when EVERY contact is free. If any contact has an active call (or
// live ring reservation), the member is busy, an idle browser must not mask an
// in-progress branch call, so onlyFree returns nil (not offerable) and otherwise the
// preferred BUSY contact is returned, so downstream HasActiveCall() checks agree with
// the presence roster.
func (r *InProcSessionRegistry) representativeLocked(sessions map[string]dialer_domain.DialerSession, onlyFree bool) dialer_domain.DialerSession {
	busy := make(map[string]dialer_domain.DialerSession, len(sessions))
	free := make(map[string]dialer_domain.DialerSession, len(sessions))
	for sid, s := range sessions {
		if s.HasActiveCall() {
			busy[sid] = s
		} else {
			free[sid] = s
		}
	}
	if len(busy) > 0 {
		if onlyFree {
			return nil
		}
		return r.preferredLocked(busy)
	}
	return r.preferredLocked(free)
}

// FindByID resolves a live session by its (globally unique) session ID. The byID
// index makes this O(1); used to cancel every contact of a multi-user ringall wave.
func (r *InProcSessionRegistry) FindByID(sessionID string) (dialer_domain.DialerSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[sessionID]
	return s, ok
}

func (r *InProcSessionRegistry) ListAvailable(workspaceID string) []dialer_domain.DialerSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	out := make([]dialer_domain.DialerSession, 0, len(users))
	for _, sessions := range users {
		// Offer only endpoints the member allows to ring (their ring-channel policy),
		// then collapse to the representative under the one-human-one-call rule.
		if rep := r.representativeLocked(ringEligibleSessions(sessions), true); rep != nil {
			out = append(out, rep)
		}
	}
	r.sortByRegistrationOrder(out)
	return out
}

func (r *InProcSessionRegistry) ListAll(workspaceID string) []dialer_domain.DialerSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	out := make([]dialer_domain.DialerSession, 0, len(users))
	for _, sessions := range users {
		if rep := r.representativeLocked(sessions, false); rep != nil {
			out = append(out, rep)
		}
	}
	r.sortByRegistrationOrder(out)
	return out
}

// ListPresence returns one row per online member, folding their contact set into a
// single status + endpoint-kind view. Busy is true only when NO contact is free (every
// endpoint is on a call or ringing), matching representativeLocked's availability rule.
func (r *InProcSessionRegistry) ListPresence(workspaceID string) []dialer_domain.MemberPresence {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	out := make([]dialer_domain.MemberPresence, 0, len(users))
	for uid, sessions := range users {
		if len(sessions) == 0 {
			continue
		}
		mp := dialer_domain.MemberPresence{UserID: uid}
		// A member is one human reachable on multiple endpoints (browser + branch).
		// If they are on a call on ANY endpoint they are busy: an idle browser must
		// not mask an in-progress branch call (a person can only handle one call).
		// This is the presence-display semantic; routing/offer availability is a
		// separate concern handled by representativeLocked's free-endpoint pick.
		anyOnCall := false
		anyRinging := false
		for _, s := range sessions {
			if isPhysicalEndpoint(s) {
				mp.HasBranch = true
			} else {
				mp.HasBrowser = true
			}
			if s.ActiveCallID() != "" {
				anyOnCall = true
			} else if s.HasActiveCall() {
				anyRinging = true
			}
		}
		mp.OnCall = anyOnCall
		mp.Ringing = anyRinging && !anyOnCall
		mp.Busy = anyOnCall || anyRinging
		out = append(out, mp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// ListBrowserSessions returns every non-physical (browser/WebSocket) session, i.e. the
// ones that can receive a pushed presence snapshot. Notifying ONE representative per
// member would skip a branch-owning member's browser (the branch wins as representative)
// and any second browser tab, so presence must fan out to every browser contact.
func (r *InProcSessionRegistry) ListBrowserSessions(workspaceID string) []dialer_domain.DialerSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	var out []dialer_domain.DialerSession
	for _, sessions := range users {
		for _, s := range sessions {
			if s != nil && !isPhysicalEndpoint(s) {
				out = append(out, s)
			}
		}
	}
	r.sortByRegistrationOrder(out)
	return out
}

func (r *InProcSessionRegistry) sortByRegistrationOrder(sessions []dialer_domain.DialerSession) {
	sort.SliceStable(sessions, func(left, right int) bool {
		return r.seqByID[sessions[left].ID()] < r.seqByID[sessions[right].ID()]
	})
}

func (r *InProcSessionRegistry) SetPresenceListener(listener dialer_domain.PresenceListener) {
	if listener == nil {
		r.listener.Store(nil)
		return
	}
	r.listener.Store(&listener)
}

func (r *InProcSessionRegistry) NotifyPresenceChanged(workspaceID string) {
	if workspaceID == "" {
		return
	}
	r.notifyPresenceChanged(workspaceID)
}

func (r *InProcSessionRegistry) notifyPresenceChanged(workspaceID string) {
	if lp := r.listener.Load(); lp != nil && *lp != nil {
		(*lp).OnPresenceChanged(workspaceID)
	}
}

var _ dialer_domain.DialerSessionRegistry = (*InProcSessionRegistry)(nil)
