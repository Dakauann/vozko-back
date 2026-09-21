package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

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
	if !legacy.IsInteractivePrompt() {
		t.Error("a legacy interactive prompt must still park and branch")
	}
}

func TestDecodingAGraphUpgradesTheLegacyNodeType(t *testing.T) {
	var n Node
	if err := json.Unmarshal([]byte(`{"id":"n5","type":"action_send_whatsapp_button"}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != NodeTypeActionSendInteractive {
		t.Errorf("decoded type = %q, want it normalized on read", n.Type)
	}
}

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

func TestCanonicalLeavesEveryOtherTypeAlone(t *testing.T) {
	for _, nt := range []NodeType{NodeTypeActionSendText, NodeTypeActionSendMedia, "action_made_up"} {
		if got := nt.Canonical(); got != nt {
			t.Errorf("Canonical(%q) = %q, want it unchanged", nt, got)
		}
	}
}
