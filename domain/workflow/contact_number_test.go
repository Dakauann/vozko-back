package workflow

import (
	"strings"
	"testing"
)

func TestApplyContactNumberSeedsTheKeyAuthorsType(t *testing.T) {
	data := map[string]interface{}{}
	ApplyContactNumber(data, " 5584994409624 ")

	if got := data["contact_number"]; got != "5584994409624" {
		t.Errorf("contact_number = %v, want the trimmed address", got)
	}
	if DataKeyContactNumber != "contact_number" {
		t.Errorf("key constant = %q, must stay contact_number", DataKeyContactNumber)
	}
}

func TestApplyContactNumberSkipsEmptyAndNilSafely(t *testing.T) {
	ApplyContactNumber(nil, "5511999999999")

	data := map[string]interface{}{}
	ApplyContactNumber(data, "")
	ApplyContactNumber(data, "   ")
	if _, ok := data[DataKeyContactNumber]; ok {
		t.Errorf("empty address must write nothing, got %v", data[DataKeyContactNumber])
	}
}

func TestContactNumberInterpolatesInNodeConfig(t *testing.T) {
	data := map[string]interface{}{}
	ApplyContactNumber(data, "5584994409624")

	state := RunState{Vars: data}
	out := InterpolateMap(map[string]interface{}{
		"url": "https://cdn.example.com/eventos/{{contact_number}}.jpeg",
	}, &state, nil)

	if got := out["url"]; got != "https://cdn.example.com/eventos/5584994409624.jpeg" {
		t.Errorf("url = %v, the address must interpolate", got)
	}
}

func TestContactNumberNeverInterpolatesToNothing(t *testing.T) {
	const senderFallback = "5584994409624"

	for _, tc := range []struct {
		name         string
		leadNumber   string
		wantResolved string
	}{
		{"lead row found", "5584994409624", "5584994409624"},
		{"lead lookup missed", "", senderFallback},
		{"lead row present but blank", "   ", senderFallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
