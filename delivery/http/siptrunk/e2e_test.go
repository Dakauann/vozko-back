package siptrunk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
	workspace_domain "vozko/domain/workspace"
	voipinfra "vozko/infra/voip"
	"vozko/infra/voip/voiptest"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
)

const (
	workspaceA = "ws-a"
	workspaceB = "ws-b"
	allGrants  = "sip_trunks:create,sip_trunks:read,sip_trunks:update,sip_trunks:delete,sip_trunks:call"
)

type apiFixture struct {
	t        *testing.T
	server   *httptest.Server
	provider *voiptest.Provider
	manager  *voipinfra.SIPTrunkManager
}

func grantGuard(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		needed := string(resource) + ":" + string(action)
		for _, grant := range strings.Split(r.Header.Get("X-Test-Grants"), ",") {
			if grant == needed {
				next(w, r)
				return
			}
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}
}

func startAPI(t *testing.T) *apiFixture {
	t.Helper()
	provider := voiptest.StartProvider(t)
	repo := siptrunktest.NewMemoryRepository()
	manager, err := voipinfra.NewSIPTrunkManager(voipinfra.TrunkManagerConfig{
		SIPBindHost:     "127.0.0.1",
		SIPPortStart:    voiptest.FreeUDPPort(t),
		SIPPortCount:    1,
		RTPPortStart:    42000,
		RTPPortEnd:      42999,
		RegisterExpiry:  time.Minute,
		DialTimeout:     5 * time.Second,
		MediaTimeout:    30 * time.Second,
		MaxCallDuration: time.Hour,
		WatchInterval:   100 * time.Millisecond,
		PublicAddress:   "127.0.0.1",
		UserAgent:       "VozkoTest",

		AllowPrivateHosts: true,
	}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop() })

	handler := NewHandler(HandlerDeps{
		Create:    sip_trunk_usecase.NewCreateTrunkUseCase(repo, manager),
		Update:    sip_trunk_usecase.NewUpdateTrunkUseCase(repo, manager),
		Delete:    sip_trunk_usecase.NewDeleteTrunkUseCase(repo, manager),
		List:      sip_trunk_usecase.NewListTrunksUseCase(repo, manager),
		Get:       sip_trunk_usecase.NewGetTrunkUseCase(repo, manager),
		Hangup:    sip_trunk_usecase.NewHangupCallUseCase(repo, manager),
		ListCalls: sip_trunk_usecase.NewListCallsUseCase(repo, manager),
	})
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, handler, grantGuard)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &apiFixture{t: t, server: server, provider: provider, manager: manager}
}

type apiResponse struct {
	status int
	body   string
}

func (f *apiFixture) do(workspaceID, grants, method, path string, body any) apiResponse {
	f.t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			f.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, f.server.URL+path, &payload)
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("X-Test-Grants", grants)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(res.Body)
	return apiResponse{status: res.StatusCode, body: raw.String()}
}

func (r apiResponse) decode(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.body), into); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (f *apiFixture) createTrunk() TrunkResponse {
	f.t.Helper()
	res := f.do(workspaceA, allGrants, http.MethodPost, "/sip-trunks", map[string]any{
		"name":      "Main line",
		"host":      "127.0.0.1",
		"port":      f.provider.Port,
		"transport": "UDP",
		"username":  voiptest.Username,
		"password":  voiptest.Password,
		"settings": map[string]any{
			"bindHost":      "127.0.0.1",
			"publicAddress": "127.0.0.1",
		},
	})
	if res.status != http.StatusCreated {
		f.t.Fatalf("create trunk = %d %s", res.status, res.body)
	}
	if strings.Contains(res.body, voiptest.Password) {
		f.t.Fatal("the API echoed the SIP password")
	}
	var trunk TrunkResponse
	res.decode(f.t, &trunk)
	if !trunk.HasPassword {
		f.t.Fatal("hasPassword = false for a trunk created with a password")
	}
	deadline := time.Now().Add(5 * time.Second)
	for trunk.RegistrationStatus != "REGISTERED" {
		if time.Now().After(deadline) {
			f.t.Fatalf("trunk never registered, last status %s %s", trunk.RegistrationStatus, trunk.LastError)
		}
		time.Sleep(20 * time.Millisecond)
		f.do(workspaceA, allGrants, http.MethodGet, "/sip-trunks/"+trunk.ID, nil).decode(f.t, &trunk)
	}
	return trunk
}

func (f *apiFixture) activeCalls(workspaceID, trunkID string) []ActiveCallResponse {
	f.t.Helper()
	res := f.do(workspaceID, allGrants, http.MethodGet, "/sip-trunks/"+trunkID+"/calls", nil)
	if res.status != http.StatusOK {
		f.t.Fatalf("list calls = %d %s", res.status, res.body)
	}
	var calls []ActiveCallResponse
	res.decode(f.t, &calls)
	return calls
}

func (f *apiFixture) startCall(trunkID string) string {
	f.t.Helper()
	session, err := f.manager.Invite(context.Background(), trunkID, sip_trunk.TrunkInviteInput{PhoneNumber: "100"})
	if err != nil {
		f.t.Fatalf("engine Invite() error = %v", err)
	}
	return session.ID
}

func TestSIPTrunkAPIListsAndHangsUpTheTrunkCalls(t *testing.T) {
	f := startAPI(t)
	trunk := f.createTrunk()
	ended := make(chan struct{})
	f.provider.OnCall(voiptest.AnswerAndHold(ended))
	callID := f.startCall(trunk.ID)

	calls := f.activeCalls(workspaceA, trunk.ID)
	if len(calls) != 1 || calls[0].ID != callID || calls[0].AnsweredAt == nil || calls[0].Direction != "outbound" {
		t.Fatalf("active calls = %+v, want the answered call", calls)
	}
	if res := f.do(workspaceA, allGrants, http.MethodDelete, "/sip-trunks/"+trunk.ID+"/calls/"+callID, nil); res.status != http.StatusOK {
		t.Fatalf("hang up = %d %s", res.status, res.body)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider's leg stayed up after the API hangup")
	}
	if calls := f.activeCalls(workspaceA, trunk.ID); len(calls) != 0 {
		t.Fatalf("active calls after hangup = %+v", calls)
	}
}

func TestSIPTrunkAPIHasNoUnmeteredWayToPlaceACall(t *testing.T) {
	f := startAPI(t)
	trunk := f.createTrunk()
	res := f.do(workspaceA, allGrants, http.MethodPost, "/sip-trunks/"+trunk.ID+"/calls", map[string]string{"phoneNumber": "100"})
	if res.status != http.StatusMethodNotAllowed && res.status != http.StatusNotFound {
		t.Fatalf("POST calls = %d, want no REST dialling: calls go through the metered call session", res.status)
	}
}

func TestSIPTrunkAPIKeepsEveryTrunkAndCallInsideItsWorkspace(t *testing.T) {
	f := startAPI(t)
	trunk := f.createTrunk()
	f.provider.OnCall(voiptest.AnswerAndHold(nil))
	callID := f.startCall(trunk.ID)

	foreign := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/sip-trunks/" + trunk.ID, nil},
		{http.MethodPut, "/sip-trunks/" + trunk.ID, map[string]string{"name": "Hijacked"}},
		{http.MethodGet, "/sip-trunks/" + trunk.ID + "/calls", nil},
		{http.MethodDelete, "/sip-trunks/" + trunk.ID + "/calls/" + callID, nil},
		{http.MethodDelete, "/sip-trunks/" + trunk.ID, nil},
	}
	for _, req := range foreign {
		if res := f.do(workspaceB, allGrants, req.method, req.path, req.body); res.status != http.StatusNotFound {
			t.Errorf("%s %s from another workspace = %d %s, want 404", req.method, req.path, res.status, res.body)
		}
	}
	var listed []TrunkResponse
	f.do(workspaceB, allGrants, http.MethodGet, "/sip-trunks", nil).decode(t, &listed)
	if len(listed) != 0 {
		t.Fatalf("another workspace listed %d trunks", len(listed))
	}
	if calls := f.activeCalls(workspaceA, trunk.ID); len(calls) != 1 {
		t.Fatalf("the owner's call was affected by another workspace: %+v", calls)
	}
	var fetched TrunkResponse
	f.do(workspaceA, allGrants, http.MethodGet, "/sip-trunks/"+trunk.ID, nil).decode(t, &fetched)
	if fetched.Name != "Main line" {
		t.Fatalf("trunk name = %q after a foreign update", fetched.Name)
	}
}

func TestSIPTrunkAPIRequiresTheCallPermissionToHangUp(t *testing.T) {
	f := startAPI(t)
	trunk := f.createTrunk()
	f.provider.OnCall(voiptest.AnswerAndHold(nil))
	callID := f.startCall(trunk.ID)
	if res := f.do(workspaceA, "sip_trunks:read", http.MethodDelete, "/sip-trunks/"+trunk.ID+"/calls/"+callID, nil); res.status != http.StatusForbidden {
		t.Fatalf("hanging up without sip_trunks:call = %d, want 403", res.status)
	}
	if calls := f.activeCalls(workspaceA, trunk.ID); len(calls) != 1 {
		t.Fatal("a forbidden hangup ended the call")
	}
}

func TestSIPTrunkAPIRejectsInvalidTrunks(t *testing.T) {
	f := startAPI(t)
	cases := []map[string]any{
		{"name": "No host", "username": "u", "password": "p"},
		{"name": "TLS", "host": "sip.example.com", "transport": "TLS", "username": "u", "password": "p"},
		{"name": "No credentials", "host": "sip.example.com"},
		{"name": "Bad codec", "host": "sip.example.com", "username": "u", "password": "p", "settings": map[string]any{"codecs": []string{"G729"}}},
		{"name": "Opus only", "host": "sip.example.com", "username": "u", "password": "p", "settings": map[string]any{"codecs": []string{"opus"}}},
	}
	for _, body := range cases {
		if res := f.do(workspaceA, allGrants, http.MethodPost, "/sip-trunks", body); res.status != http.StatusBadRequest {
			t.Errorf("create %v = %d %s, want 400", body["name"], res.status, res.body)
		}
	}
}

func TestSIPTrunkAPIDeleteEndsCallsAndRemovesTheTrunk(t *testing.T) {
	f := startAPI(t)
	trunk := f.createTrunk()
	ended := make(chan struct{})
	f.provider.OnCall(voiptest.AnswerAndHold(ended))
	f.startCall(trunk.ID)
	if res := f.do(workspaceA, allGrants, http.MethodDelete, "/sip-trunks/"+trunk.ID, nil); res.status != http.StatusOK {
		t.Fatalf("delete trunk = %d %s", res.status, res.body)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("deleting the trunk left its call up")
	}
	if res := f.do(workspaceA, allGrants, http.MethodGet, "/sip-trunks/"+trunk.ID, nil); res.status != http.StatusNotFound {
		t.Fatalf("get deleted trunk = %d, want 404", res.status)
	}
	if _, ok := f.manager.TrunkStatus(trunk.ID); ok {
		t.Fatal("the engine kept a connection for a deleted trunk")
	}
}
