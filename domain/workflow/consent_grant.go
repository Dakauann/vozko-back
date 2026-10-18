package workflow

import (
	"bytes"
	"encoding/json"
)

func GrantsConsent(g *Graph) bool {
	return len(ConsentGrantNodes(g)) > 0
}

func ConsentGrantNodes(g *Graph) []string {
	return ChangedConsentGrants(nil, g)
}

func ChangedConsentGrants(stored, proposed *Graph) []string {
	if proposed == nil {
		return nil
	}
	before := consentGrantsByID(stored)
	var changed []string
	for i := range proposed.Nodes {
		n := &proposed.Nodes[i]
		if !isConsentGrant(n) {
			continue
		}
		if previous, ok := before[n.ID]; ok && sameConfig(previous.Config, n.Config) {
			continue
		}
		changed = append(changed, n.ID)
	}
	return changed
}

func consentGrantsByID(g *Graph) map[string]*Node {
	out := map[string]*Node{}
	if g == nil {
		return out
	}
	for i := range g.Nodes {
		if n := &g.Nodes[i]; isConsentGrant(n) {
			out[n.ID] = n
		}
	}
	return out
}

func isConsentGrant(n *Node) bool {
	return n.Type == NodeTypeActionConsent && consentMode(n) == ConsentModeGrant
}

func sameConfig(a, b map[string]interface{}) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && bytes.Equal(left, right)
}
