package media

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/zaf/g711"
)

const (
	sampleRate    = 8000
	frameSamples  = sampleRate / 50
	framePCMBytes = frameSamples * 2
	frameDuration = 20 * time.Millisecond
)

var defaultSTUNServers = []string{"stun:stun.l.google.com:19302"}

type Session struct {
	pc         *webrtc.PeerConnection
	localTrack *webrtc.TrackLocalStaticSample
	offerSDP   string
	answerSDP  string

	readCh chan []byte

	writeMu sync.Mutex
	pending []byte
	started bool

	closeOnce sync.Once
	closed    chan struct{}
}

func buildSession(publicIP string, stunServers []string) (*Session, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypePCMU,
			ClockRate: sampleRate,
			Channels:  1,
		},
		PayloadType: 0,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register pcmu codec: %w", err)
	}

	settingEngine := webrtc.SettingEngine{}
	ip := strings.TrimSpace(publicIP)
	hasPublicIP := ip != ""
	if hasPublicIP {
		settingEngine.SetNAT1To1IPs([]string{ip}, webrtc.ICECandidateTypeHost)
	}

	if mux, ok := loadSharedUDPMux(); ok {
		settingEngine.SetICEUDPMux(mux)
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithSettingEngine(settingEngine),
	)

	var iceServers []webrtc.ICEServer
	if !hasPublicIP {
		urls := stunServers
		if len(urls) == 0 {
			urls = defaultSTUNServers
		}
		iceServers = []webrtc.ICEServer{{URLs: urls}}
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	s := &Session{
		pc:     pc,
		readCh: make(chan []byte, 64),
		closed: make(chan struct{}),
	}

	localTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: sampleRate, Channels: 1},
		"audio", "whatsapp",
	)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("create local track: %w", err)
	}
	if _, err := pc.AddTrack(localTrack); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("add local track: %w", err)
	}
	s.localTrack = localTrack

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go s.readLoop(remote)
	})

	return s, nil
}

func (s *Session) gatherLocalDescription() (string, error) {
	gatherComplete := webrtc.GatheringCompletePromise(s.pc)
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):

	}
	local := s.pc.LocalDescription()
	if local == nil {
		return "", fmt.Errorf("local description unavailable after gathering")
	}
	return local.SDP, nil
}

func NewSession(publicIP string, stunServers []string) (*Session, error) {
	s, err := buildSession(publicIP, stunServers)
	if err != nil {
		return nil, err
	}
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		_ = s.pc.Close()
		return nil, fmt.Errorf("create offer: %w", err)
	}
	if err := s.pc.SetLocalDescription(offer); err != nil {
		_ = s.pc.Close()
		return nil, fmt.Errorf("set local description: %w", err)
	}
	sdp, err := s.gatherLocalDescription()
	if err != nil {
		_ = s.pc.Close()
		return nil, err
	}
	s.offerSDP = sdp
	return s, nil
}

func NewSessionAnswerer(offerSDP, publicIP string, stunServers []string) (*Session, error) {
	s, err := buildSession(publicIP, stunServers)
	if err != nil {
		return nil, err
	}
	if err := s.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		_ = s.pc.Close()
		return nil, fmt.Errorf("set remote offer: %w", err)
	}
	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		_ = s.pc.Close()
		return nil, fmt.Errorf("create answer: %w", err)
	}
	if err := s.pc.SetLocalDescription(answer); err != nil {
		_ = s.pc.Close()
		return nil, fmt.Errorf("set local description: %w", err)
	}
	sdp, err := s.gatherLocalDescription()
	if err != nil {
		_ = s.pc.Close()
		return nil, err
	}
	s.answerSDP = sdp
	s.writeMu.Lock()
	s.started = true
	s.writeMu.Unlock()
	return s, nil
}

func (s *Session) Offer() string { return s.offerSDP }

func (s *Session) Answer() string { return s.answerSDP }

func (s *Session) Start(answerSDP string) error {
	if err := s.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}
	s.writeMu.Lock()
	s.started = true
	s.writeMu.Unlock()
	return nil
}

func (s *Session) ReadChan() <-chan []byte { return s.readCh }

func (s *Session) WritePCM(pcm16 []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if !s.started {
		return nil
	}

	buf := pcm16
	if len(s.pending) > 0 {
		buf = append(s.pending, pcm16...)
		s.pending = nil
	}

	off := 0
	for ; off+framePCMBytes <= len(buf); off += framePCMBytes {
		ulaw := g711.EncodeUlaw(buf[off : off+framePCMBytes])
		if err := s.localTrack.WriteSample(media.Sample{Data: ulaw, Duration: frameDuration}); err != nil {
			s.pending = append([]byte(nil), buf[off:]...)
			return fmt.Errorf("write sample: %w", err)
		}
	}
	if off < len(buf) {
		s.pending = append([]byte(nil), buf[off:]...)
	}
	return nil
}

func (s *Session) readLoop(remote *webrtc.TrackRemote) {
	for {
		select {
		case <-s.closed:
			return
		default:
		}
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		pcm := g711.DecodeUlaw(pkt.Payload)
		select {
		case s.readCh <- pcm:
		default:
		}
	}
}

func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.pc.Close()
		close(s.readCh)
	})
	return nil
}
