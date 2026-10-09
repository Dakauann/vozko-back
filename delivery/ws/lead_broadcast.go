package ws

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"vozko/domain/lead"
	"vozko/domain/workspace"
)

const (
	WSEventLeadUpdate       WSEventType = "conversation:lead_update"
	leadUpdateBroadcastType             = "lead_update"
	leadEntriesTimeout                  = 5 * time.Second
)

type LeadUpdatePayload struct {
	LeadID  string   `json:"leadId"`
	Version int64    `json:"version"`
	Fields  []string `json:"fields"`
}

type leadUpdateBroadcast struct {
	Event   json.RawMessage `json:"e"`
	Entries []leadEntryRef  `json:"r,omitempty"`
}

type leadEntryRef struct {
	ID   string `json:"i"`
	Type string `json:"t"`
}

type LeadChangeNotifier struct {
	hub     *ConversationHub
	entries lead.EntryDirectory
}

func NewLeadChangeNotifier(hub *ConversationHub, entries lead.EntryDirectory) *LeadChangeNotifier {
	return &LeadChangeNotifier{hub: hub, entries: entries}
}

func (n *LeadChangeNotifier) LeadChanged(change lead.Change) {
	if n == nil || n.hub == nil || change.WorkspaceID == "" || change.LeadID == "" {
		return
	}
	go n.publish(change)
}

func (n *LeadChangeNotifier) publish(change lead.Change) {
	fields := change.Fields
	if fields == nil {
		fields = []string{}
	}
	event, err := json.Marshal(&WSOutgoingMessage{
		Type:    WSEventLeadUpdate,
		Payload: LeadUpdatePayload{LeadID: change.LeadID, Version: change.Version, Fields: fields},
	})
	if err != nil {
		log.Printf("[ConversationHub] lead update of %s: %v", change.LeadID, err)
		return
	}
	broadcast := leadUpdateBroadcast{Event: event, Entries: n.entryRefs(change)}
	n.hub.sendLeadUpdateLocal(change.WorkspaceID, broadcast)
	if raw, err := json.Marshal(broadcast); err == nil {
		n.hub.publishWorkspacePayload(leadUpdateBroadcastType, change.WorkspaceID, raw)
	}
}

func (n *LeadChangeNotifier) entryRefs(change lead.Change) []leadEntryRef {
	if n.entries == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), leadEntriesTimeout)
	defer cancel()
	refs, err := n.entries.EntryRefs(ctx, change.WorkspaceID, change.LeadID)
	if err != nil {
		log.Printf("[ConversationHub] conversations of lead %s unavailable, only lead readers are told: %v", change.LeadID, err)
		return nil
	}
	out := make([]leadEntryRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, leadEntryRef{ID: ref.EntryID, Type: string(ref.EntryType)})
	}
	return out
}

func (h *ConversationHub) deliverLeadUpdate(workspaceID string, payload []byte) {
	var broadcast leadUpdateBroadcast
	if err := json.Unmarshal(payload, &broadcast); err != nil || len(broadcast.Event) == 0 {
		return
	}
	h.sendLeadUpdateLocal(workspaceID, broadcast)
}

func (h *ConversationHub) sendLeadUpdateLocal(workspaceID string, broadcast leadUpdateBroadcast) {
	if workspaceID == "" || len(broadcast.Event) == 0 {
		return
	}
	subscribers := h.leadSubscribers(broadcast.Entries)
	h.sendToWorkspaceWhere(workspaceID, broadcast.Event, func(connID string, conn *WSConnection) bool {
		return subscribers[connID] || h.readsLeads(conn)
	})
}

func (h *ConversationHub) leadSubscribers(refs []leadEntryRef) map[string]bool {
	subscribers := map[string]bool{}
	h.subMu.RLock()
	defer h.subMu.RUnlock()
	for _, ref := range refs {
		for connID := range h.entrySubscribers[entrySubscription{entryID: ref.ID, entryType: ref.Type}] {
			subscribers[connID] = true
		}
	}
	return subscribers
}

func (h *ConversationHub) readsLeads(conn *WSConnection) bool {
	return h.authorizer != nil &&
		h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, string(workspace.ResourceLeads), string(workspace.ActionRead), conn.IsAdmin)
}

var _ lead.ChangeNotifier = (*LeadChangeNotifier)(nil)

const WSEventLeadsBulkUpdate WSEventType = "conversation:leads_bulk_update"

type LeadsBulkUpdatePayload struct {
	RunID string `json:"runId"`
}

func (n *LeadChangeNotifier) LeadsBulkUpdated(workspaceID, runID string) {
	if n == nil || n.hub == nil || workspaceID == "" || runID == "" {
		return
	}
	event, err := json.Marshal(&WSOutgoingMessage{Type: WSEventLeadsBulkUpdate, Payload: LeadsBulkUpdatePayload{RunID: runID}})
	if err != nil {
		log.Printf("[ConversationHub] bulk lead update of run %s: %v", runID, err)
		return
	}
	broadcast := leadUpdateBroadcast{Event: event}
	n.hub.sendLeadUpdateLocal(workspaceID, broadcast)
	if raw, err := json.Marshal(broadcast); err == nil {
		n.hub.publishWorkspacePayload(leadUpdateBroadcastType, workspaceID, raw)
	}
}
