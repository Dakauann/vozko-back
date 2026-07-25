package voipinfra

import (
	"log"
	"testing"

	"github.com/emiago/diago/media"
)

// ensureMediaPortRange must never leave diago on OS-ephemeral ports: it shares an
// already-set window, sets one from config when unset, and REFUSES (errors) when both
// are missing/invalid rather than silently binding ephemeral ports.
func TestEnsureMediaPortRange(t *testing.T) {
	saveStart, saveEnd := media.RTPPortStart, media.RTPPortEnd
	defer func() { media.RTPPortStart, media.RTPPortEnd = saveStart, saveEnd }()

	reg := &BranchRegistrar{
		cfg:    BranchRegistrarConfig{RTPPortStart: 16384, RTPPortEnd: 32767},
		logger: log.Default(),
	}

	// (1) Globals already set (trunk manager ran) -> share, do not overwrite.
	media.RTPPortStart, media.RTPPortEnd = 10000, 20000
	if err := reg.ensureMediaPortRange(); err != nil {
		t.Fatalf("share path errored: %v", err)
	}
	if media.RTPPortStart != 10000 || media.RTPPortEnd != 20000 {
		t.Fatalf("overwrote an already-set window: got %d-%d", media.RTPPortStart, media.RTPPortEnd)
	}

	// (2) Globals unset + valid config -> set from config.
	media.RTPPortStart, media.RTPPortEnd = 0, 0
	if err := reg.ensureMediaPortRange(); err != nil {
		t.Fatalf("set-from-config path errored: %v", err)
	}
	if media.RTPPortStart != 16384 || media.RTPPortEnd != 32767 {
		t.Fatalf("did not set globals from config: got %d-%d", media.RTPPortStart, media.RTPPortEnd)
	}

	// (3) Globals unset + invalid config -> refuse (error), never fall back to ephemeral.
	media.RTPPortStart, media.RTPPortEnd = 0, 0
	bad := &BranchRegistrar{cfg: BranchRegistrarConfig{RTPPortStart: 0, RTPPortEnd: 0}, logger: log.Default()}
	if err := bad.ensureMediaPortRange(); err == nil {
		t.Fatal("expected refusal when the RTP window is unset and config is invalid")
	}
}
