package container

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// validatePortLayout refuses to start when the port plan is unsafe. A media plane must
// be deterministic, so a misconfiguration is FATAL, not a warning: the WhatsApp media
// UDP mux binds one fixed port, and a port inside the OS ephemeral range would be
// stolen by an outbound socket and blackhole call media under load. Runs at the very
// top of container.New, before anything binds.
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

// checkPortLayout returns human-readable descriptions of every unsafe condition in the
// port plan (empty = safe). Pure, so it is unit-tested; validatePortLayout turns any
// result into a fatal refuse-to-start.
func checkPortLayout(in portLayoutInputs) []string {
	if in.mux <= 0 {
		return nil // media not configured (dev/CI); nothing to validate
	}
	var v []string
	if in.mux >= in.ephLo && in.mux <= in.ephHi {
		v = append(v, fmt.Sprintf("WhatsApp media mux port %d sits inside the OS ephemeral range %d-%d; an outbound socket could take it first and blackhole call media (pick a port below %d)", in.mux, in.ephLo, in.ephHi, in.ephLo))
	}
	return v
}

// osEphemeralRange returns the OS local/ephemeral port range so the media mux can be
// kept clear of it. Linux: read the kernel setting; elsewhere (Windows/dev) fall back
// to the platform default dynamic range, so the homolog validates against its own
// reality and a Linux box against the kernel's.
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
