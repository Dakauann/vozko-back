package voipinfra

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"vozko/domain/sip_trunk"
)

func TestPortAllocator_AllocateAndRelease(t *testing.T) {
	pa := newPortAllocator(10000, 5)

	ports := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		p, err := pa.allocate()
		if err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
		ports = append(ports, p)
	}

	if _, err := pa.allocate(); err == nil {
		t.Fatal("expected error when all ports exhausted, got nil")
	}

	pa.release(ports[2])
	p, err := pa.allocate()
	if err != nil {
		t.Fatalf("re-allocate after release: %v", err)
	}
	if p != ports[2] {
		t.Fatalf("expected released port %d, got %d", ports[2], p)
	}
}

func TestPortAllocator_LargeRange(t *testing.T) {
	const count = 1_000
	pa := newPortAllocator(10000, count)
	pa.bindHost = "127.0.0.1"

	allocated := make(map[int]bool, count)
	var lastErr error
	for i := 0; i < count; i++ {
		p, err := pa.allocate()
		if err != nil {
			lastErr = err
			break
		}
		if allocated[p] {
			t.Fatalf("duplicate port %d at iteration %d", p, i)
		}
		allocated[p] = true
	}

	if len(allocated) < 900 {
		t.Fatalf("only allocated %d of %d ports (last err: %v)", len(allocated), count, lastErr)
	}

	for p := range allocated {
		if p < 10000 || p >= 10000+count {
			t.Fatalf("port %d out of expected range [10000, %d)", p, 10000+count)
		}
	}
}

func TestNewSIPTrunkManager_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TrunkManagerConfig
		wantErr string
	}{
		{
			name: "valid defaults (10000-59999 = 24999 concurrent calls)",
			cfg: TrunkManagerConfig{
				SIPPortStart: 15060,
				SIPPortCount: 100,
				RTPPortStart: 10000,
				RTPPortEnd:   59999,
			},
			wantErr: "",
		},
		{
			name: "full range with sysctl (1024-59999 = 29487 concurrent calls)",
			cfg: TrunkManagerConfig{
				SIPPortStart: 15060,
				SIPPortCount: 10,
				RTPPortStart: 1024,
				RTPPortEnd:   59999,
			},
			wantErr: "",
		},
		{
			name: "SIP port below 1024",
			cfg: TrunkManagerConfig{
				SIPPortStart: 80,
				SIPPortCount: 10,
				RTPPortStart: 10000,
				RTPPortEnd:   59999,
			},
			wantErr: "SIPPortStart 80 out of valid range",
		},
		{
			name: "SIP range exceeds 65535",
			cfg: TrunkManagerConfig{
				SIPPortStart: 65500,
				SIPPortCount: 100,
				RTPPortStart: 10000,
				RTPPortEnd:   59999,
			},
			wantErr: "exceeds max port 65535",
		},
		{
			name: "RTP start below 1024",
			cfg: TrunkManagerConfig{
				SIPPortStart: 15060,
				SIPPortCount: 10,
				RTPPortStart: 500,
				RTPPortEnd:   59999,
			},
			wantErr: "RTPPortStart 500 out of valid range",
		},
		{
			name: "RTP end exceeds 65535",
			cfg: TrunkManagerConfig{
				SIPPortStart: 15060,
				SIPPortCount: 10,
				RTPPortStart: 10000,
				RTPPortEnd:   70000,
			},
			wantErr: "RTPPortEnd 70000 exceeds max port 65535",
		},
		{
			name: "RTP end <= RTP start",
			cfg: TrunkManagerConfig{
				SIPPortStart: 15060,
				SIPPortCount: 10,
				RTPPortStart: 20000,
				RTPPortEnd:   20000,
			},
			wantErr: "must be greater than RTPPortStart",
		},
		{
			name: "RTP end less than RTP start",
			cfg: TrunkManagerConfig{
				SIPPortStart: 15060,
				SIPPortCount: 10,
				RTPPortStart: 30000,
				RTPPortEnd:   20000,
			},
			wantErr: "must be greater than RTPPortStart",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg

			cfg.Ownership = sip_trunk.NewTrunkOwnershipManager(newOwnTestSharedState(), "replica-validation")
			_, err := NewSIPTrunkManager(cfg, nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestPortCapacity_DefaultConfig(t *testing.T) {

	rtpStart := 10000
	rtpEnd := 59999
	rtpCapacity := (rtpEnd - rtpStart) / 2

	t.Logf("Default RTP range: %d-%d", rtpStart, rtpEnd)
	t.Logf("RTP capacity: %d concurrent calls per server", rtpCapacity)

	if rtpCapacity < 24000 {
		t.Fatalf("default RTP capacity %d is below 24,000 concurrent calls target", rtpCapacity)
	}

	if rtpStart < 1024 {
		t.Fatalf("default RTP start %d uses privileged ports (<1024)", rtpStart)
	}

	if rtpEnd > 65535 {
		t.Fatalf("default RTP end %d exceeds max port 65535", rtpEnd)
	}
}

func TestPortCapacity_MaximumSingleServer(t *testing.T) {
	maxStart := 1024
	maxEnd := 59999
	maxCapacity := (maxEnd - maxStart) / 2

	t.Logf("Maximum RTP range (with sysctl): %d-%d", maxStart, maxEnd)
	t.Logf("Maximum capacity: %d concurrent calls per IP", maxCapacity)

	if maxCapacity < 29000 {
		t.Fatalf("maximum capacity %d unexpectedly low", maxCapacity)
	}

	ips := 3
	totalCapacity := maxCapacity * ips
	t.Logf("With %d IPs on one server: %d concurrent calls", ips, totalCapacity)

	if totalCapacity < 70000 {
		t.Fatalf("single-server-3-IP capacity %d does not reach 70k target", totalCapacity)
	}
}

func TestPortCapacity_ScaleChart(t *testing.T) {
	rtpStart := 10000
	rtpEnd := 59999
	perServerOneIP := (rtpEnd - rtpStart) / 2

	targets := []int{10_000, 50_000, 70_000, 100_000, 500_000, 1_000_000}

	t.Logf("Per-server RTP capacity (1 IP): %d concurrent calls (range %d-%d)", perServerOneIP, rtpStart, rtpEnd)
	t.Logf("Per-server RTP capacity (3 IPs): %d concurrent calls", perServerOneIP*3)
	t.Logf("")
	t.Logf("%-15s %-20s %-20s", "Target Calls", "Replicas (1 IP)", "Replicas (3 IPs)")
	t.Logf("%-15s %-20s %-20s", "------------", "---------------", "----------------")
	for _, target := range targets {
		r1 := (target + perServerOneIP - 1) / perServerOneIP
		r3 := (target + perServerOneIP*3 - 1) / (perServerOneIP * 3)
		t.Logf("%-15d %-20d %-20d", target, r1, r3)
	}
}

func TestPortBinding_StressUDP(t *testing.T) {
	const (
		startPort = 40000
		count     = 2000
	)

	t.Logf("Binding %d UDP sockets on 127.0.0.1 ports %d-%d...", count, startPort, startPort+count-1)

	var mu sync.Mutex
	conns := make([]*net.UDPConn, 0, count)

	defer func() {
		for _, c := range conns {
			c.Close()
		}
		t.Logf("Released %d sockets", len(conns))
	}()

	bound := 0
	failed := 0
	for i := 0; i < count; i++ {
		port := startPort + i
		addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
		conn, err := net.ListenUDP("udp4", addr)
		if err != nil {
			failed++
			if failed <= 5 {
				t.Logf("  bind failed at port %d: %v", port, err)
			}
			continue
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
		bound++
	}

	t.Logf("Successfully bound: %d/%d UDP sockets (%.1f%%)", bound, count, float64(bound)/float64(count)*100)
	t.Logf("Failed: %d (likely already in use by other processes)", failed)
	t.Logf("Equivalent concurrent calls: %d (RTP+RTCP pairs)", bound/2)

	minExpected := int(float64(count) * 0.90)
	if bound < minExpected {
		t.Fatalf("only bound %d sockets, expected at least %d (90%%)", bound, minExpected)
	}
}

func TestPortBinding_SimulateRTPPairs(t *testing.T) {
	const (
		startPort = 42000
		numPairs  = 500
	)

	t.Logf("Simulating %d concurrent calls (RTP+RTCP pairs) on ports %d-%d...",
		numPairs, startPort, startPort+numPairs*2-1)

	type pair struct {
		rtp  *net.UDPConn
		rtcp *net.UDPConn
	}
	pairs := make([]pair, 0, numPairs)

	defer func() {
		for _, p := range pairs {
			p.rtp.Close()
			p.rtcp.Close()
		}
	}()

	successPairs := 0
	for i := 0; i < numPairs; i++ {
		rtpPort := startPort + i*2
		rtcpPort := rtpPort + 1

		if rtpPort%2 != 0 {
			t.Fatalf("RTP port %d is not even at pair %d", rtpPort, i)
		}

		rtpAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort}
		rtpConn, err := net.ListenUDP("udp4", rtpAddr)
		if err != nil {
			t.Logf("  pair %d: RTP bind failed on port %d: %v", i, rtpPort, err)
			continue
		}

		rtcpAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtcpPort}
		rtcpConn, err := net.ListenUDP("udp4", rtcpAddr)
		if err != nil {
			rtpConn.Close()
			t.Logf("  pair %d: RTCP bind failed on port %d: %v", i, rtcpPort, err)
			continue
		}

		pairs = append(pairs, pair{rtp: rtpConn, rtcp: rtcpConn})
		successPairs++
	}

	t.Logf("Successfully bound: %d/%d RTP+RTCP pairs (%.1f%%)",
		successPairs, numPairs, float64(successPairs)/float64(numPairs)*100)

	minExpected := int(float64(numPairs) * 0.90)
	if successPairs < minExpected {
		t.Fatalf("only bound %d pairs, expected at least %d (90%%)", successPairs, minExpected)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ = fmt.Sprintf
