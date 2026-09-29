package voipinfra

import (
	"net"
	"testing"
	"time"
)

func TestCallExpiry(t *testing.T) {
	answered := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	limits := callLimits{MediaTimeout: 30 * time.Second, MaxDuration: time.Hour}
	cases := []struct {
		name       string
		now        time.Time
		lastPacket time.Time
		want       expiryReason
	}{
		{"fresh call with no media yet", answered.Add(10 * time.Second), time.Time{}, expiryNone},
		{"no media ever received", answered.Add(31 * time.Second), time.Time{}, expiryMediaTimeout},
		{"media flowing", answered.Add(10 * time.Minute), answered.Add(10*time.Minute - time.Second), expiryNone},
		{"media stopped", answered.Add(10 * time.Minute), answered.Add(9 * time.Minute), expiryMediaTimeout},
		{"maximum duration reached", answered.Add(time.Hour), answered.Add(time.Hour - time.Second), expiryMaxDuration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := limits.expiry(tc.now, answered, tc.lastPacket); got != tc.want {
				t.Fatalf("expiry() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPortAllocatorHandsOutFreePortsAndReusesReleasedOnes(t *testing.T) {
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	base := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	allocator := newPortAllocator("127.0.0.1", base, 2)
	first, err := allocator.allocate()
	if err != nil || first != base {
		t.Fatalf("first allocate() = %d, %v, want %d", first, err, base)
	}
	second, err := allocator.allocate()
	if err != nil || second != base+1 {
		t.Fatalf("second allocate() = %d, %v, want %d", second, err, base+1)
	}
	if _, err := allocator.allocate(); err == nil {
		t.Fatal("allocate() on an exhausted range = nil error")
	}
	allocator.release(first)
	if again, err := allocator.allocate(); err != nil || again != first {
		t.Fatalf("allocate() after release = %d, %v, want %d", again, err, first)
	}
}

func TestPortAllocatorSkipsPortsHeldByOtherProcesses(t *testing.T) {
	held, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	base := held.LocalAddr().(*net.UDPAddr).Port

	allocator := newPortAllocator("127.0.0.1", base, 2)
	got, err := allocator.allocate()
	if err != nil || got != base+1 {
		t.Fatalf("allocate() = %d, %v, want %d", got, err, base+1)
	}
	held.Close()
	allocator.release(got)
	if again, err := allocator.allocate(); err != nil || again != base {
		t.Fatalf("allocate() after the other process let go = %d, %v, want %d", again, err, base)
	}
}
