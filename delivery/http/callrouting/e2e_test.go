package callrouting

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/domain/callsession"
	"vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
	callrouting_infra "vozko/infra/callrouting"
	"vozko/infra/database/schema"
	"vozko/infra/holdmusic"
	callrouting_repository "vozko/infra/repositories/callrouting"
	"vozko/infra/repositories/repotest"
	callrouting_usecase "vozko/usecases/callrouting"
	callsession_usecase "vozko/usecases/callsession"
)

type departments map[string]string

func (d departments) GetDepartmentByID(id string) (*dept_domain.Department, error) {
	workspaceID, ok := d[id]
	if !ok {
		return nil, dept_domain.ErrDepartmentNotFound
	}
	return &dept_domain.Department{ID: id, WorkspaceID: workspaceID}, nil
}

type members map[string]string

func (m members) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	if m[userID] != workspaceID {
		return nil, nil
	}
	return &workspace.Member{WorkspaceID: workspaceID, UserID: userID}, nil
}

type nobodyOnline struct {
	callsession.CallSessionRegistry
}

func (nobodyOnline) ListAvailable(string) []callsession.CallSession { return nil }

func (nobodyOnline) ListPresence(string) []callsession.MemberPresence { return nil }

type everyoneAnswers struct{}

func (everyoneAnswers) MayAnswerCalls(string, string, string) bool { return true }

func allowAll(_ workspace.Resource, _ workspace.Action, next http.HandlerFunc) http.HandlerFunc {
	return next
}

type api struct {
	t      *testing.T
	server *httptest.Server
}

func startAPI(t *testing.T, workspaceID string) *api {
	t.Helper()
	db := repotest.IsolatedDB(t, "call_routing_http", &schema.CallQueue{}, &schema.CallRoutingSettings{}, &schema.CallTransfer{})
	library, err := holdmusic.NewLibrary(nil)
	if err != nil {
		t.Fatal(err)
	}
	queues := callrouting_repository.NewQueueRepository(db)
	dispatcher := callrouting_usecase.NewDispatcher(callrouting_usecase.DispatcherDeps{
		Sessions:   nobodyOnline{},
		Permission: everyoneAnswers{},
		Ringer:     callsession_usecase.NewInboundRinger(callsession_usecase.NewInboundOfferBroker()),
		Activity:   callrouting_infra.NewAgentActivity(),
		Members:    callrouting_usecase.StaticMembers{},
		Music:      library,
	})
	handler := NewHandler(HandlerDeps{
		Queues: callrouting_usecase.NewQueueCatalog(callrouting_usecase.QueueCatalogDeps{
			Queues:      queues,
			Departments: departments{"vendas": workspaceID, "suporte": uuid.NewString()},
			Members:     members{"ana": workspaceID, "bia": workspaceID, "zé": uuid.NewString()},
			Music:       library,
		}),
		Settings: callrouting_usecase.NewRoutingSettings(callrouting_repository.NewSettingsRepository(db), library),
		Targets:  callrouting_usecase.NewTransferTargets(queues, dispatcher),
		Monitor: callrouting_usecase.NewQueueMonitor(callrouting_usecase.QueueMonitorDeps{
			Queues: queues, Dispatcher: dispatcher, History: callrouting_repository.NewTransferLog(db),
		}),
		Music: library,
	})
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, handler, allowAll)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &api{t: t, server: server}
}

func (a *api) do(workspaceID, method, path string, body any) (int, []byte, http.Header) {
	a.t.Helper()
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	req, _ := http.NewRequest(method, a.server.URL+path, &payload)
	req.Header.Set("X-Workspace-ID", workspaceID)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(res.Body)
	return res.StatusCode, raw.Bytes(), res.Header
}

func TestQueuesAreManagedOverHTTP(t *testing.T) {
	workspaceID := uuid.NewString()
	a := startAPI(t, workspaceID)

	status, body, _ := a.do(workspaceID, http.MethodPost, "/call-queues", map[string]any{"name": "Suporte", "memberUserIds": []string{"ana", "bia"}})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, body)
	}
	var created QueueResponse
	_ = json.Unmarshal(body, &created)
	if created.Strategy != "longest_idle" || created.HoldMusic.PresetID != "piano_calmo" || created.RingSeconds != 15 {
		t.Fatalf("created = %+v", created)
	}

	for name, req := range map[string]map[string]any{
		"stranger":      {"name": "X", "memberUserIds": []string{"zé"}},
		"foreign dept":  {"name": "X", "departmentId": "suporte"},
		"unknown music": {"name": "X", "memberUserIds": []string{"ana"}, "holdMusic": map[string]string{"presetId": "heavy_metal"}},
		"bad strategy":  {"name": "X", "memberUserIds": []string{"ana"}, "strategy": "ringall"},
	} {
		if status, body, _ := a.do(workspaceID, http.MethodPost, "/call-queues", req); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d %s", name, status, body)
		}
	}

	update := map[string]any{"name": "Vendas", "departmentId": "vendas", "strategy": "round_robin", "ringSeconds": 20, "maxWaitSeconds": 120, "wrapUpSeconds": 5, "holdMusic": map[string]string{"presetId": "bossa_nova"}}
	if status, body, _ := a.do(workspaceID, http.MethodPut, "/call-queues/"+created.ID, update); status != http.StatusOK || !strings.Contains(string(body), `"presetId":"bossa_nova"`) {
		t.Fatalf("update = %d %s", status, body)
	}
	if status, _, _ := a.do(uuid.NewString(), http.MethodGet, "/call-queues/"+created.ID, nil); status != http.StatusNotFound {
		t.Fatalf("another workspace read the queue: %d", status)
	}

	status, body, _ = a.do(workspaceID, http.MethodGet, "/call-queues/transfer-targets", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"name":"Vendas","waiting":0,"ready":0`) {
		t.Fatalf("targets = %d %s", status, body)
	}

	if status, _, _ := a.do(workspaceID, http.MethodDelete, "/call-queues/"+created.ID, nil); status != http.StatusOK {
		t.Fatalf("delete = %d", status)
	}
	if _, body, _ := a.do(workspaceID, http.MethodGet, "/call-queues", nil); string(bytes.TrimSpace(body)) != "[]" {
		t.Fatalf("list after delete = %s", body)
	}
}

func TestHoldMusicIsChosenAndPreviewedOverHTTP(t *testing.T) {
	workspaceID := uuid.NewString()
	a := startAPI(t, workspaceID)

	if _, body, _ := a.do(workspaceID, http.MethodGet, "/call-routing/settings", nil); !strings.Contains(string(body), `"presetId":"piano_calmo"`) {
		t.Fatalf("default settings = %s", body)
	}
	if status, _, _ := a.do(workspaceID, http.MethodPut, "/call-routing/settings", map[string]any{"holdMusic": map[string]string{"presetId": "nope"}}); status != http.StatusBadRequest {
		t.Fatalf("unknown preset saved: %d", status)
	}
	if status, body, _ := a.do(workspaceID, http.MethodPut, "/call-routing/settings", map[string]any{"holdMusic": map[string]string{"presetId": "lofi"}}); status != http.StatusOK || !strings.Contains(string(body), "lofi") {
		t.Fatalf("save = %d %s", status, body)
	}

	_, body, _ := a.do(workspaceID, http.MethodGet, "/hold-music/presets", nil)
	var presets []HoldPresetResponse
	_ = json.Unmarshal(body, &presets)
	if len(presets) != 20 {
		t.Fatalf("presets = %d", len(presets))
	}
	status, wav, header := a.do(workspaceID, http.MethodGet, "/hold-music/presets/lofi/audio", nil)
	if status != http.StatusOK || header.Get("Content-Type") != "audio/wav" || string(wav[:4]) != "RIFF" || len(wav) < 8000*2*20 {
		t.Fatalf("preview = %d %s %d bytes", status, header.Get("Content-Type"), len(wav))
	}
	if status, _, _ := a.do(workspaceID, http.MethodGet, "/hold-music/presets/nope/audio", nil); status != http.StatusNotFound {
		t.Fatalf("unknown preview = %d", status)
	}
}

func TestManagersFollowQueuesLiveAndByDay(t *testing.T) {
	workspaceID := uuid.NewString()
	a := startAPI(t, workspaceID)
	status, body, _ := a.do(workspaceID, http.MethodPost, "/call-queues", map[string]any{"name": "Suporte", "memberUserIds": []string{"ana"}})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, body)
	}

	status, body, _ = a.do(workspaceID, http.MethodGet, "/call-queues/live", nil)
	var live []QueueLiveResponse
	_ = json.Unmarshal(body, &live)
	if status != http.StatusOK || len(live) != 1 || live[0].Name != "Suporte" || len(live[0].Waiting) != 0 || live[0].Counts.Offline != 1 || live[0].Agents[0].State != "offline" {
		t.Fatalf("live = %d %s", status, body)
	}

	status, body, _ = a.do(workspaceID, http.MethodGet, "/call-queues/stats?from=2026-09-30&to=2026-09-30", nil)
	var stats []QueueStatsResponse
	_ = json.Unmarshal(body, &stats)
	if status != http.StatusOK || len(stats) != 1 || stats[0].Offered != 0 || stats[0].ServiceLevelTargetSeconds != 20 {
		t.Fatalf("stats = %d %s", status, body)
	}
	for _, query := range []string{"", "?from=2026-09-30", "?from=2026-01-01&to=2026-09-30", "?from=2026-09-30&to=2026-09-01"} {
		if status, body, _ := a.do(workspaceID, http.MethodGet, "/call-queues/stats"+query, nil); status != http.StatusBadRequest {
			t.Errorf("stats%s = %d %s", query, status, body)
		}
	}
}
