package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/gorilla/websocket"
	"github.com/pion/rtp"

	"vozko/domain/auth"
	"vozko/domain/callsession"
	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
	infra_callsession "vozko/infra/callsession"
	"vozko/infra/http/middleware"
	voipinfra "vozko/infra/voip"
	"vozko/infra/voip/voiptest"
	callsession_usecase "vozko/usecases/callsession"
	conversation_usecase "vozko/usecases/conversation"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
)

const (
	dialerWorkspace = "ws-dialer"
	otherWorkspace  = "ws-other"
	dialerUser      = "agent-1"
	otherUser       = "agent-2"
	colleagueUser   = "agent-3"
	listenerUser    = "agent-4"
)

type dialerGrants map[string]bool

func (g dialerGrants) MayCallThroughTrunks(userID, workspaceID string, isAdmin bool) bool {
	return isAdmin || g[userID+"|"+workspaceID+"|sip_trunks|call"]
}

type sipAdmission struct {
	mu       sync.Mutex
	channels []string
	released int
}

func (a *sipAdmission) Acquire(_ context.Context, input callsession.CallAdmissionInput) (*callsession.CallAdmissionLease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.channels = append(a.channels, input.CallChannel)
	return &callsession.CallAdmissionLease{WorkspaceID: input.WorkspaceID, CallChannel: input.CallChannel, SlotAcquired: true}, nil
}
func (a *sipAdmission) Refresh(*callsession.CallAdmissionLease, time.Duration) error { return nil }
func (a *sipAdmission) Release(*callsession.CallAdmissionLease) error {
	a.mu.Lock()
	a.released++
	a.mu.Unlock()
	return nil
}

type dialerStack struct {
	server    *httptest.Server
	provider  *voiptest.Provider
	manager   *voipinfra.SIPTrunkManager
	trunk     *sip_trunk.SIPTrunk
	billing   *liveCallBillingFakePub
	admission *sipAdmission
	sipPort   int
	routing   *routingParts
	executor  *CallSessionInboundExecutor
	sessions  callsession.CallSessionRegistry
}

type dialerStackConfig struct {
	mediaTimeout time.Duration
	routing      func(stackServices) *routingParts
}

type stackServices struct {
	sessions  callsession.CallSessionRegistry
	broker    *callsession_usecase.InboundOfferBroker
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner
	end       callsession.EndOutboundCallUseCase
	grants    dialerGrants
}

func startDialerStack(t *testing.T) *dialerStack {
	return startDialerStackWithMediaTimeout(t, 30*time.Second)
}

func startDialerStackWithMediaTimeout(t *testing.T, mediaTimeout time.Duration) *dialerStack {
	return startStack(t, dialerStackConfig{mediaTimeout: mediaTimeout})
}

func startStack(t *testing.T, cfg dialerStackConfig) *dialerStack {
	t.Helper()
	mediaTimeout := cfg.mediaTimeout
	provider := voiptest.StartProvider(t)
	bindPort := voiptest.FreeUDPPort(t)
	trunk := &sip_trunk.SIPTrunk{
		ID: "trunk-dialer", WorkspaceID: dialerWorkspace, Name: "Main", TrunkType: sip_trunk.TrunkTypeBidirectional,
		Host: "127.0.0.1", Port: provider.Port, Transport: sip_trunk.TransportUDP,
		Username: voiptest.Username, Password: voiptest.Password, Enabled: true,
		Settings: sip_trunk.Settings{BindHost: "127.0.0.1", BindPort: bindPort, PublicAddress: "127.0.0.1"},
	}
	repo := siptrunktest.NewMemoryRepository(trunk)
	manager, err := voipinfra.NewSIPTrunkManager(voipinfra.TrunkManagerConfig{
		SIPBindHost: "127.0.0.1", SIPPortStart: voiptest.FreeUDPPort(t), SIPPortCount: 1,
		RTPPortStart: 43000, RTPPortEnd: 43999, RegisterExpiry: time.Minute, DialTimeout: 5 * time.Second,
		MediaTimeout: mediaTimeout, MaxCallDuration: time.Hour, WatchInterval: 100 * time.Millisecond,
		PublicAddress: "127.0.0.1", UserAgent: "VozkoTest", AllowPrivateHosts: true,
	}, repo)
	if err != nil {
		t.Fatal(err)
	}
	grants := dialerGrants{
		dialerUser + "|" + dialerWorkspace + "|sip_trunks|call":       true,
		dialerUser + "|" + dialerWorkspace + "|call_session|use":      true,
		otherUser + "|" + otherWorkspace + "|sip_trunks|call":         true,
		otherUser + "|" + otherWorkspace + "|call_session|use":        true,
		dialerUser + "|" + dialerWorkspace + "|conversations|read":    true,
		colleagueUser + "|" + dialerWorkspace + "|sip_trunks|call":    true,
		colleagueUser + "|" + dialerWorkspace + "|call_session|use":   true,
		colleagueUser + "|" + dialerWorkspace + "|conversations|read": true,
		dialerUser + "|" + dialerWorkspace + "|call_session|transfer": true,
		listenerUser + "|" + dialerWorkspace + "|call_session|use":    true,
	}

	admission := &sipAdmission{}
	pub := &liveCallBillingFakePub{}
	lifecycle, err := callsession_usecase.NewOutboundCallLifecycleRunner(admission, nil, nil, pub, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	source := sip_trunk_usecase.NewCallSource(sip_trunk_usecase.NewCallPlanner(repo, manager, grants), manager)
	start := callsession_usecase.NewStartOutboundCallUseCase(conversation_usecase.NewDispatchingCallSource(nil, source), nil, admission)
	end := callsession_usecase.NewEndOutboundCallUseCase(admission)
	sessions := infra_callsession.NewInProcSessionRegistry()
	calls := infra_callsession.NewInProcCallRegistry()
	broker := callsession_usecase.NewInboundOfferBroker()
	executor := NewCallSessionInboundExecutor(calls, end, lifecycle, nil, log.Default())
	inbound := sip_trunk_usecase.InboundCallConfig{
		Sessions: sessions, Admission: admission, Ringer: callsession_usecase.NewInboundRinger(broker),
		Executor: executor, Permissions: grants, Engine: manager,
	}
	var routing *routingParts
	if cfg.routing != nil {
		routing = cfg.routing(stackServices{sessions: sessions, broker: broker, lifecycle: lifecycle, end: end, grants: grants})
		executor.WithChannels(routing.channels)
		inbound.Parking, inbound.Queues, inbound.VoiceFlows = routing.channels, routing.transfers, routing.flows
	}
	manager.SetInboundInviteHandler(sip_trunk_usecase.NewInboundCallUseCase(inbound))
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if status, ok := manager.TrunkStatus(trunk.ID); ok && status.Status == sip_trunk.RegistrationStatusRegistered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("trunk never registered")
		}
		time.Sleep(20 * time.Millisecond)
	}

	authorizer := workspaceScopedAuthorizer{granted: grants}
	handler := NewCallSessionWSHandler(start, end, lifecycle, authorizer, log.Default(), noopWSMetricsRecorder{}).
		WithRegistries(sessions, calls).
		WithInboundCalls(broker)
	if routing != nil {
		handler.WithChannels(routing.channels).WithTransfers(routing.transfers).WithReconnects(routing.transfers)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user")
		ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: userID, Role: "user"})
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, r.URL.Query().Get("workspaceId"))
		handler.HandleWebSocket(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	return &dialerStack{server: server, provider: provider, manager: manager, trunk: trunk, billing: pub, admission: admission, sipPort: bindPort, routing: routing, executor: executor, sessions: sessions}
}

type dialerClient struct {
	t    *testing.T
	conn *websocket.Conn
	msgs chan WSOutgoingMessage
	raw  chan json.RawMessage
}

func (s *dialerStack) connect(t *testing.T, userID, workspaceID string) *dialerClient {
	t.Helper()
	u, _ := url.Parse(s.server.URL)
	u.Scheme = "ws"
	u.RawQuery = url.Values{"user": {userID}, "workspaceId": {workspaceID}}.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial call session: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &dialerClient{t: t, conn: conn, raw: make(chan json.RawMessage, 512)}
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				close(c.raw)
				return
			}
			for _, line := range strings.Split(string(data), "\n") {
				if strings.TrimSpace(line) != "" {
					c.raw <- json.RawMessage(line)
				}
			}
		}
	}()
	c.waitFor(WSEventConnected, nil)
	return c
}

func (c *dialerClient) send(eventType WSEventType, payload any) {
	c.t.Helper()
	raw, _ := json.Marshal(payload)
	if err := c.conn.WriteJSON(WSIncomingMessage{Type: eventType, Payload: raw}); err != nil {
		c.t.Fatalf("send %s: %v", eventType, err)
	}
}

func (c *dialerClient) waitFor(eventType WSEventType, match func(json.RawMessage) bool) json.RawMessage {
	c.t.Helper()
	timeout := time.After(8 * time.Second)
	for {
		select {
		case raw, ok := <-c.raw:
			if !ok {
				c.t.Fatalf("socket closed while waiting for %s", eventType)
			}
			var env struct {
				Type    WSEventType     `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(raw, &env) != nil || env.Type != eventType {
				continue
			}
			if match == nil || match(env.Payload) {
				return env.Payload
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s", eventType)
		}
	}
}

func statusIs(status string) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var p CallStatusPayload
		return json.Unmarshal(raw, &p) == nil && string(p.Status) == status
	}
}

func (c *dialerClient) sendSpeech(frames int) {
	c.t.Helper()
	for i := 0; i < frames; i++ {
		c.send(WSEventCallAudio, CallAudioPayload{Audio: base64.StdEncoding.EncodeToString(make([]byte, 320)), SampleRate: 8000})
	}
}

func TestDialerCallsThroughATrunkWithAudioBothWaysAndBillsTheMinute(t *testing.T) {
	stack := startDialerStack(t)
	heard := make(chan struct{}, 1)
	ended := make(chan struct{})
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		defer close(ended)
		if err := d.Answer(); err != nil {
			return
		}
		go voiptest.SendRTP(d, 200)
		var p rtp.Packet
		if _, err := d.MediaSession().ReadRTP(make([]byte, 1500), &p); err == nil {
			heard <- struct{}{}
		}
		<-d.Context().Done()
	})

	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "+55 11 99999-0000", TrunkID: stack.trunk.ID, RequestID: "req-1"})
	client.waitFor(WSEventCallStatus, statusIs("answered"))
	if got := <-stack.provider.InvitedUsers; got != "5511999990000" {
		t.Fatalf("provider was asked to reach %q", got)
	}

	audio := client.waitFor(WSEventCallAudioS, nil)
	var out CallAudioOutPayload
	if err := json.Unmarshal(audio, &out); err != nil || out.SampleRate != 8000 || out.Audio == "" {
		t.Fatalf("browser audio = %s, want 8 kHz PCM from the provider", audio)
	}
	client.sendSpeech(10)
	select {
	case <-heard:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider never heard the browser")
	}

	client.send(WSEventEndCall, map[string]string{"request_id": "req-1"})
	client.waitFor(WSEventCallEnded, nil)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider's leg stayed up after end_call")
	}

	deadline := time.Now().Add(3 * time.Second)
	for stack.billing.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	events := stack.billing.Events(t)
	if len(events) != 1 || events[0].Channel != "sip" || !strings.HasPrefix(events[0].CallID, "sip-out-") || events[0].DurationSec < 1 {
		t.Fatalf("billing events = %+v, want one sip minute", events)
	}
	if len(stack.manager.ActiveCalls(stack.trunk.ID)) != 0 {
		t.Fatal("the engine still tracks the call")
	}
}

func TestDialerCannotUseAnotherWorkspacesTrunk(t *testing.T) {
	stack := startDialerStack(t)
	client := stack.connect(t, otherUser, otherWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-x"})
	payload := client.waitFor(WSEventError, nil)
	var errPayload ErrorPayload
	_ = json.Unmarshal(payload, &errPayload)
	if errPayload.Code != "trunk_unavailable" {
		t.Fatalf("error = %+v, want trunk_unavailable", errPayload)
	}
	select {
	case got := <-stack.provider.InvitedUsers:
		t.Fatalf("a foreign workspace made the trunk dial %q", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestDialerRejectsJoiningAWorkspaceWithoutCallingPermission(t *testing.T) {
	stack := startDialerStack(t)
	u, _ := url.Parse(stack.server.URL)
	u.Scheme = "ws"
	u.RawQuery = url.Values{"user": {otherUser}, "workspaceId": {dialerWorkspace}}.Encode()
	if _, res, err := websocket.DefaultDialer.Dial(u.String(), nil); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("joining another workspace's call session = %v, want 403", err)
	}
}

func TestDialerRingsTheMemberForAnInboundTrunkCallAndConnectsAudio(t *testing.T) {
	stack := startDialerStack(t)
	client := stack.connect(t, dialerUser, dialerWorkspace)
	dialed := stack.dialIn(t)

	offerRaw := client.waitFor(WSEventInboundCall, nil)
	var offer callsession.InboundCallOffer
	if err := json.Unmarshal(offerRaw, &offer); err != nil || offer.Channel != "sip" || offer.WorkspaceID != dialerWorkspace {
		t.Fatalf("incoming offer = %s", offerRaw)
	}
	client.send(WSEventInboundCallAccept, InboundCallActionPayload{OfferID: offer.OfferID})

	var remote *diago.DialogClientSession
	select {
	case remote = <-dialed:
	case <-time.After(8 * time.Second):
		t.Fatal("the inbound call was never answered")
	}
	client.waitFor(WSEventCallStatus, statusIs("answered"))
	client.sendSpeech(5)

	_ = remote.Hangup(context.Background())
	_ = remote.Close()
	client.waitFor(WSEventCallEnded, nil)
	deadline := time.Now().Add(3 * time.Second)
	for len(stack.manager.ActiveCalls(stack.trunk.ID)) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(stack.manager.ActiveCalls(stack.trunk.ID)) != 0 {
		t.Fatal("the inbound call is still tracked after the caller hung up")
	}
}

func TestDialerEndsTheCallInTheBrowserWhenTheCalledPartyHangsUp(t *testing.T) {
	stack := startDialerStack(t)
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		if err := d.Answer(); err != nil {
			return
		}
		go voiptest.SendRTP(d, 20)
		time.Sleep(500 * time.Millisecond)
		_ = d.Hangup(context.Background())
	})
	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-remote"})
	client.waitFor(WSEventCallStatus, statusIs("answered"))
	client.sendSpeech(3)
	client.waitFor(WSEventCallEnded, nil)
}

func TestDialerEndsTheCallInTheBrowserWhenTheFarEndGoesSilentWithoutBye(t *testing.T) {
	stack := startDialerStackWithMediaTimeout(t, 400*time.Millisecond)
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		if err := d.Answer(); err != nil {
			return
		}
		voiptest.SendRTP(d, 5)
		<-d.Context().Done()
	})
	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-silent"})
	client.waitFor(WSEventCallStatus, statusIs("answered"))
	client.waitFor(WSEventCallEnded, nil)
}
