package voipinfra

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"vozko/domain/voip"

	"github.com/emiago/diago/media"
	"github.com/pion/stun/v3"
)

const (
	stunQueryTimeout = 4 * time.Second
)

var (
	CodecPCMU = media.Codec{
		PayloadType: 0,
		SampleRate:  8000,
		SampleDur:   20 * time.Millisecond,
		NumChannels: 1,
		Name:        "PCMU",
	}

	CodecPCMA = media.Codec{
		PayloadType: 8,
		SampleRate:  8000,
		SampleDur:   20 * time.Millisecond,
		NumChannels: 1,
		Name:        "PCMA",
	}

	// NOTE: G.722 and Opus were intentionally removed. The media plane only
	// implements G.711 (µ-law/A-law) end-to-end, and Brazilian carriers
	// transcode everything to narrowband at interconnect, so wideband bought
	// nothing while risking silent/garbled calls if a carrier selected it.
	// See docs/SIP_AUDIO_PIPELINE.md §9.1.

	CodecTelephoneEvent = media.Codec{
		PayloadType: 101,
		SampleRate:  8000,
		SampleDur:   20 * time.Millisecond,
		NumChannels: 1,
		Name:        "telephone-event",
	}
)

var defaultStunServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun2.l.google.com:19302",
	"stun3.l.google.com:19302",
	"stun4.l.google.com:19302",
}

func buildMediaInfo(sess *media.MediaSession) voip.MediaInfo {
	info := voip.MediaInfo{
		LocalRTPAddr:  sess.Laddr.String(),
		RemoteRTPAddr: sess.Raddr.String(),
		Mode:          sess.Mode,
		SecureRTP:     sess.SecureRTP > 0,
	}

	if info.SecureRTP {
		info.RTPProfile = "RTP/SAVP"
	} else {
		info.RTPProfile = "RTP/AVP"
	}

	if sess.Laddr.Port > 0 {
		info.LocalRTCPAddr = fmt.Sprintf("%s:%d", sess.Laddr.IP.String(), sess.Laddr.Port+1)
	}
	if sess.Raddr.Port > 0 {
		info.RemoteRTCPAddr = fmt.Sprintf("%s:%d", sess.Raddr.IP.String(), sess.Raddr.Port+1)
	}

	negotiated := sess.CommonCodecs()
	if len(negotiated) == 0 {
		negotiated = sess.Codecs
	}

	for _, c := range negotiated {
		ci := voip.CodecInfo{
			Name:        c.Name,
			PayloadType: c.PayloadType,
			SampleRate:  c.SampleRate,
			Channels:    c.NumChannels,
			PtimeMs:     int(c.SampleDur.Milliseconds()),
		}
		info.Codecs = append(info.Codecs, ci)
	}

	for _, c := range negotiated {
		if c.Name != "telephone-event" {
			info.SelectedCodec = voip.CodecInfo{
				Name:        c.Name,
				PayloadType: c.PayloadType,
				SampleRate:  c.SampleRate,
				Channels:    c.NumChannels,
				PtimeMs:     int(c.SampleDur.Milliseconds()),
			}
			break
		}
	}

	return info
}

func detectPublicIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org?format=text", nil)
	if err != nil {
		return "", err
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}

func determineOutboundIP(registrarAddr string) (net.IP, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.Dial("udp", registrarAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("unexpected local addr type %T", conn.LocalAddr())
	}

	return udpAddr.IP, nil
}

func discoverExternalEndpoint(ctx context.Context, listenHost string, listenPort int, servers ...string) (string, int, error) {
	laddr := &net.UDPAddr{IP: net.ParseIP(listenHost), Port: listenPort}
	if laddr.IP == nil {
		resolvedIPs, err := net.LookupIP(listenHost)
		if err != nil || len(resolvedIPs) == 0 {
			laddr.IP = net.ParseIP("0.0.0.0")
		} else {
			laddr.IP = resolvedIPs[0]
		}
	}

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return "", 0, fmt.Errorf("stun bind: %w", err)
	}
	defer conn.Close()

	stunServers := servers
	if len(stunServers) == 0 {
		stunServers = defaultStunServers
	}

	buf := make([]byte, 1500)
	for _, server := range stunServers {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		default:
		}

		raddr, resolveErr := net.ResolveUDPAddr("udp", server)
		if resolveErr != nil {
			continue
		}

		deadline := time.Now().Add(stunQueryTimeout)
		if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
			deadline = dl
		}

		if err := conn.SetDeadline(deadline); err != nil {
			continue
		}

		msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
		if _, err := conn.WriteTo(msg.Raw, raddr); err != nil {
			continue
		}

		n, _, readErr := conn.ReadFrom(buf)
		if readErr != nil {
			continue
		}

		var response stun.Message
		if err := stun.Decode(buf[:n], &response); err != nil {
			continue
		}

		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(&response); err != nil {
			continue
		}

		mappedIP := xorAddr.IP.String()
		mappedPort := xorAddr.Port
		if mappedIP != "" && mappedPort > 0 {
			return mappedIP, mappedPort, nil
		}
	}

	return "", 0, fmt.Errorf("stun: no response")
}
