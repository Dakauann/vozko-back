package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"vozko/domain/tracepayload"
)

type PinSource string

const (
	PinSourceManual     PinSource = "manual"
	PinSourceSimulation PinSource = "simulation"
	pinSourceRunPrefix            = "run:"
)

func PinSourceOfRun(runID string) PinSource {
	return PinSource(pinSourceRunPrefix + runID)
}

func (s PinSource) RunID() string {
	if !strings.HasPrefix(string(s), pinSourceRunPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(string(s), pinSourceRunPrefix))
}

func (s PinSource) Valid() bool {
	switch s {
	case PinSourceManual, PinSourceSimulation:
		return true
	}
	return s.RunID() != ""
}

type NodePin struct {
	Output   map[string]interface{} `json:"output"`
	Edge     string                 `json:"edge,omitempty"`
	Source   PinSource              `json:"source"`
	PinnedAt time.Time              `json:"pinned_at"`
	Withheld bool                   `json:"withheld,omitempty"`
}

var (
	ErrPinInvalid       = errors.New("workflow: invalid pinned output")
	ErrPinNodeNotFound  = fmt.Errorf("%w: node not found", ErrPinInvalid)
	ErrPinNotAllowed    = fmt.Errorf("%w: triggers, end and decoration nodes cannot be pinned", ErrPinInvalid)
	ErrPinSourceInvalid = fmt.Errorf("%w: source must be manual, simulation or run:<id da execução>", ErrPinInvalid)
	ErrPinTooLarge      = fmt.Errorf("%w: output is larger than %d bytes", ErrPinInvalid, tracepayload.MaxBytes)
	ErrPinStepUnusable  = fmt.Errorf("%w: only a step that finished (executed or skipped) has an output to pin", ErrPinInvalid)
	ErrPinWithheld      = fmt.Errorf("%w: a withheld pin has no output and is only kept when it matches the stored pin", ErrPinInvalid)
)

const PinWithheldNotice = "saída fixada indisponível para você"

func (p *NodePin) Servable() bool {
	return p != nil && !p.Withheld
}

func (p *NodePin) WithheldNotice() string {
	if p == nil || !p.Withheld {
		return ""
	}
	return PinWithheldNotice
}

func (p NodePin) clone() *NodePin {
	cp := p
	cp.Output = cloneConfigMap(p.Output)
	return &cp
}

func pinnable(t NodeType) bool {
	return !t.IsTrigger() && !t.IsEnd() && !t.IsDecoration()
}

func ValidatePin(n Node) error {
	if n.Pinned == nil {
		return nil
	}
	if !pinnable(n.Type) {
		return fmt.Errorf("%w: node %s (%s)", ErrPinNotAllowed, n.ID, n.Type)
	}
	if !n.Pinned.Source.Valid() {
		return fmt.Errorf("%w: node %s has %q", ErrPinSourceInvalid, n.ID, n.Pinned.Source)
	}
	if n.Pinned.Withheld {
		return fmt.Errorf("%w: node %s", ErrPinWithheld, n.ID)
	}
	if n.Pinned.PinnedAt.IsZero() {
		return fmt.Errorf("%w: node %s has no pinned_at", ErrPinInvalid, n.ID)
	}
	encoded, err := json.Marshal(n.Pinned.Output)
	if err != nil {
		return fmt.Errorf("%w: node %s output is not JSON", ErrPinInvalid, n.ID)
	}
	if len(encoded) > tracepayload.MaxBytes {
		return fmt.Errorf("%w: node %s has %d bytes", ErrPinTooLarge, n.ID, len(encoded))
	}
	return nil
}

func ValidatePins(g *Graph) error {
	for _, n := range g.Nodes {
		if err := ValidatePin(n); err != nil {
			return err
		}
	}
	return nil
}

func SetPin(g *Graph, nodeID string, pin NodePin) error {
	node := g.FindNode(nodeID)
	if node == nil {
		return fmt.Errorf("%w: %s", ErrPinNodeNotFound, nodeID)
	}
	candidate := *node
	candidate.Pinned = pin.clone()
	if err := ValidatePin(candidate); err != nil {
		return err
	}
	node.Pinned = candidate.Pinned
	return nil
}

func (g *Graph) HasPins() bool {
	for _, n := range g.Nodes {
		if n.Pinned != nil {
			return true
		}
	}
	return false
}

func RunPinIDs(g *Graph) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, n := range g.Nodes {
		if n.Pinned == nil {
			continue
		}
		if id := n.Pinned.Source.RunID(); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func WithholdPins(g Graph, visible func(runID string) bool) Graph {
	out := Graph{Nodes: make([]Node, len(g.Nodes)), Edges: g.Edges}
	for i, n := range g.Nodes {
		if n.Pinned != nil {
			if runID := n.Pinned.Source.RunID(); runID != "" && !visible(runID) {
				n.Pinned = &NodePin{Edge: n.Pinned.Edge, Source: n.Pinned.Source, PinnedAt: n.Pinned.PinnedAt, Withheld: true}
			}
		}
		out.Nodes[i] = n
	}
	return out
}

func RestoreWithheldPins(incoming, stored Graph) Graph {
	out := Graph{Nodes: make([]Node, len(incoming.Nodes)), Edges: incoming.Edges}
	for i, n := range incoming.Nodes {
		if n.Pinned != nil && n.Pinned.Withheld {
			n.Pinned = storedPinMatching(&stored, n.ID, n.Pinned)
		}
		out.Nodes[i] = n
	}
	return out
}

func storedPinMatching(stored *Graph, nodeID string, withheld *NodePin) *NodePin {
	node := stored.FindNode(nodeID)
	if node == nil || node.Pinned == nil || node.Pinned.Withheld || node.Pinned.Source != withheld.Source {
		return nil
	}
	return node.Pinned.clone()
}

func PinnedResult(g *Graph, n *Node, pin *NodePin) *NodeResult {
	output := cloneConfigMap(pin.Output)
	if pin.Edge == "" {
		return &NodeResult{Output: output}
	}
	for _, e := range g.OutgoingEdges(n.ID) {
		if e.Label == pin.Edge {
			return &NodeResult{Output: output, NextNodeID: e.Target}
		}
	}
	return &NodeResult{
		Output:    output,
		Error:     fmt.Sprintf("A saída fixada do nó %s segue a conexão %q, que não existe mais no rascunho. Fixe a saída de novo ou recrie a conexão.", n.ID, pin.Edge),
		ErrorKind: ErrorKindConfig,
	}
}

func ServePinned(g *Graph, n *Node, pin *NodePin, state *RunState) *NodeResult {
	result := PinnedResult(g, n, pin)
	if result.Error == "" {
		ReplayPinnedStateWrites(n, result.Output, state)
	}
	return result
}

const HTTPMetaPrefix = "_httpmeta_"

func RecordHTTPCapture(state *RunState, captureVar string, body interface{}, statusCode int) {
	state.Set(captureVar, body)
	state.Set(HTTPMetaPrefix+captureVar, map[string]interface{}{
		"status_code": statusCode,
		"success":     statusCode < 400,
	})
}

func ReplayPinnedStateWrites(n *Node, output map[string]interface{}, state *RunState) {
	if n.Type != NodeTypeActionHTTPRequest {
		return
	}
	captureVar, _ := n.Config["capture_variable"].(string)
	if captureVar == "" {
		return
	}
	body, ok := output["json"]
	if !ok {
		body = output["body"]
	}
	status := 0
	switch code := output["status_code"].(type) {
	case float64:
		status = int(code)
	case int:
		status = code
	}
	RecordHTTPCapture(state, captureVar, body, status)
}

func (l *WorkflowRunLog) Pinnable() bool {
	return l != nil && slices.Contains(pinnableStepStatuses, l.Status)
}

func PinFromStep(step *WorkflowRunLog, runID string, at time.Time) (NodePin, error) {
	if !step.Pinnable() {
		return NodePin{}, ErrPinStepUnusable
	}
	return NodePin{
		Output:   cloneConfigMap(step.Output),
		Edge:     step.Branch,
		Source:   PinSourceOfRun(runID),
		PinnedAt: at,
	}, nil
}
