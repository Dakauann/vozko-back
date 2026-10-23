package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/tracepayload"
)

type TestInput struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Input   map[string]interface{} `json:"input"`
	SavedAt time.Time              `json:"savedAt"`
}

var (
	ErrTestInputInvalid    = errors.New("workflow: invalid saved test input")
	ErrTestInputNotAllowed = fmt.Errorf("%w: only trigger nodes keep test inputs", ErrTestInputInvalid)
	ErrTestInputTooLarge   = fmt.Errorf("%w: input is larger than %d bytes", ErrTestInputInvalid, tracepayload.MaxBytes)
)

func (in TestInput) clone() TestInput {
	cp := in
	cp.Input = cloneConfigMap(in.Input)
	return cp
}

func cloneTestInputs(inputs []TestInput) []TestInput {
	if inputs == nil {
		return nil
	}
	out := make([]TestInput, len(inputs))
	for i, in := range inputs {
		out[i] = in.clone()
	}
	return out
}

func ValidateTestInputs(g *Graph) error {
	for _, n := range g.Nodes {
		if err := validateNodeTestInputs(n); err != nil {
			return err
		}
	}
	return nil
}

func validateNodeTestInputs(n Node) error {
	if len(n.TestInputs) == 0 {
		return nil
	}
	if !n.Type.IsTrigger() {
		return fmt.Errorf("%w: node %s (%s)", ErrTestInputNotAllowed, n.ID, n.Type)
	}
	ids := make(map[string]bool, len(n.TestInputs))
	names := make(map[string]bool, len(n.TestInputs))
	for _, in := range n.TestInputs {
		id, name := strings.TrimSpace(in.ID), strings.ToLower(strings.TrimSpace(in.Name))
		switch {
		case id == "":
			return fmt.Errorf("%w: node %s has a test input without id", ErrTestInputInvalid, n.ID)
		case name == "":
			return fmt.Errorf("%w: node %s has a test input without name", ErrTestInputInvalid, n.ID)
		case ids[id]:
			return fmt.Errorf("%w: node %s repeats the test input id %q", ErrTestInputInvalid, n.ID, id)
		case names[name]:
			return fmt.Errorf("%w: node %s repeats the test input name %q", ErrTestInputInvalid, n.ID, strings.TrimSpace(in.Name))
		}
		ids[id], names[name] = true, true
		encoded, err := json.Marshal(in.Input)
		if err != nil {
			return fmt.Errorf("%w: node %s test input %q is not JSON", ErrTestInputInvalid, n.ID, id)
		}
		if len(encoded) > tracepayload.MaxBytes {
			return fmt.Errorf("%w: node %s test input %q has %d bytes", ErrTestInputTooLarge, n.ID, id, len(encoded))
		}
	}
	return nil
}

func (g *Graph) HasDraftOnly() bool {
	for _, n := range g.Nodes {
		if n.Pinned != nil || len(n.TestInputs) > 0 {
			return true
		}
	}
	return false
}

func StripDraftOnly(g Graph) Graph {
	if !g.HasDraftOnly() {
		return g
	}
	out := Graph{Nodes: make([]Node, len(g.Nodes)), Edges: g.Edges}
	for i, n := range g.Nodes {
		n.Pinned = nil
		n.TestInputs = nil
		out.Nodes[i] = n
	}
	return out
}
