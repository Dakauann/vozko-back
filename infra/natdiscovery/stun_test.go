package natdiscovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func startSTUNResponder(t *testing.T, mapped *net.UDPAddr) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var req stun.Message
			if err := stun.Decode(buf[:n], &req); err != nil {
				continue
			}
			reflected := mapped
			if reflected == nil {
				reflected = from
			}
			res := stun.MustBuild(stun.NewTransactionIDSetter(req.TransactionID), stun.BindingSuccess,
				&stun.XORMappedAddress{IP: reflected.IP, Port: reflected.Port}, stun.Fingerprint)
			_, _ = conn.WriteToUDP(res.Raw, from)
		}
	}()
	return "stun:" + conn.LocalAddr().String()
}

func TestMappedAddressReturnsTheReflectedEndpoint(t *testing.T) {
	server := startSTUNResponder(t, &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 40000})
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	got, err := MappedAddress(context.Background(), conn, []string{server})
	if err != nil {
		t.Fatalf("MappedAddress() error = %v", err)
	}
	if !got.IP.Equal(net.IPv4(203, 0, 113, 7)) || got.Port != 40000 {
		t.Fatalf("MappedAddress() = %v, want 203.0.113.7:40000", got)
	}
}

func TestMappedAddressFallsThroughToTheNextServer(t *testing.T) {
	dead, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.LocalAddr().String()
	dead.Close()
	server := startSTUNResponder(t, nil)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := MappedAddress(ctx, conn, []string{"stun:" + deadAddr, server})
	if err != nil {
		t.Fatalf("MappedAddress() error = %v", err)
	}
	if got.Port != conn.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("MappedAddress() port = %d, want the probing socket port %d", got.Port, conn.LocalAddr().(*net.UDPAddr).Port)
	}
}

func TestMappedAddressFailsWhenNoServerAnswers(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := MappedAddress(ctx, conn, []string{"stun:127.0.0.1:9"}); !errors.Is(err, ErrNoMappedAddress) {
		t.Fatalf("MappedAddress() error = %v, want ErrNoMappedAddress", err)
	}
}

func TestPublicEndpointBindsTheRequestedPort(t *testing.T) {
	server := startSTUNResponder(t, nil)
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	got, err := PublicEndpoint(context.Background(), "127.0.0.1", port, []string{server})
	if err != nil {
		t.Fatalf("PublicEndpoint() error = %v", err)
	}
	if got.Port != port {
		t.Fatalf("PublicEndpoint() port = %d, want %d", got.Port, port)
	}
}

func TestOutboundIPReturnsTheRoutingInterface(t *testing.T) {
	ip, err := OutboundIP("127.0.0.1:5060")
	if err != nil {
		t.Fatalf("OutboundIP() error = %v", err)
	}
	if !ip.IsLoopback() {
		t.Fatalf("OutboundIP() = %v, want a loopback address for a loopback target", ip)
	}
}
