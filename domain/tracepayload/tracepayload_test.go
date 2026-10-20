package tracepayload

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMapKeepsPayloadsThatFitUntouched(t *testing.T) {
	in := map[string]interface{}{"status": 200, "body": "ok"}
	got := Map(in)
	if got["status"] != 200 || got["body"] != "ok" || got[TruncatedKey] != nil {
		t.Fatalf("got %+v", got)
	}
	if Map(nil) != nil {
		t.Fatal("a missing payload stays missing")
	}
}

func TestMapReplacesAnOversizedPayloadWithAMarkedPreviewWithinTheCap(t *testing.T) {
	in := map[string]interface{}{"body": strings.Repeat("a", MaxBytes*2)}
	got := Map(in)
	if got[TruncatedKey] != true {
		t.Fatalf("the oversized payload must say it was truncated, got keys %v", keys(got))
	}
	if size, _ := got["size_bytes"].(int); size <= MaxBytes {
		t.Fatalf("size_bytes = %v, want the original size", got["size_bytes"])
	}
	encoded, _ := json.Marshal(got)
	if len(encoded) > MaxBytes {
		t.Fatalf("capped payload is %d bytes, over the %d cap", len(encoded), MaxBytes)
	}
}

func TestTextCutsOnARuneBoundaryAndSaysSo(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		limit     int
		want      string
		truncated bool
	}{
		{"fits", "olá", 10, "olá", false},
		{"exact", "abc", 3, "abc", false},
		{"cut ascii", "abcdef", 3, "abc", true},
		{"cut inside a rune", "aé", 2, "a", true},
		{"no limit means the default cap", "x", 0, "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := Text(tc.in, tc.limit)
			if got != tc.want || truncated != tc.truncated {
				t.Fatalf("Text(%q, %d) = %q, %v; want %q, %v", tc.in, tc.limit, got, truncated, tc.want, tc.truncated)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("cut produced invalid UTF-8: %q", got)
			}
		})
	}
}

func TestMarkedTextAppendsTheMarkerOnlyWhenCut(t *testing.T) {
	if got := MarkedText("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("b", MaxBytes+10)
	got := MarkedText(long)
	if !strings.HasSuffix(got, Marker) || len(got) > MaxBytes {
		t.Fatalf("marked text has %d bytes and suffix %q", len(got), got[len(got)-20:])
	}
}

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
