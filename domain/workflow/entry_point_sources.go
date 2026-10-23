package workflow

import (
	"time"
)

type EntryPointSourceQuery struct {
	WorkspaceID string
	WorkflowIDs []string
	FormIDs     []string
}

type WorkspaceChannel struct {
	EntryChannel
	WorkflowID string
}

type EntryPointSources struct {
	Webhooks        map[string]WebhookEntry
	Forms           map[string]EntryForm
	Agents          map[string][]EntryAgent
	LinkedChannels  []WorkspaceChannel
	EnabledChannels []WorkspaceChannel
	LeadExits       map[string][]LeadExitEntry
	LastStarts      map[string]map[EntryPointKind]time.Time
	LastDeliveries  map[string]EntryDelivery
}

type EntryPointReader interface {
	PublishedTriggers(workflowIDs []string) (map[string][]Node, error)
	EntryPointSources(q EntryPointSourceQuery) (*EntryPointSources, error)
}

func RunningTriggers(w *Workflow, published map[string][]Node) []Node {
	if w == nil {
		return nil
	}
	if w.PublishedVersion != nil {
		return published[w.ID]
	}
	var out []Node
	for _, n := range w.Graph.TriggerNodes() {
		out = append(out, *n)
	}
	return out
}

func EntryPointQueryOf(workspaceID string, workflows []*Workflow, published map[string][]Node) EntryPointSourceQuery {
	q := EntryPointSourceQuery{WorkspaceID: workspaceID}
	forms := map[string]bool{}
	for _, w := range workflows {
		if w == nil {
			continue
		}
		q.WorkflowIDs = append(q.WorkflowIDs, w.ID)
		for _, node := range RunningTriggers(w, published) {
			if TriggerType(node.Type) != TriggerFormSubmitted {
				continue
			}
			ids, _ := formIDList(node.Config[FormTriggerFormIDsKey])
			for _, id := range ids {
				forms[id] = true
			}
		}
	}
	q.FormIDs = keysOf(forms)
	return q
}

func keysOf(set map[string]bool) []string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	return out
}

func (s *EntryPointSources) FactsOf(workflowID string) EntryPointFacts {
	if s == nil {
		return EntryPointFacts{}
	}
	facts := EntryPointFacts{
		Forms:      s.Forms,
		Agents:     s.Agents[workflowID],
		LeadExits:  s.LeadExits[workflowID],
		LastStarts: s.LastStarts[workflowID],
	}
	if webhook, ok := s.Webhooks[workflowID]; ok {
		facts.Webhook = &webhook
	}
	if delivery, ok := s.LastDeliveries[workflowID]; ok {
		facts.LastDelivery = &delivery
	}
	for _, c := range s.LinkedChannels {
		if c.WorkflowID == workflowID {
			facts.Channels = append(facts.Channels, c.EntryChannel)
		}
	}
	for _, c := range s.EnabledChannels {
		if c.WorkflowEnabled && c.WorkflowID == "" {
			facts.OpenChannels = append(facts.OpenChannels, c.EntryChannel)
		}
	}
	return facts
}
