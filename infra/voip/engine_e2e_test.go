package voipinfra

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"

	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
	"vozko/domain/voip"
	"vozko/infra/voip/voiptest"
)

const eventually = 5 * time.Second

type gaugeMetrics struct {
	dialing  atomic.Int64
	ongoing  atomic.Int64
	finished atomic.Int64
}

func (g *gaugeMetrics) IncCallDialing()  { g.dialing.Add(1) }
func (g *gaugeMetrics) DecCallDialing()  { g.dialing.Add(-1) }
func (g *gaugeMetrics) IncCallOngoing()  { g.ongoing.Add(1) }
func (g *gaugeMetrics) DecCallOngoing()  { g.ongoing.Add(-1) }
func (g *gaugeMetrics) IncCallFinished() { g.finished.Add(1) }

type engineFixture struct {
	manager  *SIPTrunkManager
	repo     *siptrunktest.MemoryRepository
	metrics  *gaugeMetrics
	provider *voiptest.Provider
	trunk    *sip_trunk.SIPTrunk
}

func testTrunk(provider *voiptest.Provider) *sip_trunk.SIPTrunk {
	return &sip_trunk.SIPTrunk{
		ID:          "trunk-1",
		WorkspaceID: "ws-1",
		Name:        "Test",
		TrunkType:   sip_trunk.TrunkTypeBidirectional,
		Host:        "127.0.0.1",
		Port:        provider.Port,
		Transport:   sip_trunk.TransportUDP,
		Username:    voiptest.Username,
		Password:    voiptest.Password,
		Enabled:     true,
		Settings: sip_trunk.Settings{
			BindHost:      "127.0.0.1",
			PublicAddress: "127.0.0.1",
		},
	}
}

func newEngine(t *testing.T, repo *siptrunktest.MemoryRepository, metrics *gaugeMetrics, tune func(*TrunkManagerConfig)) *SIPTrunkManager {
	t.Helper()
	cfg := TrunkManagerConfig{
		SIPBindHost:     "127.0.0.1",
		SIPPortStart:    voiptest.FreeUDPPort(t),
		SIPPortCount:    1,
		RTPPortStart:    41000,
		RTPPortEnd:      41999,
		RegisterExpiry:  60 * time.Second,
		DialTimeout:     5 * time.Second,
		MediaTimeout:    10 * time.Second,
		MaxCallDuration: time.Hour,
		WatchInterval:   50 * time.Millisecond,
		PublicAddress:   "127.0.0.1",
		UserAgent:       "VozkoTest",
		CallMetrics:     metrics,
	}
	if tune != nil {
		tune(&cfg)
	}
	manager, err := NewSIPTrunkManager(cfg, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop() })
	return manager
}

func startEngine(t *testing.T, tune func(*TrunkManagerConfig)) engineFixture {
	t.Helper()
	provider := voiptest.StartProvider(t)
	trunk := testTrunk(provider)
	repo := siptrunktest.NewMemoryRepository(trunk)
	metrics := &gaugeMetrics{}
	manager := newEngine(t, repo, metrics, tune)
	if err := manager.RegisterTrunk(trunk); err != nil {
		t.Fatalf("RegisterTrunk() error = %v", err)
	}
	f := engineFixture{manager: manager, repo: repo, metrics: metrics, provider: provider, trunk: trunk}
	f.waitStatus(t, sip_trunk.RegistrationStatusRegistered)
	return f
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(eventually)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f engineFixture) waitStatus(t *testing.T, want sip_trunk.RegistrationStatus) {
	t.Helper()
	waitFor(t, "status "+string(want), func() bool {
		status, ok := f.manager.TrunkStatus(f.trunk.ID)
		return ok && status.Status == want
	})
}

func (f engineFixture) waitNoCalls(t *testing.T) {
	t.Helper()
	waitFor(t, "no active calls", func() bool { return len(f.manager.ActiveCalls(f.trunk.ID)) == 0 })
	waitFor(t, "call gauges back to zero", func() bool {
		return f.metrics.dialing.Load() == 0 && f.metrics.ongoing.Load() == 0
	})
}

func sendFrame(t *testing.T, audio voip.PCMStream) {
	t.Helper()
	if err := audio.WritePCM(make([]byte, 320)); err != nil {
		t.Fatalf("WritePCM() error = %v", err)
	}
}

func readFrame(t *testing.T, audio voip.PCMStream) []byte {
	t.Helper()
	select {
	case frame, ok := <-audio.Frames():
		if !ok {
			t.Fatal("audio ended before the provider spoke")
		}
		return frame
	case <-time.After(eventually):
		t.Fatal("no audio received from the provider")
	}
	return nil
}

func TestEngineRegistersWithDigestAuthentication(t *testing.T) {
	f := startEngine(t, nil)
	if f.provider.Registrations() < 1 {
		t.Fatal("provider never accepted a REGISTER")
	}
	if got := f.persistedStatus(); got != sip_trunk.RegistrationStatusRegistered {
		t.Fatalf("persisted status = %s, want REGISTERED", got)
	}
}

func TestEngineReportsFailedRegistrationForWrongCredentials(t *testing.T) {
	provider := voiptest.StartProvider(t)
	trunk := testTrunk(provider)
	trunk.Password = "wrong"
	repo := siptrunktest.NewMemoryRepository(trunk)
	manager := newEngine(t, repo, &gaugeMetrics{}, nil)
	if err := manager.RegisterTrunk(trunk); err != nil {
		t.Fatalf("RegisterTrunk() error = %v", err)
	}
	f := engineFixture{manager: manager, repo: repo, provider: provider, trunk: trunk}
	f.waitStatus(t, sip_trunk.RegistrationStatusFailed)
	if _, err := manager.Invite(context.Background(), trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "5511"}); !errors.Is(err, sip_trunk.ErrTrunkNotRegistered) {
		t.Fatalf("Invite() on a failed trunk = %v, want ErrTrunkNotRegistered", err)
	}
}

func TestEngineOutboundCallCarriesMediaBothWaysAndEndsOnRemoteBye(t *testing.T) {
	f := startEngine(t, nil)
	received := make(chan struct{}, 1)
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		if err := d.Answer(); err != nil {
			return
		}
		go voiptest.SendRTP(d, 10)
		var p rtp.Packet
		if _, err := d.MediaSession().ReadRTP(make([]byte, 1500), &p); err == nil {
			received <- struct{}{}
		}
		time.Sleep(300 * time.Millisecond)
		_ = d.Hangup(context.Background())
	})

	session, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "+5511999990000"})
	if err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	if got := <-f.provider.InvitedUsers; got != "+5511999990000" {
		t.Fatalf("provider saw INVITE for %q, want the dialled number", got)
	}
	if session.Direction != sip_trunk.CallDirectionOutbound || session.Media.Codec.Name == "" {
		t.Fatalf("session = %+v, want an outbound call with a negotiated codec", session)
	}
	if calls := f.manager.ActiveCalls(f.trunk.ID); len(calls) != 1 || calls[0].ID != session.ID {
		t.Fatalf("ActiveCalls() = %+v, want the new call", calls)
	}
	if f.metrics.ongoing.Load() != 1 {
		t.Fatalf("ongoing gauge = %d, want 1", f.metrics.ongoing.Load())
	}

	if frame := readFrame(t, session.Audio); len(frame) != 320 {
		t.Fatalf("decoded frame = %d bytes, want 20 ms of PCM16", len(frame))
	}
	for range 5 {
		sendFrame(t, session.Audio)
	}
	select {
	case <-received:
	case <-time.After(eventually):
		t.Fatal("provider never received our RTP")
	}
	f.waitNoCalls(t)
	if f.metrics.finished.Load() != 1 {
		t.Fatalf("finished counter = %d, want 1", f.metrics.finished.Load())
	}
}

func TestEngineDialPlanRewritesTheDialledNumber(t *testing.T) {
	provider := voiptest.StartProvider(t)
	trunk := testTrunk(provider)
	trunk.Settings.DialPlan = sip_trunk.DialPlan{StripPrefix: "+55", AddPrefix: "0"}
	repo := siptrunktest.NewMemoryRepository()
	manager := newEngine(t, repo, &gaugeMetrics{}, nil)
	if err := manager.RegisterTrunk(trunk); err != nil {
		t.Fatal(err)
	}
	f := engineFixture{manager: manager, repo: repo, provider: provider, trunk: trunk, metrics: &gaugeMetrics{}}
	f.waitStatus(t, sip_trunk.RegistrationStatusRegistered)
	provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Respond(sip.StatusBusyHere, "Busy Here", nil)
	})
	_, _ = manager.Invite(context.Background(), trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "+5511999990000"})
	if got := <-provider.InvitedUsers; got != "011999990000" {
		t.Fatalf("provider saw %q, want the dial-plan result 011999990000", got)
	}
}

func TestEngineHangupSendsByeToTheProvider(t *testing.T) {
	f := startEngine(t, nil)
	byeReceived := make(chan struct{})
	f.provider.OnCall(voiptest.AnswerAndHold(byeReceived))
	session, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"})
	if err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	if err := f.manager.Hangup(context.Background(), f.trunk.ID, session.ID); err != nil {
		t.Fatalf("Hangup() error = %v", err)
	}
	select {
	case <-byeReceived:
	case <-time.After(eventually):
		t.Fatal("provider dialog never ended after Hangup")
	}
	f.waitNoCalls(t)
	if err := f.manager.Hangup(context.Background(), f.trunk.ID, session.ID); !errors.Is(err, sip_trunk.ErrCallNotFound) {
		t.Fatalf("second Hangup() = %v, want ErrCallNotFound", err)
	}
}

func TestEngineHangupIgnoresCallsOfAnotherTrunk(t *testing.T) {
	f := startEngine(t, nil)
	f.provider.OnCall(voiptest.AnswerAndHold(nil))
	session, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"})
	if err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	if err := f.manager.Hangup(context.Background(), "other-trunk", session.ID); !errors.Is(err, sip_trunk.ErrCallNotFound) {
		t.Fatalf("Hangup() through another trunk = %v, want ErrCallNotFound", err)
	}
	if len(f.manager.ActiveCalls(f.trunk.ID)) != 1 {
		t.Fatal("a hangup addressed to another trunk ended the call")
	}
}

func TestEngineRejectedCallReturnsTheProviderStatus(t *testing.T) {
	f := startEngine(t, nil)
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Respond(sip.StatusBusyHere, "Busy Here", nil)
	})
	_, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"})
	var rejected *sip_trunk.CallRejectedError
	if !errors.As(err, &rejected) || rejected.StatusCode != sip.StatusBusyHere {
		t.Fatalf("Invite() error = %v, want CallRejectedError 486", err)
	}
	f.waitNoCalls(t)
	if f.metrics.finished.Load() != 1 {
		t.Fatalf("finished counter = %d, want 1", f.metrics.finished.Load())
	}
}

func TestEngineCancelsTheInviteWhenTheCallerGivesUp(t *testing.T) {
	f := startEngine(t, nil)
	cancelled := make(chan struct{})
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		<-d.Context().Done()
		close(cancelled)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := f.manager.Invite(ctx, f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"}); err == nil {
		t.Fatal("Invite() with an expired context = nil error")
	}
	select {
	case <-cancelled:
	case <-time.After(eventually):
		t.Fatal("provider never saw the CANCEL")
	}
	f.waitNoCalls(t)
}

func TestEngineHangsUpACallWhoseMediaStops(t *testing.T) {
	f := startEngine(t, func(cfg *TrunkManagerConfig) { cfg.MediaTimeout = 300 * time.Millisecond })
	ended := make(chan struct{})
	f.provider.OnCall(voiptest.AnswerAndHold(ended))
	if _, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"}); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	select {
	case <-ended:
	case <-time.After(eventually):
		t.Fatal("silent call was never hung up")
	}
	f.waitNoCalls(t)
}

func TestEngineHangsUpAtTheMaximumCallDuration(t *testing.T) {
	f := startEngine(t, func(cfg *TrunkManagerConfig) { cfg.MaxCallDuration = 400 * time.Millisecond })
	ended := make(chan struct{})
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		defer close(ended)
		if err := d.Answer(); err != nil {
			return
		}
		go voiptest.SendRTP(d, 100)
		<-d.Context().Done()
	})
	if _, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"}); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	select {
	case <-ended:
	case <-time.After(eventually):
		t.Fatal("call outlived the maximum duration")
	}
	f.waitNoCalls(t)
}

func TestEngineKeepsTheCallAndMediaThroughAReInvite(t *testing.T) {
	f := startEngine(t, nil)
	reinvited := make(chan error, 1)
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		if err := d.Answer(); err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		reinvited <- d.ReInvite(ctx)
		cancel()
		go voiptest.SendRTP(d, 50)
		<-d.Context().Done()
	})
	session, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"})
	if err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	select {
	case err := <-reinvited:
		if err != nil {
			t.Fatalf("provider re-INVITE failed: %v", err)
		}
	case <-time.After(eventually):
		t.Fatal("re-INVITE never completed")
	}
	if frame := readFrame(t, session.Audio); len(frame) != 320 {
		t.Fatalf("decoded frame after re-INVITE = %d bytes", len(frame))
	}
	if len(f.manager.ActiveCalls(f.trunk.ID)) != 1 {
		t.Fatal("re-INVITE ended the call")
	}
}

func TestEngineUnregisterHangsUpActiveCalls(t *testing.T) {
	f := startEngine(t, nil)
	ended := make(chan struct{})
	f.provider.OnCall(voiptest.AnswerAndHold(ended))
	if _, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"}); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	if err := f.manager.UnregisterTrunk(f.trunk.ID); err != nil {
		t.Fatalf("UnregisterTrunk() error = %v", err)
	}
	select {
	case <-ended:
	case <-time.After(eventually):
		t.Fatal("removing the trunk left its call up")
	}
	if _, ok := f.manager.TrunkStatus(f.trunk.ID); ok {
		t.Fatal("trunk still has a connection after UnregisterTrunk")
	}
	if got := f.persistedStatus(); got != sip_trunk.RegistrationStatusUnregistered {
		t.Fatalf("persisted status = %s, want UNREGISTERED", got)
	}
	waitFor(t, "gauges back to zero", func() bool { return f.metrics.ongoing.Load() == 0 && f.metrics.dialing.Load() == 0 })
}

func TestEngineRefreshReconnectsOnTheSamePort(t *testing.T) {
	f := startEngine(t, nil)
	updated := *f.trunk
	updated.Name = "Renamed"
	if err := f.manager.RefreshTrunk(&updated); err != nil {
		t.Fatalf("RefreshTrunk() error = %v", err)
	}
	f.waitStatus(t, sip_trunk.RegistrationStatusRegistered)
	disabled := updated
	disabled.Enabled = false
	if err := f.manager.RefreshTrunk(&disabled); err != nil {
		t.Fatalf("RefreshTrunk(disabled) error = %v", err)
	}
	if _, ok := f.manager.TrunkStatus(f.trunk.ID); ok {
		t.Fatal("a disabled trunk kept its connection")
	}
}

func TestEngineStartRegistersEnabledTrunksAndStopEndsCalls(t *testing.T) {
	provider := voiptest.StartProvider(t)
	trunk := testTrunk(provider)
	repo := siptrunktest.NewMemoryRepository(trunk)
	metrics := &gaugeMetrics{}
	manager := newEngine(t, repo, metrics, nil)
	f := engineFixture{manager: manager, repo: repo, metrics: metrics, provider: provider, trunk: trunk}
	f.waitStatus(t, sip_trunk.RegistrationStatusRegistered)

	ended := make(chan struct{})
	provider.OnCall(voiptest.AnswerAndHold(ended))
	if _, err := manager.Invite(context.Background(), trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"}); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-ended:
	case <-time.After(eventually):
		t.Fatal("Stop() left a call up")
	}
	if _, err := manager.Invite(context.Background(), trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"}); !errors.Is(err, sip_trunk.ErrEngineNotRunning) {
		t.Fatalf("Invite() after Stop = %v, want ErrEngineNotRunning", err)
	}
	if metrics.ongoing.Load() != 0 || metrics.dialing.Load() != 0 {
		t.Fatalf("gauges after Stop = dialing %d ongoing %d, want 0", metrics.dialing.Load(), metrics.ongoing.Load())
	}
}

type answeringHandler struct {
	answered chan sip_trunk.TrunkCallSession
}

func (h *answeringHandler) HandleInboundInvite(ctx context.Context, invite sip_trunk.InboundInvite) error {
	session, err := invite.Dialog.Answer(ctx)
	if err != nil {
		return err
	}
	h.answered <- session
	for range session.Audio.Frames() {
	}
	return nil
}

func dialEngine(t *testing.T, provider *voiptest.Provider, manager *SIPTrunkManager, trunkID string) (*diago.DialogClientSession, error) {
	t.Helper()
	conn, ok := manager.connection(trunkID)
	if !ok {
		t.Fatal("trunk has no connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return provider.Dialer.Invite(ctx, sip.Uri{User: "4000", Host: "127.0.0.1", Port: conn.listenPort}, diago.InviteOptions{})
}

func TestEngineRejectsInboundCallsWithoutAHandler(t *testing.T) {
	f := startEngine(t, nil)
	_, err := dialEngine(t, f.provider, f.manager, f.trunk.ID)
	var response *sipgo.ErrDialogResponse
	if !errors.As(err, &response) || response.Res.StatusCode != sip.StatusTemporarilyUnavailable {
		t.Fatalf("inbound INVITE without handler = %v, want 480", err)
	}
}

func TestEngineRejectsInboundCallsFromUnknownSources(t *testing.T) {
	provider := voiptest.StartProvider(t)
	trunk := testTrunk(provider)
	trunk.Host = "192.0.2.10"
	trunk.Username, trunk.Password = "", ""
	trunk.Settings.SkipRegistration = true
	repo := siptrunktest.NewMemoryRepository()
	manager := newEngine(t, repo, &gaugeMetrics{}, nil)
	manager.SetInboundInviteHandler(&answeringHandler{answered: make(chan sip_trunk.TrunkCallSession, 1)})
	if err := manager.RegisterTrunk(trunk); err != nil {
		t.Fatal(err)
	}
	f := engineFixture{manager: manager, repo: repo, provider: provider, trunk: trunk}
	f.waitStatus(t, sip_trunk.RegistrationStatusRegistered)

	_, err := dialEngine(t, provider, manager, trunk.ID)
	var response *sipgo.ErrDialogResponse
	if !errors.As(err, &response) || response.Res.StatusCode != sip.StatusForbidden {
		t.Fatalf("INVITE from an unlisted source = %v, want 403", err)
	}
}

func TestEngineAnswersInboundCallsFromTheProvider(t *testing.T) {
	f := startEngine(t, nil)
	handler := &answeringHandler{answered: make(chan sip_trunk.TrunkCallSession, 1)}
	f.manager.SetInboundInviteHandler(handler)

	dialog, err := dialEngine(t, f.provider, f.manager, f.trunk.ID)
	if err != nil {
		t.Fatalf("inbound INVITE error = %v", err)
	}
	var session sip_trunk.TrunkCallSession
	select {
	case session = <-handler.answered:
	case <-time.After(eventually):
		t.Fatal("inbound handler never answered")
	}
	if session.Direction != sip_trunk.CallDirectionInbound {
		t.Fatalf("Direction = %s, want inbound", session.Direction)
	}
	if calls := f.manager.ActiveCalls(f.trunk.ID); len(calls) != 1 {
		t.Fatalf("ActiveCalls() = %+v, want the inbound call", calls)
	}
	if err := dialog.Hangup(context.Background()); err != nil {
		t.Fatalf("provider hangup error = %v", err)
	}
	_ = dialog.Close()
	f.waitNoCalls(t)
}

func (f engineFixture) persistedStatus() sip_trunk.RegistrationStatus {
	stored, _ := f.repo.Stored(f.trunk.ID)
	if stored == nil {
		return ""
	}
	return stored.RegistrationStatus
}
