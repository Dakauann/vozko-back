package container

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func (c *Container) validatePortLayout() {
	eLo, eHi := osEphemeralRange()
	in := portLayoutInputs{
		mux:   c.cfg.WhatsAppMediaUDPMuxPort,
		ephLo: eLo,
		ephHi: eHi,
	}
	if violations := checkPortLayout(in); len(violations) > 0 {
		for _, v := range violations {
			log.Printf("[port-config] violation: %s", v)
		}
		log.Fatalf("[port-config] REFUSING TO START: %d unsafe port-layout violation(s) above, fix WHATSAPP_MEDIA_UDP_MUX_PORT", len(violations))
	}
	log.Printf("[port-config] OK: WhatsApp media mux %d (outside OS ephemeral %d-%d)", in.mux, eLo, eHi)
}

type portLayoutInputs struct {
	mux          int
	ephLo, ephHi int
}

func checkPortLayout(in portLayoutInputs) []string {
	if in.mux <= 0 {
		return nil
	}
	var v []string
	if in.mux >= in.ephLo && in.mux <= in.ephHi {
		v = append(v, fmt.Sprintf("WhatsApp media mux port %d sits inside the OS ephemeral range %d-%d; an outbound socket could take it first and blackhole call media (pick a port below %d)", in.mux, in.ephLo, in.ephHi, in.ephLo))
	}
	return v
}

func osEphemeralRange() (lo, hi int) {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range"); err == nil {
			if l, h, ok := parseEphemeralRange(string(data)); ok {
				return l, h
			}
		}
		return 32768, 60999
	}
	return 49152, 65535
}

func parseEphemeralRange(content string) (lo, hi int, ok bool) {
	f := strings.Fields(strings.TrimSpace(content))
	if len(f) != 2 {
		return 0, 0, false
	}
	l, e1 := strconv.Atoi(f[0])
	h, e2 := strconv.Atoi(f[1])
	if e1 != nil || e2 != nil || l <= 0 || h < l {
		return 0, 0, false
	}
	return l, h, true
}
