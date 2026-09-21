package metaembeddedsignup

import (
	"strings"
	"testing"
)

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

func TestEmbeddedSignupExtras_VersionDefaultsV4(t *testing.T) {
	h := NewMetaEmbeddedSignupHandler(MetaEmbeddedSignupConfig{SolutionID: "S", ESVersion: ""})
	if got := h.embeddedSignupExtras(); !strings.Contains(got, "version: 'v4'") {
		t.Fatalf("expected default version v4, got: %s", got)
	}
}

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
