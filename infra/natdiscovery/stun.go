package natdiscovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

const perServerTimeout = 3 * time.Second

var DefaultSTUNServers = []string{"stun:stun.l.google.com:19302", "stun:stun.cloudflare.com:3478"}

var ErrNoMappedAddress = errors.New("stun: no server returned a mapped address")

func MappedAddress(ctx context.Context, conn net.PacketConn, servers []string) (*net.UDPAddr, error) {
	if len(servers) == 0 {
		servers = DefaultSTUNServers
	}
	defer conn.SetDeadline(time.Time{})
	buf := make([]byte, 1500)
	for _, server := range servers {
		if ctx.Err() != nil {
			break
		}
		target, err := net.ResolveUDPAddr("udp4", strings.TrimPrefix(strings.TrimSpace(server), "stun:"))
		if err != nil {
			continue
		}
		if mapped, err := query(ctx, conn, target, buf); err == nil {
			return mapped, nil
		}
	}
	return nil, ErrNoMappedAddress
}

func query(ctx context.Context, conn net.PacketConn, target *net.UDPAddr, buf []byte) (*net.UDPAddr, error) {
	deadline := time.Now().Add(perServerTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	if _, err := conn.WriteTo(request.Raw, target); err != nil {
		return nil, err
	}
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return nil, err
		}
		if from.String() != target.String() {
			continue
		}
		var response stun.Message
		if err := stun.Decode(buf[:n], &response); err != nil || response.TransactionID != request.TransactionID {
			continue
		}
		var mapped stun.XORMappedAddress
		if err := mapped.GetFrom(&response); err != nil {
			return nil, err
		}
		return &net.UDPAddr{IP: mapped.IP, Port: mapped.Port}, nil
	}
}

func PublicIP(ctx context.Context, servers []string) (string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return "", fmt.Errorf("stun bind: %w", err)
	}
	defer conn.Close()
	mapped, err := MappedAddress(ctx, conn, servers)
	if err != nil {
		return "", err
	}
	return mapped.IP.String(), nil
}

func PublicEndpoint(ctx context.Context, bindHost string, port int, servers []string) (*net.UDPAddr, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(bindHost), Port: port})
	if err != nil {
		return nil, fmt.Errorf("stun bind %s:%d: %w", bindHost, port, err)
	}
	defer conn.Close()
	return MappedAddress(ctx, conn, servers)
}

func OutboundIP(target string) (net.IP, error) {
	conn, err := net.Dial("udp4", target)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}
