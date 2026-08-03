package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// The three data keys are a contract between every channel's inbound handler
// and AdvanceOnReply. A channel that writes a different key does not fail
// loudly, it silently routes every button press down the no_match branch,
// which reads as "the customer typed something unexpected" and is very hard to
// trace back to a typo. These pin the keys.

func TestApplySelectionWritesTheKeysAdvanceOnReplyReads(t *testing.T) {
	data := map[string]interface{}{}
	ApplySelection(data, &OptionSelection{ID: "sim", Title: "Sim", Kind: "callback_query"})

	if got := data[DataKeySelectedOptionID]; got != "sim" {
		t.Errorf("%s = %v, want the option id", DataKeySelectedOptionID, got)
	}
	if got := data[DataKeySelectedOptionTitle]; got != "Sim" {
		t.Errorf("%s = %v", DataKeySelectedOptionTitle, got)
	}
	if got := data[DataKeySelectedOptionKind]; got != "callback_query" {
		t.Errorf("%s = %v", DataKeySelectedOptionKind, got)
	}
}

// The literal names, asserted once. resolveInteractiveReplyEdge reads
// "selected_option_id" and nothing else; renaming the constant must not
// silently rename the wire key.
func TestSelectionKeyNamesAreStable(t *testing.T) {
	if DataKeySelectedOptionID != "selected_option_id" {
		t.Errorf("id key = %q", DataKeySelectedOptionID)
	}
	if DataKeySelectedOptionTitle != "selected_option_title" {
		t.Errorf("title key = %q", DataKeySelectedOptionTitle)
	}
	if DataKeySelectedOptionKind != "selected_option_type" {
		t.Errorf("kind key = %q", DataKeySelectedOptionKind)
	}
}

// A typed reply is not a selection. Writing an empty option id would make
// AdvanceOnReply look for a branch labelled "" and find none.
func TestApplySelectionIgnoresANonSelection(t *testing.T) {
	for _, sel := range []*OptionSelection{nil, {ID: ""}} {
		data := map[string]interface{}{"message": "oi"}
		ApplySelection(data, sel)
		if _, present := data[DataKeySelectedOptionID]; present {
			t.Errorf("sel=%+v must not mark the event as a selection", sel)
		}
	}
}

func TestApplySelectionOmitsUnknownDisplayFields(t *testing.T) {
	data := map[string]interface{}{}
	ApplySelection(data, &OptionSelection{ID: "sim"})

	if _, present := data[DataKeySelectedOptionTitle]; present {
		t.Error("an unknown title must be absent, not an empty string")
	}
}

func TestApplySelectionToleratesANilMap(t *testing.T) {
	ApplySelection(nil, &OptionSelection{ID: "sim"})
}

// New workflows are authored with the channel-neutral value; the WhatsApp-era
// one still loads. Both halves matter: the first is what the user sees in the
// editor and the AI builder, the second is every workflow saved before the
// rename.
func TestNewWorkflowsUseTheChannelNeutralNodeType(t *testing.T) {
	if NodeTypeActionSendInteractive != "action_send_interactive" {
		t.Errorf("node type = %q, want the channel-neutral name", NodeTypeActionSendInteractive)
	}
	if !NodeTypeActionSendInteractive.IsInteractivePrompt() {
		t.Error("the interactive prompt must be recognised as one")
	}
}

func TestLegacyInteractiveNodeTypeStillResolves(t *testing.T) {
	legacy := NodeTypeActionSendWhatsappButtonLegacy

	if got := legacy.Canonical(); got != NodeTypeActionSendInteractive {
		t.Errorf("Canonical() = %q, want the current type", got)
	}
	// Every behavioural predicate must accept the old value, or a workflow saved
	// before the rename stops parking for the contact's reply.
	if !legacy.IsInteractivePrompt() {
		t.Error("a legacy interactive prompt must still park and branch")
	}
}

// Graphs are stored as JSONB, so decoding is the one place every read passes
// through. Normalizing here is what keeps the rest of the codebase from ever
// seeing a retired value.
func TestDecodingAGraphUpgradesTheLegacyNodeType(t *testing.T) {
	var n Node
	if err := json.Unmarshal([]byte(`{"id":"n5","type":"action_send_whatsapp_button"}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != NodeTypeActionSendInteractive {
		t.Errorf("decoded type = %q, want it normalized on read", n.Type)
	}
}

// Re-encoding writes the current value, so a saved workflow upgrades in place.
func TestReEncodingWritesTheCurrentNodeType(t *testing.T) {
	var n Node
	if err := json.Unmarshal([]byte(`{"id":"n5","type":"action_send_whatsapp_button"}`), &n); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"action_send_interactive"`) {
		t.Errorf("re-encoded = %s, want the current value", out)
	}
}

// An unknown type must pass through untouched rather than being coerced.
func TestCanonicalLeavesEveryOtherTypeAlone(t *testing.T) {
	for _, nt := range []NodeType{NodeTypeActionSendText, NodeTypeActionSendMedia, "action_made_up"} {
		if got := nt.Canonical(); got != nt {
			t.Errorf("Canonical(%q) = %q, want it unchanged", nt, got)
		}
	}
}
