package media

import "testing"

func TestEnableSharedUDPMux(t *testing.T) {
	t.Cleanup(func() { sharedUDPMux.Store(nil) })

	if _, ok := loadSharedUDPMux(); ok {
		t.Fatal("mux should be unset before EnableSharedUDPMux")
	}

	port, err := EnableSharedUDPMux(0)
	if err != nil {
		t.Fatalf("EnableSharedUDPMux(0): %v", err)
	}
	if port <= 0 {
		t.Fatalf("bound port = %d, want > 0", port)
	}
	if mux, ok := loadSharedUDPMux(); !ok || mux == nil {
		t.Fatalf("loadSharedUDPMux() = (%v,%v), want non-nil,true", mux, ok)
	}
}

func TestSharedUDPMux_ManySessionsOnePort(t *testing.T) {
	if testing.Short() {
		t.Skip("creates real PeerConnections")
	}
	t.Cleanup(func() { sharedUDPMux.Store(nil) })

	if _, err := EnableSharedUDPMux(0); err != nil {
		t.Fatalf("EnableSharedUDPMux: %v", err)
	}

	const n = 25
	sessions := make([]*Session, 0, n)
	for i := 0; i < n; i++ {
		s, err := NewSession("203.0.113.1", []string{})
		if err != nil {
			t.Fatalf("session %d on shared mux: %v", i, err)
		}
		if s.Offer() == "" {
			t.Fatalf("session %d produced empty offer", i)
		}
		sessions = append(sessions, s)
	}
	for _, s := range sessions {
		_ = s.Close()
	}
}
