package ws

import (
	"log"
	"sort"
	"strings"
	"time"
)

const viewerKeyTTL = 45 * time.Second

type EntryViewer struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

type ViewersEvent struct {
	EntryID   string        `json:"entry_id"`
	EntryType string        `json:"entry_type"`
	Viewers   []EntryViewer `json:"viewers"`
}

func viewerKey(sub entrySubscription) string {
	return "hub:viewers:" + sub.entryType + ":" + sub.entryID
}

func viewerMember(userID, replicaID string) string {
	return userID + "|" + replicaID
}

func viewerUserIDs(members []string, ownReplica string, localUsers []string, replicaAlive func(string) bool) (users, stale []string) {
	seen := make(map[string]bool, len(members)+len(localUsers))
	for _, userID := range localUsers {
		seen[userID] = true
	}
	for _, member := range members {
		userID, replica, ok := strings.Cut(member, "|")
		if !ok || userID == "" || replica == ownReplica {
			continue
		}
		if !replicaAlive(replica) {
			stale = append(stale, member)
			continue
		}
		seen[userID] = true
	}
	users = make([]string, 0, len(seen))
	for userID := range seen {
		users = append(users, userID)
	}
	sort.Strings(users)
	return users, stale
}

func (h *ConversationHub) scheduleViewersAnnouncement(sub entrySubscription) {
	h.viewerTimersMu.Lock()
	defer h.viewerTimersMu.Unlock()
	if timer, pending := h.viewerTimers[sub]; pending {
		timer.Stop()
	}
	h.viewerTimers[sub] = time.AfterFunc(h.viewersDelay, func() {
		h.viewerTimersMu.Lock()
		delete(h.viewerTimers, sub)
		h.viewerTimersMu.Unlock()
		h.announceViewers(sub)
	})
}

func (h *ConversationHub) announceViewers(sub entrySubscription) {
	userIDs := h.syncEntryViewers(sub)
	if len(userIDs) == 0 {
		return
	}
	h.broadcast <- &broadcastMessage{
		entryID:   sub.entryID,
		entryType: sub.entryType,
		event: &WSOutgoingMessage{
			Type:    WSEventViewers,
			Payload: ViewersEvent{EntryID: sub.entryID, EntryType: sub.entryType, Viewers: h.namedViewers(userIDs)},
		},
	}
}

func (h *ConversationHub) syncEntryViewers(sub entrySubscription) []string {
	h.viewersMu.Lock()
	defer h.viewersMu.Unlock()

	local := h.localViewerUserIDs(sub)
	h.publishLocalViewers(sub, local)

	var members []string
	if h.sharedState != nil {
		var err error
		if members, err = h.sharedState.SMembers(viewerKey(sub)); err != nil {
			log.Printf("[ConversationHub] reading viewers of %s:%s failed: %v", sub.entryType, sub.entryID, err)
		}
	}
	users, stale := viewerUserIDs(members, h.replicaID, local, h.replicaAliveMemo())
	if len(stale) > 0 {
		_ = h.sharedState.SRem(viewerKey(sub), stale...)
	}
	return users
}

func (h *ConversationHub) localViewerUserIDs(sub entrySubscription) []string {
	h.subMu.RLock()
	connIDs := make([]string, 0, len(h.entrySubscribers[sub]))
	for connID := range h.entrySubscribers[sub] {
		connIDs = append(connIDs, connID)
	}
	h.subMu.RUnlock()

	h.connMu.RLock()
	defer h.connMu.RUnlock()
	seen := make(map[string]bool, len(connIDs))
	users := make([]string, 0, len(connIDs))
	for _, connID := range connIDs {
		conn, ok := h.connections[connID]
		if !ok || seen[conn.UserID] {
			continue
		}
		seen[conn.UserID] = true
		users = append(users, conn.UserID)
	}
	return users
}

func (h *ConversationHub) publishLocalViewers(sub entrySubscription, local []string) {
	current := make(map[string]bool, len(local))
	for _, userID := range local {
		current[userID] = true
	}
	previous := h.publishedViewers[sub]
	if len(current) == 0 {
		delete(h.publishedViewers, sub)
	} else {
		h.publishedViewers[sub] = current
	}
	if h.sharedState == nil {
		return
	}

	key := viewerKey(sub)
	var gone []string
	for userID := range previous {
		if !current[userID] {
			gone = append(gone, viewerMember(userID, h.replicaID))
		}
	}
	if len(gone) > 0 {
		_ = h.sharedState.SRem(key, gone...)
	}
	if len(local) == 0 {
		return
	}
	members := make([]string, 0, len(local))
	for _, userID := range local {
		members = append(members, viewerMember(userID, h.replicaID))
	}
	if err := h.sharedState.SAdd(key, members...); err != nil {
		log.Printf("[ConversationHub] publishing viewers of %s:%s failed: %v", sub.entryType, sub.entryID, err)
		return
	}
	_, _ = h.sharedState.Expire(key, viewerKeyTTL)
}

func (h *ConversationHub) replicaAliveMemo() func(string) bool {
	known := map[string]bool{}
	return func(replica string) bool {
		if alive, ok := known[replica]; ok {
			return alive
		}
		alive, err := h.sharedState.Exists("replica:heartbeat:" + replica)
		alive = alive || err != nil
		known[replica] = alive
		return alive
	}
}

func (h *ConversationHub) namedViewers(userIDs []string) []EntryViewer {
	names := make(map[string]string, len(userIDs))
	if h.userRepo != nil {
		if users, err := h.userRepo.FindByIDs(userIDs); err == nil {
			for _, u := range users {
				if u != nil {
					names[u.ID] = u.Username
				}
			}
		} else {
			log.Printf("[ConversationHub] resolving viewer names failed: %v", err)
		}
	}
	viewers := make([]EntryViewer, 0, len(userIDs))
	for _, userID := range userIDs {
		viewers = append(viewers, EntryViewer{UserID: userID, Name: names[userID]})
	}
	return viewers
}

func (h *ConversationHub) refreshViewerKeys() {
	if h.sharedState == nil {
		return
	}
	h.viewersMu.Lock()
	keys := make([]string, 0, len(h.publishedViewers))
	for sub := range h.publishedViewers {
		keys = append(keys, viewerKey(sub))
	}
	h.viewersMu.Unlock()
	for _, key := range keys {
		_, _ = h.sharedState.Expire(key, viewerKeyTTL)
	}
}
