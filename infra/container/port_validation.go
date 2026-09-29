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
		sip:   portRange{lo: c.cfg.SIP.PortStart, hi: c.cfg.SIP.PortStart + c.cfg.SIP.PortCount - 1},
		rtp:   portRange{lo: c.cfg.SIP.RTPPortStart, hi: c.cfg.SIP.RTPPortEnd},
		ephLo: eLo,
		ephHi: eHi,
	}
	if violations := checkPortLayout(in); len(violations) > 0 {
		for _, v := range violations {
			log.Printf("[port-config] violation: %s", v)
		}
		log.Fatalf("[port-config] REFUSING TO START: %d unsafe port-layout violation(s) above, fix WHATSAPP_MEDIA_UDP_MUX_PORT or the SIP_* port ranges", len(violations))
	}
	log.Printf("[port-config] OK: WhatsApp media mux %d, SIP %d-%d, RTP %d-%d (outside OS ephemeral %d-%d)", in.mux, in.sip.lo, in.sip.hi, in.rtp.lo, in.rtp.hi, eLo, eHi)
}

type portRange struct {
	lo, hi int
}

func (r portRange) set() bool {
	return r.lo > 0 && r.hi >= r.lo
}

func (r portRange) contains(port int) bool {
	return r.set() && port >= r.lo && port <= r.hi
}

func (r portRange) overlaps(lo, hi int) bool {
	return r.set() && r.lo <= hi && lo <= r.hi
}

type portLayoutInputs struct {
	mux          int
	sip          portRange
	rtp          portRange
	ephLo, ephHi int
}

func checkPortLayout(in portLayoutInputs) []string {
	var v []string
	if in.mux > 0 && in.mux >= in.ephLo && in.mux <= in.ephHi {
		v = append(v, fmt.Sprintf("WhatsApp media mux port %d sits inside the OS ephemeral range %d-%d; an outbound socket could take it first and blackhole call media (pick a port below %d)", in.mux, in.ephLo, in.ephHi, in.ephLo))
	}
	for _, named := range []struct {
		name string
		r    portRange
	}{{"SIP signalling ports", in.sip}, {"SIP RTP ports", in.rtp}} {
		if named.r.overlaps(in.ephLo, in.ephHi) {
			v = append(v, fmt.Sprintf("%s %d-%d overlap the OS ephemeral range %d-%d; outbound sockets could take them and break calls (keep them below %d)", named.name, named.r.lo, named.r.hi, in.ephLo, in.ephHi, in.ephLo))
		}
		if in.mux > 0 && named.r.contains(in.mux) {
			v = append(v, fmt.Sprintf("WhatsApp media mux port %d sits inside the %s %d-%d", in.mux, named.name, named.r.lo, named.r.hi))
		}
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
