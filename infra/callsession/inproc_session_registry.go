package callsession

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"

	callsession_domain "vozko/domain/callsession"
)

// ErrNilSession: Register was handed no session at all.
var ErrNilSession = errors.New("call session: nil session")

// TODO: fix this to be redis based

// InProcSessionRegistry holds, per (workspace, user), the SET of that member's live
// call sessions (contacts). A member may hold more than one (a second browser tab):
// registering one does not evict the other, and deregistering removes only that one.
// FindByUser returns a single preferred contact (for callers that target one
// endpoint); FindSessionsByUser returns all of them.
type InProcSessionRegistry struct {
	mu       sync.RWMutex
	byWS     map[string]map[string]map[string]callsession_domain.CallSession // ws -> uid -> sid -> session
	byID     map[string]callsession_domain.CallSession
	seq      uint64
	seqByID  map[string]uint64
	listener atomic.Pointer[callsession_domain.PresenceListener]
}

func NewInProcSessionRegistry() *InProcSessionRegistry {
	return &InProcSessionRegistry{
		byWS:    make(map[string]map[string]map[string]callsession_domain.CallSession),
		byID:    make(map[string]callsession_domain.CallSession),
		seqByID: make(map[string]uint64),
	}
}

func (r *InProcSessionRegistry) Register(s callsession_domain.CallSession) (func(), error) {
	if s == nil {
		return func() {}, ErrNilSession
	}
	ws := s.WorkspaceID()
	uid := s.UserID()
	sid := s.ID()
	if ws == "" {
		return func() {}, callsession_domain.ErrWorkspaceRequired
	}
	if uid == "" {
		return func() {}, callsession_domain.ErrOwnerRequired
	}

	r.mu.Lock()
	users, ok := r.byWS[ws]
	if !ok {
		users = make(map[string]map[string]callsession_domain.CallSession)
		r.byWS[ws] = users
	}
	sessions, ok := users[uid]
	if !ok {
		sessions = make(map[string]callsession_domain.CallSession)
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

func (r *InProcSessionRegistry) FindByUser(workspaceID, userID string) (callsession_domain.CallSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessions := r.byWS[workspaceID][userID]
	if len(sessions) == 0 {
		return nil, false
	}
	return r.preferredLocked(sessions), true
}

func (r *InProcSessionRegistry) FindSessionsByUser(workspaceID, userID string) []callsession_domain.CallSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessions := r.byWS[workspaceID][userID]
	if len(sessions) == 0 {
		return nil
	}
	out := make([]callsession_domain.CallSession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s)
	}
	r.sortByRegistrationOrder(out)
	return out
}

// preferredLocked returns the single contact a one-endpoint caller should target:
// the most recently attached session wins. Caller holds the lock.
func (r *InProcSessionRegistry) preferredLocked(sessions map[string]callsession_domain.CallSession) callsession_domain.CallSession {
	var best callsession_domain.CallSession
	var bestSeq uint64
	for sid, s := range sessions {
		seq := r.seqByID[sid]
		if best == nil || seq > bestSeq {
			best, bestSeq = s, seq
		}
	}
	return best
}

// representativeLocked collapses a member's contacts to one session for the
// per-user presence/target lists, under a "one human = one call" rule: a member is
// available only when EVERY contact is free. If any contact has an active call (or
// live ring reservation), the member is busy, an idle browser must not mask an
// in-progress branch call, so onlyFree returns nil (not offerable) and otherwise the
// preferred BUSY contact is returned, so downstream HasActiveCall() checks agree with
// the presence roster.
func (r *InProcSessionRegistry) representativeLocked(sessions map[string]callsession_domain.CallSession, onlyFree bool) callsession_domain.CallSession {
	busy := make(map[string]callsession_domain.CallSession, len(sessions))
	free := make(map[string]callsession_domain.CallSession, len(sessions))
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
func (r *InProcSessionRegistry) FindByID(sessionID string) (callsession_domain.CallSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[sessionID]
	return s, ok
}

func (r *InProcSessionRegistry) ListAvailable(workspaceID string) []callsession_domain.CallSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	out := make([]callsession_domain.CallSession, 0, len(users))
	for _, sessions := range users {
		// Collapse to the representative under the one-human-one-call rule.
		if rep := r.representativeLocked(sessions, true); rep != nil {
			out = append(out, rep)
		}
	}
	r.sortByRegistrationOrder(out)
	return out
}

func (r *InProcSessionRegistry) ListAll(workspaceID string) []callsession_domain.CallSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	out := make([]callsession_domain.CallSession, 0, len(users))
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
func (r *InProcSessionRegistry) ListPresence(workspaceID string) []callsession_domain.MemberPresence {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	out := make([]callsession_domain.MemberPresence, 0, len(users))
	for uid, sessions := range users {
		if len(sessions) == 0 {
			continue
		}
		mp := callsession_domain.MemberPresence{UserID: uid}
		// A member is one human who may hold more than one browser session. If they
		// are on a call on ANY of them they are busy: an idle second tab must not
		// mask an in-progress call (a person can only handle one call).
		anyOnCall := false
		anyRinging := false
		for _, s := range sessions {
			mp.HasBrowser = true
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

// ListBrowserSessions returns every browser (WebSocket) session, i.e. the ones that
// can receive a pushed presence snapshot. Notifying ONE representative per member
// would skip a member's second browser tab, so presence fans out to every contact.
func (r *InProcSessionRegistry) ListBrowserSessions(workspaceID string) []callsession_domain.CallSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users, ok := r.byWS[workspaceID]
	if !ok {
		return nil
	}
	var out []callsession_domain.CallSession
	for _, sessions := range users {
		for _, s := range sessions {
			if s != nil {
				out = append(out, s)
			}
		}
	}
	r.sortByRegistrationOrder(out)
	return out
}

func (r *InProcSessionRegistry) sortByRegistrationOrder(sessions []callsession_domain.CallSession) {
	sort.SliceStable(sessions, func(left, right int) bool {
		return r.seqByID[sessions[left].ID()] < r.seqByID[sessions[right].ID()]
	})
}

func (r *InProcSessionRegistry) SetPresenceListener(listener callsession_domain.PresenceListener) {
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

var _ callsession_domain.CallSessionRegistry = (*InProcSessionRegistry)(nil)
