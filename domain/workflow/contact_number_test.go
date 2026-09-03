package workflow

import (
	"strings"
	"testing"
)

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

// The seeding site reads the CRM's lead row first, but that lookup is
// best-effort — its error is discarded, and it is skipped when the receiving
// workspace cannot be resolved. Live traffic hit exactly that: an empty
// {{contact_number}} turned a per-contact URL into ".jpeg" with nothing in front
// of it, and every fetch came back 404.
//
// This pins the rule the seeding site has to honour: a run always has a sender,
// so a template that interpolates the contact must never resolve to nothing.
func TestContactNumberNeverInterpolatesToNothing(t *testing.T) {
	const senderFallback = "5584994409624"

	for _, tc := range []struct {
		name         string
		leadNumber   string // "" means the lead row was not resolved
		wantResolved string
	}{
		{"lead row found", "5584994409624", "5584994409624"},
		{"lead lookup missed", "", senderFallback},
		{"lead row present but blank", "   ", senderFallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Mirrors the seeding site: lead first, sender as the fallback.
			contact := strings.TrimSpace(tc.leadNumber)
			if contact == "" {
				contact = senderFallback
			}

			data := map[string]interface{}{}
			ApplyContactNumber(data, contact)

			state := RunState{Vars: data}
			out := InterpolateMap(map[string]interface{}{
				"media_url": "https://cdn.example.com/evento/{{contact_number}}.jpeg",
			}, &state, nil)

			got, _ := out["media_url"].(string)
			if strings.Contains(got, "//evento/.jpeg") || strings.HasSuffix(got, "/.jpeg") {
				t.Fatalf("the contact vanished from the URL: %s", got)
			}
			want := "https://cdn.example.com/evento/" + tc.wantResolved + ".jpeg"
			if got != want {
				t.Errorf("url = %q, want %q", got, want)
			}
		})
	}
}
