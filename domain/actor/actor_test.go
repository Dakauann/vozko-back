package actor

import "testing"

func TestFormatAI(t *testing.T) {
	if got := FormatAI("abc"); got != "ai:abc" {
		t.Fatalf("FormatAI = %q", got)
	}
	if got := FormatAI("ai:abc"); got != "ai:abc" {
		t.Fatalf("FormatAI idempotent = %q", got)
	}
	if got := FormatAI("  "); got != "" {
		t.Fatalf("FormatAI empty = %q", got)
	}
}

func TestParseAI(t *testing.T) {
	if got := ParseAI("ai:uuid-1"); got != "uuid-1" {
		t.Fatalf("ParseAI = %q", got)
	}
	if got := ParseAI("uuid-1"); got != "" {
		t.Fatalf("ParseAI non-ai = %q", got)
	}
}

func TestKindOf(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{"", KindSystem},
		{SystemID, KindSystem},
		{"ai:agent-1", KindAI},
		{"user-uuid", KindHuman},
		{"campaign:camp-1", KindCampaign},
	}
	for _, tc := range cases {
		if got := KindOf(tc.in); got != tc.want {
			t.Errorf("KindOf(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	k, id := Normalize(KindAI, "agent-9")
	if k != KindAI || id != "ai:agent-9" {
		t.Fatalf("Normalize AI = %q %q", k, id)
	}
	k, id = Normalize(KindHuman, "u1")
	if k != KindHuman || id != "u1" {
		t.Fatalf("Normalize human = %q %q", k, id)
	}
	k, id = Normalize("", "")
	if k != KindSystem || id != SystemID {
		t.Fatalf("Normalize empty = %q %q", k, id)
	}
}

func TestKindValid(t *testing.T) {
	if !KindHuman.Valid() || !KindAI.Valid() || !KindSystem.Valid() || !KindCampaign.Valid() {
		t.Fatal("expected valid kinds")
	}
	if Kind("nope").Valid() {
		t.Fatal("invalid kind should fail")
	}
}

func TestFormatCampaign(t *testing.T) {
	if got := FormatCampaign("camp-1"); got != "campaign:camp-1" {
		t.Fatalf("FormatCampaign = %q", got)
	}
	if got := FormatCampaign("campaign:camp-1"); got != "campaign:camp-1" {
		t.Fatalf("FormatCampaign twice = %q", got)
	}
	if got := FormatCampaign(" "); got != "" {
		t.Fatalf("FormatCampaign blank = %q", got)
	}
}
