package metaembeddedsignup

import (
	"strings"
	"testing"
)

// Standard Multi-Partner-Solution flow (WHATSAPP_ES_FEATURE_TYPE empty): the popup
// carries solutionID in setup + version v4, and NO featureType. This is the tested,
// production onboarding config.
func TestEmbeddedSignupExtras_StandardFlow(t *testing.T) {
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{
		AppID: "APPID", ConfigID: "CFG", SolutionID: "SOL123", ESFeatureType: "", ESVersion: "",
	})
	got := h.embeddedSignupExtras()
	for _, want := range []string{"solutionID: 'SOL123'", "sessionInfoVersion: '3'", "version: 'v4'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("standard flow missing %q, got: %s", want, got)
		}
	}
	if strings.Contains(got, "featureType") {
		t.Fatalf("standard flow must NOT hardcode featureType, got: %s", got)
	}
}

// Coexistence opt-in (WHATSAPP_ES_FEATURE_TYPE set): featureType is present and the
// popup solutionID is dropped (the solution attach is server-side via account_sharing).
func TestEmbeddedSignupExtras_CoexistenceOptIn(t *testing.T) {
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{
		AppID: "APPID", ConfigID: "CFG", SolutionID: "SOL123",
		ESFeatureType: "whatsapp_business_app_onboarding", ESVersion: "v4",
	})
	got := h.embeddedSignupExtras()
	if !strings.Contains(got, "featureType: 'whatsapp_business_app_onboarding'") {
		t.Fatalf("coexistence must set featureType, got: %s", got)
	}
	if !strings.Contains(got, "version: 'v4'") {
		t.Fatalf("coexistence must keep version, got: %s", got)
	}
	if strings.Contains(got, "solutionID") {
		t.Fatalf("coexistence must NOT carry popup solutionID, got: %s", got)
	}
}

// esVersion defaults to v4 when unset.
func TestEmbeddedSignupExtras_VersionDefaultsV4(t *testing.T) {
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{SolutionID: "S", ESVersion: ""})
	if got := h.embeddedSignupExtras(); !strings.Contains(got, "version: 'v4'") {
		t.Fatalf("expected default version v4, got: %s", got)
	}
}

// TestParseDialog360LiveChannels_RealPayload is the regression test for the stuck-PENDING
// bug: 360dialog's real partner webhook nests the channel id at data.id (confirmed
// against their docs), not a top-level channel_id. The parser must extract it, or a
// live number never finalizes.
func TestParseDialog360LiveChannels_RealPayload(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "real 360dialog channel_live (data.id)",
			body: `{"id":"evt_1","event":"channel_live","data":{"id":"tlbN64CH","status":"ready","account_mode":"live","phone_number":"15553785635","phone_name":"Vozko Homolog TMP"}}`,
			want: []string{"tlbN64CH"},
		},
		{
			name: "channel_running via data.account_mode",
			body: `{"event":"channel_running","data":{"id":"chAB12","account_mode":"live"}}`,
			want: []string{"chAB12"},
		},
		{
			name: "array of events, only live ones extracted",
			body: `[{"event":"channel_created","data":{"id":"chNEW"}},{"event":"channel_live","data":{"id":"chLIVE"}}]`,
			want: []string{"chLIVE"},
		},
		{
			name: "legacy top-level channel_id still works (fallback)",
			body: `{"event":"channel_live","channel_id":"legacyCh"}`,
			want: []string{"legacyCh"},
		},
		{
			name: "non-live event yields nothing",
			body: `{"id":"evt_2","event":"channel_created","data":{"id":"chX","status":"pending"}}`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDialog360LiveChannels([]byte(tc.body))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
