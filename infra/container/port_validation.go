package container

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"

	wamedia "vozko/infra/conversation/whatsapp/media"
)

// validatePortLayout refuses to start when the port plan is unsafe. A media plane must
// be deterministic, so a misconfiguration is FATAL, not a warning: an RTP window that
// overlaps the OS ephemeral range (outbound sockets, Redis/DB/STUN/HTTP, would steal
// RTP ports and blackhole media under load), SIP signaling overlapping RTP, an odd RTP
// start (breaks RFC 3550 even-RTP / odd-RTCP pairing), or a fixed listener sitting
// inside the RTP window. Runs at the very top of container.New, before anything binds.
func (c *Container) validatePortLayout() {
	eLo, eHi := osEphemeralRange()
	in := portLayoutInputs{
		rtpStart:  c.cfg.SIPTrunkRTPPortStart,
		rtpEnd:    c.cfg.SIPTrunkRTPPortEnd,
		sipStart:  c.cfg.SIPTrunkPortStart,
		sipCount:  c.cfg.SIPTrunkPortCount,
		branchSIP: c.cfg.BranchSIPListenPort,
		mux:       c.cfg.WhatsAppMediaUDPMuxPort,
		ephLo:     eLo,
		ephHi:     eHi,
	}
	if violations := checkPortLayout(in); len(violations) > 0 {
		for _, v := range violations {
			log.Printf("[port-config] violation: %s", v)
		}
		log.Fatalf("[port-config] REFUSING TO START: %d unsafe port-layout violation(s) above, fix SIP_TRUNK_RTP_PORT_* / SIP_TRUNK_PORT_* / BRANCH_SIP_LISTEN_PORT / WHATSAPP_MEDIA_UDP_MUX_PORT", len(violations))
	}
	log.Printf("[port-config] OK: RTP %d-%d (%d RTP/RTCP pairs, below OS ephemeral %d-%d), SIP trunk %d-%d, branch SIP %d, mux %d",
		in.rtpStart, in.rtpEnd, (in.rtpEnd-in.rtpStart)/2, eLo, eHi, in.sipStart, in.sipStart+in.sipCount-1, in.branchSIP, in.mux)
}

type portLayoutInputs struct {
	rtpStart, rtpEnd   int
	sipStart, sipCount int
	branchSIP, mux     int
	ephLo, ephHi       int
}

// checkPortLayout returns human-readable descriptions of every unsafe condition in the
// port plan (empty = safe). Pure, so it is unit-tested; validatePortLayout turns any
// result into a fatal refuse-to-start.
func checkPortLayout(in portLayoutInputs) []string {
	if in.rtpStart <= 0 || in.rtpEnd <= 0 {
		return nil // media not configured (dev/CI); nothing to validate
	}
	var v []string
	if in.rtpStart%2 != 0 {
		v = append(v, fmt.Sprintf("RTP start %d is odd; RFC 3550 needs an even RTP port (RTCP = RTP+1)", in.rtpStart))
	}
	if wamedia.PortRangeOverlaps(in.rtpStart, in.rtpEnd, in.ephLo, in.ephHi) {
		v = append(v, fmt.Sprintf("RTP window %d-%d overlaps the OS ephemeral range %d-%d; outbound sockets would steal RTP ports and blackhole media (move RTP below %d, e.g. 16384-32767)", in.rtpStart, in.rtpEnd, in.ephLo, in.ephHi, in.ephLo))
	}
	if in.sipCount > 0 {
		sipEnd := in.sipStart + in.sipCount - 1
		if wamedia.PortRangeOverlaps(in.sipStart, sipEnd, in.rtpStart, in.rtpEnd) {
			v = append(v, fmt.Sprintf("SIP trunk range %d-%d overlaps the RTP window %d-%d", in.sipStart, sipEnd, in.rtpStart, in.rtpEnd))
		}
	}
	for _, l := range []struct {
		name string
		port int
	}{{"branch SIP listener", in.branchSIP}, {"WhatsApp media mux", in.mux}} {
		if l.port > 0 && l.port >= in.rtpStart && l.port <= in.rtpEnd {
			v = append(v, fmt.Sprintf("%s port %d sits inside the RTP window %d-%d", l.name, l.port, in.rtpStart, in.rtpEnd))
		}
	}
	return v
}

// osEphemeralRange returns the OS local/ephemeral port range so RTP can be kept clear of
// it. Linux: read the kernel setting; elsewhere (Windows/dev) fall back to the platform
// default dynamic range, so the homolog validates against its own reality and a Linux
// box against the kernel's.
func osEphemeralRange() (lo, hi int) {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range"); err == nil {
			if l, h, ok := parseEphemeralRange(string(data)); ok {
				return l, h
			}
		}
		return 32768, 60999 // Linux kernel default when /proc is unreadable
	}
	return 49152, 65535 // Windows / other dynamic default (IANA range)
}

// parseEphemeralRange parses the two whitespace-separated ints in
// /proc/sys/net/ipv4/ip_local_port_range (e.g. "32768\t60999\n").
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
