package workflow

import "testing"

// contact_number is a contract between four channel handlers and every
// {{contact_number}} a workflow author types into a node. Like the selection
// keys, a channel writing its own spelling does not fail loudly — the variable
// just interpolates to nothing on that one channel, which surfaces as "the
// workflow works on WhatsApp but not on Telegram" with no error anywhere.

func TestApplyContactNumberSeedsTheKeyAuthorsType(t *testing.T) {
	data := map[string]interface{}{}
	ApplyContactNumber(data, " 5584994409624 ")

	// The literal, not the constant: node configs store the template as text,
	// so renaming the constant must not silently strand every saved workflow.
	if got := data["contact_number"]; got != "5584994409624" {
		t.Errorf("contact_number = %v, want the trimmed address", got)
	}
	if DataKeyContactNumber != "contact_number" {
		t.Errorf("key constant = %q, must stay contact_number", DataKeyContactNumber)
	}
}

func TestApplyContactNumberSkipsEmptyAndNilSafely(t *testing.T) {
	ApplyContactNumber(nil, "5511999999999") // must not panic

	data := map[string]interface{}{}
	ApplyContactNumber(data, "")
	ApplyContactNumber(data, "   ")
	if _, ok := data[DataKeyContactNumber]; ok {
		// An empty value must be absent, not "", so templates fall back the
		// same way they do on a channel that never learned the id.
		t.Errorf("empty address must write nothing, got %v", data[DataKeyContactNumber])
	}
}

// The end-to-end spelling check: what the helper seeds is what a node template
// resolves. This is the exact chain of the feature's motivating case — an
// http_request node with .../{{contact_number}}.jpeg looking up a per-contact
// image.
func TestContactNumberInterpolatesInNodeConfig(t *testing.T) {
	data := map[string]interface{}{}
	ApplyContactNumber(data, "5584994409624")

	// newTriggeredRun copies event data into state verbatim; RunState{Vars: data}
	// is that state.
	state := RunState{Vars: data}
	out := InterpolateMap(map[string]interface{}{
		"url": "https://cdn.example.com/eventos/{{contact_number}}.jpeg",
	}, &state, nil)

	if got := out["url"]; got != "https://cdn.example.com/eventos/5584994409624.jpeg" {
		t.Errorf("url = %v, the address must interpolate", got)
	}
}
