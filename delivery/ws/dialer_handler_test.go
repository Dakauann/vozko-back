package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	"vozko/infra/http/middleware"
	dialer_usecase "vozko/usecases/dialer"
)

type fakeDialerCRMCall struct {
	id     string
	events chan conversation.CallEvent
	audio  chan []byte
	done   chan struct{}

	mu             sync.Mutex
	sendAudioCalls [][]byte
	hangupCount    int32
	closeOnHangup  bool
	closeAfterHang time.Duration
}

func newFakeDialerCRMCall(id string) *fakeDialerCRMCall {
	return &fakeDialerCRMCall{
		id:     id,
		events: make(chan conversation.CallEvent, 16),
		audio:  make(chan []byte, 16),
		done:   make(chan struct{}),
	}
}

func (c *fakeDialerCRMCall) ID() string                            { return c.id }
func (c *fakeDialerCRMCall) AudioStream() <-chan []byte            { return c.audio }
func (c *fakeDialerCRMCall) Events() <-chan conversation.CallEvent { return c.events }
func (c *fakeDialerCRMCall) Done() <-chan struct{}                 { return c.done }

func (c *fakeDialerCRMCall) SendAudio(pcm []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cp := append([]byte(nil), pcm...)
	c.sendAudioCalls = append(c.sendAudioCalls, cp)
	return nil
}

func (c *fakeDialerCRMCall) Hangup() error {
	atomic.AddInt32(&c.hangupCount, 1)
	if c.closeOnHangup {
		if c.closeAfterHang > 0 {
			go func() {
				time.Sleep(c.closeAfterHang)
				c.closeDoneOnce()
			}()
		} else {
			c.closeDoneOnce()
		}
	}
	return nil
}

func (c *fakeDialerCRMCall) closeDoneOnce() {
	defer func() { _ = recover() }()
	close(c.done)
}

func (c *fakeDialerCRMCall) snapshotAudioCalls() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.sendAudioCalls))
	for i, b := range c.sendAudioCalls {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

type fakeStartUseCase struct {
	call      *fakeDialerCRMCall
	admission *dialer_domain.CallAdmissionLease
	err       error
}

func (f *fakeStartUseCase) Execute(_ context.Context, _ dialer_domain.StartOutboundCallInput) (*dialer_domain.StartOutboundCallResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &dialer_domain.StartOutboundCallResult{
		Call:        f.call,
		PhoneNumber: "+15555550100",
		Admission:   f.admission,
	}, nil
}

type fakeEndUseCase struct {
	admission *recordingAdmission
	mu        sync.Mutex
	calls     []dialer_domain.EndOutboundCallInput
}

func (f *fakeEndUseCase) Execute(_ context.Context, in dialer_domain.EndOutboundCallInput) error {
	f.mu.Lock()
	f.calls = append(f.calls, in)
	f.mu.Unlock()
	if in.Hangup && in.Call != nil {
		_ = in.Call.Hangup()
	}
	if in.ReleaseAdmission && in.Admission != nil && f.admission != nil {
		return f.admission.Release(in.Admission)
	}
	return nil
}

func (f *fakeEndUseCase) snapshot() []dialer_domain.EndOutboundCallInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dialer_domain.EndOutboundCallInput, len(f.calls))
	copy(out, f.calls)
	return out
}

type recordingAdmission struct {
	releaseCount int32
}

func (r *recordingAdmission) Acquire(_ context.Context, _ dialer_domain.CallAdmissionInput) (*dialer_domain.CallAdmissionLease, error) {
	return nil, nil
}

func (r *recordingAdmission) Refresh(_ *dialer_domain.CallAdmissionLease, _ time.Duration) error {
	return nil
}

func (r *recordingAdmission) Release(_ *dialer_domain.CallAdmissionLease) error {
	atomic.AddInt32(&r.releaseCount, 1)
	return nil
}

func (r *recordingAdmission) Releases() int32 {
	return atomic.LoadInt32(&r.releaseCount)
}

type allowAllAuthorizer struct{}

func (allowAllAuthorizer) CanAccessEntry(_, _, _, _ string, _ bool) bool    { return true }
func (allowAllAuthorizer) CanAccessCampaign(_, _, _, _ string, _ bool) bool { return true }
func (allowAllAuthorizer) GetAccessibleEntryIDs(_, _ string, _ bool) []string {
	return nil
}
func (allowAllAuthorizer) GetDepartmentScope(_, _ string, _ bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}
func (allowAllAuthorizer) HasWorkspacePermission(_, _, _, _ string, _ bool) bool { return true }
func (allowAllAuthorizer) IsWorkspaceMember(_, _ string) bool                    { return true }
func (allowAllAuthorizer) IsWorkspaceOwnerOrAdmin(_, _ string) bool              { return true }

type dialerTestHarness struct {
	server    *httptest.Server
	dialURL   string
	startUC   *fakeStartUseCase
	endUC     *fakeEndUseCase
	admission *recordingAdmission
	lifecycle *dialer_usecase.OutboundCallLifecycleRunner
	fakeCall  *fakeDialerCRMCall
}

func newDialerTestHarness(t *testing.T, call *fakeDialerCRMCall) *dialerTestHarness {
	t.Helper()

	adm := &recordingAdmission{}
	lifecycle := dialer_usecase.NewOutboundCallLifecycleRunner(adm, nil, nil, nil, log.Default())

	lease := &dialer_domain.CallAdmissionLease{
		WorkspaceID:  "ws-test",
		SlotAcquired: true,
		AcquiredAt:   time.Now(),
	}

	start := &fakeStartUseCase{call: call, admission: lease}
	end := &fakeEndUseCase{admission: adm}

	h := NewDialerWSHandler(start, end, lifecycle, allowAllAuthorizer{}, log.Default(), noopWSMetricsRecorder{})

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey,
			&auth.Claims{UserID: "user-test", Role: "admin"})
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-test")
		h.HandleWebSocket(w, r.WithContext(ctx))
	})

	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/"

	return &dialerTestHarness{
		server:    srv,
		dialURL:   u.String(),
		startUC:   start,
		endUC:     end,
		admission: adm,
		lifecycle: lifecycle,
		fakeCall:  call,
	}
}

func (h *dialerTestHarness) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(h.dialURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func (h *dialerTestHarness) startCall(t *testing.T, c *websocket.Conn) {
	t.Helper()

	payload, _ := json.Marshal(StartCallPayload{
		PhoneNumber: "+15555550100",
		RequestID:   "req-1",
	})
	msg := WSIncomingMessage{Type: WSEventStartCall, Payload: payload}
	if err := c.WriteJSON(msg); err != nil {
		t.Fatalf("write start_call: %v", err)
	}

	h.fakeCall.events <- conversation.CallEvent{Type: conversation.CallEventRinging}
	h.fakeCall.events <- conversation.CallEvent{Type: conversation.CallEventAnswered}

	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read while waiting for answered: %v", err)
		}
		var env struct {
			Type    WSEventType     `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.Type != WSEventCallStatus {
			continue
		}
		var p CallStatusPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			continue
		}
		if p.Status == conversation.CallEventAnswered {
			c.SetReadDeadline(time.Time{})
			return
		}
	}
}

func TestDialerWS_HappyPath_ReleasesAdmissionExactlyOnce(t *testing.T) {
	call := newFakeDialerCRMCall("crm-happy")
	call.closeOnHangup = true
	h := newDialerTestHarness(t, call)

	c := h.dial(t)
	h.startCall(t, c)

	call.closeDoneOnce()

	if !waitFor(t, 2*time.Second, func() bool {
		return h.admission.Releases() == 1
	}) {
		t.Fatalf("expected admission.Release count == 1, got %d", h.admission.Releases())
	}

	_ = c.Close()
	time.Sleep(150 * time.Millisecond)

	if got := h.admission.Releases(); got != 1 {
		t.Fatalf("admission released more than once: got %d, want 1", got)
	}
}

func TestDialerWS_StuckCall_WSClosed_AdmissionLeaks(t *testing.T) {
	call := newFakeDialerCRMCall("crm-stuck")

	call.closeOnHangup = false

	h := newDialerTestHarness(t, call)
	c := h.dial(t)
	h.startCall(t, c)

	_ = c.Close()

	if !waitFor(t, 5*time.Second, func() bool {
		return h.admission.Releases() >= 1
	}) {
		t.Fatalf("BUG B REPRO: admission was never released after WS close (releases=%d). "+
			"Call slot will leak until process restart.", h.admission.Releases())
	}

	if got := h.admission.Releases(); got != 1 {
		t.Fatalf("admission released %d times, want exactly 1 (double-release would corrupt the slot counter)", got)
	}
}

func TestDialerWS_RaceBetweenLifecycleExitAndForcedRelease(t *testing.T) {
	call := newFakeDialerCRMCall("crm-race")

	call.closeOnHangup = true
	call.closeAfterHang = 150 * time.Millisecond

	h := newDialerTestHarness(t, call)
	c := h.dial(t)
	h.startCall(t, c)

	_ = c.Close()

	if !waitFor(t, 5*time.Second, func() bool {
		return h.admission.Releases() >= 1
	}) {
		t.Fatalf("admission was never released (releases=%d)", h.admission.Releases())
	}

	time.Sleep(500 * time.Millisecond)

	if got := h.admission.Releases(); got != 1 {
		t.Fatalf("DOUBLE RELEASE: admission released %d times, want exactly 1", got)
	}
}

func TestDialerWS_CallAudio_ForwardsPCMUnchanged(t *testing.T) {
	call := newFakeDialerCRMCall("crm-audio")
	call.closeOnHangup = true
	h := newDialerTestHarness(t, call)

	c := h.dial(t)
	h.startCall(t, c)

	frame := make([]byte, 320)
	for i := 0; i < 160; i++ {
		sample := int16(((i * 100) % 16000) - 8000)
		frame[2*i] = byte(uint16(sample) & 0xFF)
		frame[2*i+1] = byte((uint16(sample) >> 8) & 0xFF)
	}

	payload, _ := json.Marshal(CallAudioPayload{
		Audio:      base64.StdEncoding.EncodeToString(frame),
		SampleRate: 8000,
	})
	msg := WSIncomingMessage{Type: WSEventCallAudio, Payload: payload}
	if err := c.WriteJSON(msg); err != nil {
		t.Fatalf("write call_audio: %v", err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		return len(call.snapshotAudioCalls()) >= 1
	}) {
		t.Fatalf("CRMCall.SendAudio was never called for the forwarded WS audio frame")
	}

	calls := call.snapshotAudioCalls()
	if len(calls[0]) != len(frame) {
		t.Fatalf("SendAudio received %d bytes, want %d", len(calls[0]), len(frame))
	}
	for i := range frame {
		if calls[0][i] != frame[i] {
			t.Fatalf("byte %d differs: got 0x%02X, want 0x%02X", i, calls[0][i], frame[i])
		}
	}

	uniq := map[byte]struct{}{}
	for _, b := range calls[0] {
		uniq[b] = struct{}{}
	}
	if len(uniq) < 50 {
		t.Fatalf("test buffer is too uniform (%d unique bytes); rewrite it to actually exercise non-silent audio", len(uniq))
	}
}

func TestDialerWS_CallAudio_DropsUnsupportedSampleRate(t *testing.T) {
	call := newFakeDialerCRMCall("crm-audio-rate")
	call.closeOnHangup = true
	h := newDialerTestHarness(t, call)

	c := h.dial(t)
	h.startCall(t, c)

	frame := make([]byte, 320)
	payload, _ := json.Marshal(CallAudioPayload{
		Audio:      base64.StdEncoding.EncodeToString(frame),
		SampleRate: 11025,
	})
	msg := WSIncomingMessage{Type: WSEventCallAudio, Payload: payload}
	if err := c.WriteJSON(msg); err != nil {
		t.Fatalf("write call_audio: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if got := len(call.snapshotAudioCalls()); got != 0 {
		t.Fatalf("SendAudio calls = %d, want 0 for unsupported sample rate", got)
	}
}

func TestDialerWS_CallAudio_ResamplesSupportedRate(t *testing.T) {
	call := newFakeDialerCRMCall("crm-audio-resample")
	call.closeOnHangup = true
	h := newDialerTestHarness(t, call)

	c := h.dial(t)
	h.startCall(t, c)

	frame := make([]byte, 480*2)
	payload, _ := json.Marshal(CallAudioPayload{
		Audio:      base64.StdEncoding.EncodeToString(frame),
		SampleRate: 48000,
	})
	msg := WSIncomingMessage{Type: WSEventCallAudio, Payload: payload}
	if err := c.WriteJSON(msg); err != nil {
		t.Fatalf("write call_audio: %v", err)
	}

	if !waitFor(t, time.Second, func() bool {
		return len(call.snapshotAudioCalls()) >= 1
	}) {
		t.Fatalf("SendAudio not invoked after resample; got %d calls", len(call.snapshotAudioCalls()))
	}
	out := call.snapshotAudioCalls()[0]
	if len(out) >= len(frame) {
		t.Fatalf("resampled payload size %d should be smaller than source %d", len(out), len(frame))
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

var _ = strings.TrimSpace
var _ = fmt.Sprintf
