package workflow

import (
	"errors"
	"reflect"
	"testing"
)

func graphWithEveryPinSource() Graph {
	g := pinnedGraph()
	g.Nodes[1].Pinned = &NodePin{Output: map[string]interface{}{"cpf": "•••• 33"}, Edge: "success", Source: PinSourceOfRun("run-hidden"), PinnedAt: pinTime}
	g.Nodes[2].Pinned = &NodePin{Output: map[string]interface{}{"ok": true}, Source: PinSourceOfRun("run-seen"), PinnedAt: pinTime}
	g.Nodes[3].Pinned = &NodePin{Output: map[string]interface{}{"typed": "by hand"}, Source: PinSourceManual, PinnedAt: pinTime}
	return g
}

func onlyRun(id string) func(string) bool {
	return func(runID string) bool { return runID == id }
}

func TestWithholdPinsHidesOnlyProductionOutputsTheViewerCannotSee(t *testing.T) {
	g := graphWithEveryPinSource()
	out := WithholdPins(g, onlyRun("run-seen"))

	hidden := out.FindNode("http").Pinned
	if hidden == nil || !hidden.Withheld || hidden.Output != nil {
		t.Fatalf("a pin from a conversation the viewer cannot see keeps no output: %+v", hidden)
	}
	if hidden.Edge != "success" || hidden.Source != PinSourceOfRun("run-hidden") || !hidden.PinnedAt.Equal(pinTime) {
		t.Fatalf("the pin mark, edge and source stay so the editor can show it: %+v", hidden)
	}
	if seen := out.FindNode("ok").Pinned; seen.Withheld || seen.Output["ok"] != true {
		t.Fatalf("a pin from a visible conversation is untouched: %+v", seen)
	}
	if manual := out.FindNode("fail").Pinned; manual.Withheld || manual.Output["typed"] != "by hand" {
		t.Fatalf("a hand-written pin has no conversation behind it: %+v", manual)
	}
	if g.FindNode("http").Pinned.Output["cpf"] != "•••• 33" {
		t.Fatal("withholding must never change the stored graph it was given")
	}
}

func TestRunPinIDsListsEachProductionSourceOnce(t *testing.T) {
	g := graphWithEveryPinSource()
	g.Nodes[3].Pinned = &NodePin{Output: map[string]interface{}{}, Source: PinSourceOfRun("run-hidden"), PinnedAt: pinTime}
	if got := RunPinIDs(&g); !reflect.DeepEqual(got, []string{"run-hidden", "run-seen"}) {
		t.Fatalf("run ids = %v", got)
	}
}

func TestAWithheldPinIsNeverAcceptedAsAPin(t *testing.T) {
	n := Node{ID: "http", Type: NodeTypeActionHTTPRequest, Pinned: &NodePin{Source: PinSourceOfRun("run-1"), PinnedAt: pinTime, Withheld: true}}
	if err := ValidatePin(n); !errors.Is(err, ErrPinWithheld) {
		t.Fatalf("ValidatePin = %v, want ErrPinWithheld", err)
	}
}

func TestRestoreWithheldPinsKeepsTheStoredOutputOnSave(t *testing.T) {
	stored := graphWithEveryPinSource()
	viewed := WithholdPins(stored, onlyRun("run-seen"))

	cases := []struct {
		name  string
		edit  func(g *Graph)
		check func(t *testing.T, g Graph)
	}{
		{"saving the graph as viewed restores the hidden output", func(*Graph) {}, func(t *testing.T, g Graph) {
			pin := g.FindNode("http").Pinned
			if pin == nil || pin.Withheld || pin.Output["cpf"] != "•••• 33" || pin.Edge != "success" {
				t.Fatalf("pin = %+v", pin)
			}
		}},
		{"clearing a hidden pin is allowed", func(g *Graph) { g.FindNode("http").Pinned = nil }, func(t *testing.T, g Graph) {
			if g.FindNode("http").Pinned != nil {
				t.Fatal("the viewer removed the pin")
			}
		}},
		{"a withheld pin moved to a node that never had it is dropped", func(g *Graph) {
			g.FindNode("fail").Pinned = g.FindNode("http").Pinned
		}, func(t *testing.T, g Graph) {
			if g.FindNode("fail").Pinned != nil {
				t.Fatal("a stripped pin must never become the stored output of another node")
			}
		}},
		{"a withheld pin naming another run than the stored one is dropped", func(g *Graph) {
			pin := *g.FindNode("http").Pinned
			pin.Source = PinSourceOfRun("run-other")
			g.FindNode("http").Pinned = &pin
		}, func(t *testing.T, g Graph) {
			if g.FindNode("http").Pinned != nil {
				t.Fatal("only the exact stored pin is restored")
			}
		}},
		{"visible and hand pins pass through as sent", func(g *Graph) {
			g.FindNode("fail").Pinned.Output = map[string]interface{}{"typed": "edited"}
		}, func(t *testing.T, g Graph) {
			if g.FindNode("fail").Pinned.Output["typed"] != "edited" || g.FindNode("ok").Pinned.Output["ok"] != true {
				t.Fatalf("pins = %+v %+v", g.FindNode("fail").Pinned, g.FindNode("ok").Pinned)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			incoming := viewed.Clone()
			tc.edit(&incoming)
			restored := RestoreWithheldPins(incoming, stored)
			tc.check(t, restored)
			if err := ValidatePins(&restored); err != nil {
				t.Fatalf("the restored graph must validate: %v", err)
			}
		})
	}
	if stored.FindNode("http").Pinned.Output["cpf"] != "•••• 33" {
		t.Fatal("restoring must not alias the stored pin")
	}
}
