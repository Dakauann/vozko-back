package voipinfra

import (
	"errors"
	"testing"

	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
	"vozko/infra/voip/voiptest"
)

func TestATrunkCannotAimTheEngineAtAPrivateAddress(t *testing.T) {
	provider := voiptest.StartProvider(t)
	trunk := testTrunk(provider)
	repo := siptrunktest.NewMemoryRepository(trunk)
	manager := newEngine(t, repo, &gaugeMetrics{}, func(cfg *TrunkManagerConfig) { cfg.AllowPrivateHosts = false })

	if err := manager.RegisterTrunk(trunk); !errors.Is(err, sip_trunk.ErrHostNotPublic) {
		t.Fatalf("RegisterTrunk() = %v, want ErrHostNotPublic", err)
	}
	if status, ok := manager.TrunkStatus(trunk.ID); ok && status.Status == sip_trunk.RegistrationStatusRegistered {
		t.Fatal("a trunk aimed at a private address registered")
	}
}

func TestAnOutboundProxyCannotAimTheEngineAtAPrivateAddress(t *testing.T) {
	cases := map[string]bool{
		"203.0.113.10:5060": true,
		"127.0.0.1:5060":    false,
		"169.254.169.254":   false,
		"localhost:6379":    false,
	}
	for address, want := range cases {
		err := refusePrivateHosts(t.Context(), address)
		if got := err == nil; got != want {
			t.Errorf("refusePrivateHosts(%q) = %v, want allowed=%v", address, err, want)
		}
	}
}
