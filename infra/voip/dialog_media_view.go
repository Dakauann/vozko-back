package voipinfra

import (
	"fmt"
	"net"
	"sync"

	"github.com/emiago/diago/media"
	"github.com/pion/rtp"

	"vozko/domain/voip"
)

const (
	telephoneEventCodec = "telephone-event"
	dtmfEventChars      = "0123456789*#ABCD"
	dtmfPayloadSize     = 4
	dtmfEndOfEventFlag  = 0x80
)

type dialogMediaView struct {
	current func() *media.MediaSession

	dtmfMu      sync.RWMutex
	dtmfHandler voip.DTMFHandler

	dtmfLastTimestamp uint32
	dtmfLastFired     bool
}

func newDialogMediaView(current func() *media.MediaSession) *dialogMediaView {
	return &dialogMediaView{current: current}
}

func (a *dialogMediaView) ReadRTP(buf []byte, packet any) (int, error) {
	dst, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, fmt.Errorf("media adapter: packet must be *rtp.Packet, got %T", packet)
	}
	for {
		session := a.current()
		n, err := session.ReadRTP(buf, dst)
		if err != nil {
			return n, err
		}
		if pt, ok := dtmfPayloadType(session); ok && dst.PayloadType == pt {
			a.handleDTMF(dst)
			continue
		}
		return n, nil
	}
}

func (a *dialogMediaView) handleDTMF(p *rtp.Packet) {
	if len(p.Payload) < dtmfPayloadSize {
		return
	}
	if p.Timestamp != a.dtmfLastTimestamp {
		a.dtmfLastTimestamp = p.Timestamp
		a.dtmfLastFired = false
	}
	event := p.Payload[0]
	endOfEvent := p.Payload[1]&dtmfEndOfEventFlag != 0
	if !endOfEvent || a.dtmfLastFired || int(event) >= len(dtmfEventChars) {
		return
	}
	a.dtmfLastFired = true
	a.dtmfMu.RLock()
	handler := a.dtmfHandler
	a.dtmfMu.RUnlock()
	if handler != nil {
		handler(rune(dtmfEventChars[event]))
	}
}

func (a *dialogMediaView) OnDTMF(handler voip.DTMFHandler) {
	a.dtmfMu.Lock()
	a.dtmfHandler = handler
	a.dtmfMu.Unlock()
}

func (a *dialogMediaView) WriteRTP(packet any) error {
	p, ok := packet.(*rtp.Packet)
	if !ok {
		return fmt.Errorf("media adapter: packet must be *rtp.Packet, got %T", packet)
	}
	return a.current().WriteRTP(p)
}

func (a *dialogMediaView) LocalAddr() net.Addr {
	laddr := a.current().Laddr
	return &laddr
}

func (a *dialogMediaView) RemoteAddr() net.Addr {
	raddr := a.current().Raddr
	return &raddr
}

func (a *dialogMediaView) Close() error {
	return nil
}

func (a *dialogMediaView) NegotiatedCodec() voip.CodecInfo {
	codec, _ := negotiatedVoiceCodec(a.current())
	return codec
}

func negotiatedCodecs(session *media.MediaSession) []media.Codec {
	if common := session.CommonCodecs(); len(common) > 0 {
		return common
	}
	return session.Codecs
}

func negotiatedVoiceCodec(session *media.MediaSession) (voip.CodecInfo, bool) {
	for _, c := range negotiatedCodecs(session) {
		if c.Name == telephoneEventCodec {
			continue
		}
		return voip.CodecInfo{
			Name:        c.Name,
			PayloadType: c.PayloadType,
			SampleRate:  c.SampleRate,
			Channels:    c.NumChannels,
			PtimeMs:     int(c.SampleDur.Milliseconds()),
		}, true
	}
	return voip.CodecInfo{}, false
}

func dtmfPayloadType(session *media.MediaSession) (uint8, bool) {
	for _, c := range negotiatedCodecs(session) {
		if c.Name == telephoneEventCodec {
			return c.PayloadType, true
		}
	}
	return 0, false
}

func buildMediaInfo(session *media.MediaSession) voip.MediaInfo {
	codec, _ := negotiatedVoiceCodec(session)
	dtmfPT, _ := dtmfPayloadType(session)
	encrypted, _ := sessionEncrypted(session)
	return voip.MediaInfo{Codec: codec, DTMFPayloadType: dtmfPT, Encrypted: encrypted}
}
