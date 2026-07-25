package media

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

func DiscoverPublicIP(stunServers []string) (string, error) {
	servers := stunServers
	if len(servers) == 0 {
		servers = defaultSTUNServers
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return "", fmt.Errorf("stun bind: %w", err)
	}
	defer conn.Close()

	buf := make([]byte, 1500)
	for _, server := range servers {
		host := strings.TrimPrefix(strings.TrimSpace(server), "stun:")
		raddr, err := net.ResolveUDPAddr("udp4", host)
		if err != nil {
			continue
		}

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
		if _, err := conn.WriteTo(msg.Raw, raddr); err != nil {
			continue
		}
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		var resp stun.Message
		if err := stun.Decode(buf[:n], &resp); err != nil {
			continue
		}
		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(&resp); err != nil {
			continue
		}
		if ip := xorAddr.IP.String(); ip != "" && ip != "<nil>" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("stun: no public IP discovered")
}
