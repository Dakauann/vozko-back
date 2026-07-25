package voipinfra

import (
	"testing"

	"vozko/domain/sip_trunk"
)

// The default SDP offer (no per-trunk override) must advertise only G.711
// codecs (no removed G.722/Opus), PCMA-first for Brazil's A-law-native PSTN.
// This is safe because every channel encodes with the session's negotiated
// codec. See docs/SIP_AUDIO_PIPELINE.md §8.4 / §9.1 / §10 (P1).
func TestResolveCodecList_DefaultProfileIsPCMAFirstG711Only(t *testing.T) {
	tr := baseTrunk() // no Codecs set -> system default profile
	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)

	got := make([]string, 0, len(cfg.Codecs))
	for _, c := range cfg.Codecs {
		got = append(got, c.Name)
	}

	want := []string{CodecPCMA.Name, CodecPCMU.Name, CodecTelephoneEvent.Name}
	if len(got) != len(want) {
		t.Fatalf("default profile = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default profile order = %v, want %v", got, want)
		}
	}

	// G.722 / Opus must NOT be advertised — they were removed from the media
	// plane (a carrier selecting one would yield silent/garbled audio).
	for _, c := range cfg.Codecs {
		if c.Name == "G722" || c.Name == "opus" {
			t.Fatalf("default profile must not advertise removed codec %q", c.Name)
		}
	}
}

// The codec catalog the API exposes to the trunk UI (sip_trunk.OfferableCodecs)
// must stay in sync with what the resolver can actually map onto the wire:
// every offered codec must resolve via codecByID, and none may be a removed
// G.722/Opus. This guards against the UI offering a codec the media plane drops.
func TestOfferableCodecs_AllResolvableAndG711Only(t *testing.T) {
	offered := sip_trunk.OfferableCodecs()
	if len(offered) == 0 {
		t.Fatal("OfferableCodecs returned no codecs")
	}
	for _, info := range offered {
		// codecByID returns ok=false for the removed G.722/Opus, so this also
		// asserts those are never exposed by the catalog.
		if _, ok := codecByID(info.ID, CodecTelephoneEvent); !ok {
			t.Fatalf("OfferableCodecs exposes %q but codecByID cannot map it onto the wire", info.ID)
		}
	}
}
