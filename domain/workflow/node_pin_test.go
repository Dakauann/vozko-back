package workflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/tracepayload"
)

func pinnedGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerWebhook},
			{ID: "http", Type: NodeTypeActionHTTPRequest, Config: map[string]interface{}{"url": "https://api.example.com"}},
			{ID: "ok", Type: NodeTypeActionSetVariable},
			{ID: "fail", Type: NodeTypeActionSetVariable},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{Source: "t", Target: "http"},
			{Source: "http", Target: "ok", Label: "success"},
			{Source: "http", Target: "fail", Label: "error"},
			{Source: "ok", Target: "end"},
			{Source: "fail", Target: "end"},
		},
	}
}

var pinTime = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func TestSetPinValidatesBeforeTouchingTheGraph(t *testing.T) {
	huge := map[string]interface{}{"body": strings.Repeat("a", tracepayload.MaxBytes+1)}
	cases := []struct {
		name   string
		nodeID string
		pin    NodePin
		want   error
	}{
		{"manual pin on an action", "http", NodePin{Output: map[string]interface{}{"status": 200.0}, Source: PinSourceManual, PinnedAt: pinTime}, nil},
		{"simulation pin", "http", NodePin{Output: map[string]interface{}{}, Source: PinSourceSimulation, PinnedAt: pinTime}, nil},
		{"production pin", "http", NodePin{Output: map[string]interface{}{}, Source: PinSourceOfRun("run-1"), PinnedAt: pinTime}, nil},
		{"unknown node", "nope", NodePin{Source: PinSourceManual, PinnedAt: pinTime}, ErrPinNodeNotFound},
		{"trigger has nothing to replace", "t", NodePin{Source: PinSourceManual, PinnedAt: pinTime}, ErrPinNotAllowed},
		{"end has nothing to replace", "end", NodePin{Source: PinSourceManual, PinnedAt: pinTime}, ErrPinNotAllowed},
		{"unknown source", "http", NodePin{Source: "elsewhere", PinnedAt: pinTime}, ErrPinSourceInvalid},
		{"run source without a run id", "http", NodePin{Source: "run:", PinnedAt: pinTime}, ErrPinSourceInvalid},
		{"missing pin time", "http", NodePin{Source: PinSourceManual}, ErrPinInvalid},
		{"output over the step payload cap", "http", NodePin{Output: huge, Source: PinSourceManual, PinnedAt: pinTime}, ErrPinTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := pinnedGraph()
			err := SetPin(&g, tc.nodeID, tc.pin)
			if !errors.Is(err, tc.want) {
				t.Fatalf("SetPin error = %v, want %v", err, tc.want)
			}
			if tc.want != nil && g.HasPins() {
				t.Fatal("a refused pin must leave the graph untouched")
			}
			if tc.want == nil && g.FindNode(tc.nodeID).Pinned == nil {
				t.Fatal("an accepted pin must be stored on the node")
			}
		})
	}
}

func TestStripPinsNeverChangesTheDraftItCopies(t *testing.T) {
	g := pinnedGraph()
	if err := SetPin(&g, "http", NodePin{Output: map[string]interface{}{"a": 1.0}, Source: PinSourceManual, PinnedAt: pinTime}); err != nil {
		t.Fatal(err)
	}
	stripped := StripPins(g)
	if stripped.HasPins() {
		t.Fatal("a published version must never carry a pin")
	}
	if g.FindNode("http").Pinned == nil {
		t.Fatal("stripping for publish must keep the draft's pins for the next test")
	}
	if len(stripped.Nodes) != len(g.Nodes) || len(stripped.Edges) != len(g.Edges) {
		t.Fatal("stripping pins must keep every node and edge")
	}
}

func TestGraphHashIgnoresPinsSoTestDataNeverReadsAsAnUnpublishedChange(t *testing.T) {
	g := pinnedGraph()
	before := GraphHash(&g)
	if err := SetPin(&g, "http", NodePin{Output: map[string]interface{}{"a": 1.0}, Source: PinSourceManual, PinnedAt: pinTime}); err != nil {
		t.Fatal(err)
	}
	if GraphHash(&g) != before {
		t.Fatal("a pin is test data, it must not change the publish hash")
	}
	if g.FindNode("http").Pinned == nil {
		t.Fatal("hashing must not drop the pin from the draft")
	}
}

func TestCloneCopiesThePinInsteadOfSharingIt(t *testing.T) {
	g := pinnedGraph()
	if err := SetPin(&g, "http", NodePin{Output: map[string]interface{}{"a": 1.0}, Source: PinSourceManual, PinnedAt: pinTime}); err != nil {
		t.Fatal(err)
	}
	clone := g.Clone()
	clone.FindNode("http").Pinned.Output["a"] = 2.0
	if g.FindNode("http").Pinned.Output["a"] != 1.0 {
		t.Fatal("a cloned graph must not share pin outputs with the original")
	}
}

func TestPinnedResultFollowsThePinnedEdge(t *testing.T) {
	g := pinnedGraph()
	cases := []struct {
		name     string
		edge     string
		wantNext string
		wantKind ErrorKind
	}{
		{"labelled edge", "error", "fail", ""},
		{"no edge means the default edge", "", "", ""},
		{"edge removed from the draft fails closed", "timeout", "", ErrorKindConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pin := NodePin{Output: map[string]interface{}{"status": 500.0}, Edge: tc.edge, Source: PinSourceManual, PinnedAt: pinTime}
			result := PinnedResult(&g, g.FindNode("http"), &pin)
			if result.NextNodeID != tc.wantNext || result.ErrorKind != tc.wantKind {
				t.Fatalf("got next %q kind %q, want %q %q", result.NextNodeID, result.ErrorKind, tc.wantNext, tc.wantKind)
			}
			if tc.wantKind != "" && result.Error == "" {
				t.Fatal("a pin that cannot be followed needs a message")
			}
			if tc.wantKind == "" && result.Output["status"] != 500.0 {
				t.Fatalf("output = %v, want the pinned output", result.Output)
			}
		})
	}
}

func TestPinnedResultDoesNotLetTheRunMutateThePin(t *testing.T) {
	g := pinnedGraph()
	pin := NodePin{Output: map[string]interface{}{"a": 1.0}, Source: PinSourceManual, PinnedAt: pinTime}
	result := PinnedResult(&g, g.FindNode("http"), &pin)
	result.Output["a"] = 2.0
	if pin.Output["a"] != 1.0 {
		t.Fatal("a run must get a copy of the pinned output")
	}
}

func TestPinFromStepCopiesTheRecordedOutputAndBranch(t *testing.T) {
	executed := &WorkflowRunLog{NodeID: "http", Status: LogStatusExecuted, Output: map[string]interface{}{"cpf": "•••• 12"}, Branch: "success"}
	pin, err := PinFromStep(executed, "run-1", pinTime)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Source != PinSourceOfRun("run-1") || pin.Edge != "success" || pin.Output["cpf"] != "•••• 12" || !pin.PinnedAt.Equal(pinTime) {
		t.Fatalf("pin = %+v", pin)
	}
	for _, status := range []LogStatus{LogStatusFailed, LogStatusCancelled, LogStatusWaiting, LogStatusGate} {
		if _, err := PinFromStep(&WorkflowRunLog{NodeID: "http", Status: status}, "run-1", pinTime); !errors.Is(err, ErrPinStepUnusable) {
			t.Fatalf("status %s: err = %v, want ErrPinStepUnusable, a step that did not finish has no output to replay", status, err)
		}
	}
}

func TestReplayingAPinnedHTTPCaptureFillsTheVariableTheNodeWouldHaveSet(t *testing.T) {
	cases := []struct {
		name   string
		output map[string]interface{}
		want   interface{}
	}{
		{"json answer", map[string]interface{}{"status_code": 200.0, "json": map[string]interface{}{"token": "t"}, "body": `{"token":"t"}`}, map[string]interface{}{"token": "t"}},
		{"text answer", map[string]interface{}{"status_code": 502.0, "body": "bad gateway"}, "bad gateway"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := NewRunState()
			node := Node{ID: "h", Type: NodeTypeActionHTTPRequest, Config: map[string]interface{}{"capture_variable": "login"}}
			ReplayPinnedStateWrites(&node, tc.output, &state)
			got, _ := state.Get("login")
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("login = %v, want %v", got, tc.want)
			}
			meta, _ := state.Get(HTTPMetaPrefix + "login")
			if meta == nil {
				t.Fatal("the HTTP status of the capture must be replayed too")
			}
		})
	}
	state := NewRunState()
	ReplayPinnedStateWrites(&Node{ID: "s", Type: NodeTypeActionSetVariable}, map[string]interface{}{"x": 1.0}, &state)
	if len(state.Vars) != 0 {
		t.Fatalf("a node without a capture writes nothing, got %v", state.Vars)
	}
}

func TestValidatePinsChecksEveryPinnedNode(t *testing.T) {
	g := pinnedGraph()
	g.Nodes[0].Pinned = &NodePin{Source: PinSourceManual, PinnedAt: pinTime}
	if err := ValidatePins(&g); !errors.Is(err, ErrPinNotAllowed) {
		t.Fatalf("ValidatePins = %v, want ErrPinNotAllowed for a pinned trigger", err)
	}
	clean := pinnedGraph()
	if err := ValidatePins(&clean); err != nil {
		t.Fatalf("a graph without pins is valid, got %v", err)
	}
}
