package workflow_usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

func startSandboxSimulation(t *testing.T, wf *workflow.Workflow, mutate func(*WSWorkflowSimulationDeps)) *websocket.Conn {
	t.Helper()
	repo := NewMockWorkflowRepository()
	_ = repo.Create(wf)
	deps := WSWorkflowSimulationDeps{WorkflowRepo: repo, HTTPEgress: node_executors.HTTPEgress{Client: &http.Client{}}}
	if mutate != nil {
		mutate(&deps)
	}
	sim := NewWSWorkflowSimulationUseCase(deps)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = sim.HandleSession(context.Background(), conn, SimulationSession{WorkflowID: wf.ID, WorkspaceID: wf.WorkspaceID, Viewer: shared.Person{UserID: "u-1"}})
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func send(t *testing.T, conn *websocket.Conn, kind string, data interface{}) {
	t.Helper()
	if err := conn.WriteJSON(map[string]interface{}{"type": kind, "data": data}); err != nil {
		t.Fatalf("send %s: %v", kind, err)
	}
}

func menuSimulation(withFallback bool) *workflow.Workflow {
	edges := []workflow.Edge{
		{Source: "t", Target: "menu"},
		{Source: "menu", Target: "yes", Label: "sim"},
		{Source: "menu", Target: "end", Label: "nao"},
		{Source: "yes", Target: "end"},
	}
	if withFallback {
		edges = append(edges, workflow.Edge{Source: "menu", Target: "end", Label: "no_match"})
	}
	return &workflow.Workflow{
		ID: "wf-menu", WorkspaceID: "ws1", Name: "Menu", Type: workflow.WorkflowTypeMessages, Status: workflow.WorkflowStatusDraft,
		Graph: workflow.Graph{Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
			{ID: "menu", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
				"body": "Quer se matricular?", "buttons": []interface{}{map[string]interface{}{"id": "sim", "title": "Sim"}, map[string]interface{}{"id": "nao", "title": "Não"}},
			}},
			{ID: "yes", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "Ótimo, {{selected_option_title}}!"}},
			{ID: "end", Type: workflow.NodeTypeEnd},
		}, Edges: edges},
	}
}

type sentFrame struct {
	Text    string                `json:"text"`
	MsgType string                `json:"msgType"`
	Message *conversation.Message `json:"message"`
}

type rawSentFrame struct {
	Text    string          `json:"text"`
	MsgType string          `json:"msgType"`
	Message json.RawMessage `json:"message"`
}

func framesOf(frames []simFrame, kind string) []simFrame {
	var out []simFrame
	for _, f := range frames {
		if f.Type == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestTappingAButtonInTheTesterFollowsItsBranchAndEveryMessageIsTheCRMShape(t *testing.T) {
	conn := startSandboxSimulation(t, menuSimulation(true), nil)
	_, before := readUntil(t, conn, "waiting_trigger")
	send(t, conn, "reply", map[string]string{"text": "oi"})
	_, waiting := readUntil(t, conn, "waiting_reply")
	send(t, conn, "reply_selection", map[string]string{"kind": "button", "id": "sim", "title": "Sim"})
	done, rest := readUntil(t, conn, "run_completed")
	frames := append(append(before, waiting...), rest...)

	sent := framesOf(frames, "message_sent")
	if len(sent) != 2 {
		t.Fatalf("message_sent frames = %d", len(sent))
	}
	menu := decode[rawSentFrame](t, sent[0])
	var wire map[string]interface{}
	if err := json.Unmarshal(menu.Message, &wire); err != nil {
		t.Fatal(err)
	}
	meta, _ := wire["metadata"].(map[string]interface{})
	buttons, _ := meta["buttons"].(map[string]interface{})
	if menu.MsgType != "button" || wire["text"] != "Quer se matricular?" || buttons == nil || wire["messageType"] == nil {
		t.Fatalf("menu frame = %+v, wire = %v", menu, wire)
	}
	reply := decode[sentFrame](t, sent[1])
	if reply.Message == nil || reply.Message.Text != "Ótimo, Sim!" || reply.Text != "Ótimo, Sim!" {
		t.Fatalf("reply frame = %+v", reply)
	}

	received := framesOf(frames, "message_received")
	if len(received) != 2 {
		t.Fatalf("message_received frames = %d", len(received))
	}
	var tapped struct {
		Direction string               `json:"direction"`
		Message   conversation.Message `json:"message"`
	}
	if err := json.Unmarshal(received[1].Payload, &tapped); err != nil {
		t.Fatal(err)
	}
	if tapped.Direction != "inbound" || tapped.Message.Text != "Sim" {
		t.Fatalf("received = %+v", tapped)
	}

	vars := decode[statePayload](t, done).Vars
	if vars["selected_option_id"] != "sim" || vars["selected_option_type"] != "button" || vars["message_type"] != "text" {
		t.Fatalf("vars = %v", vars)
	}
}

func TestAReplyTheMenuCannotHandleKeepsWaitingLikeProduction(t *testing.T) {
	conn := startSandboxSimulation(t, menuSimulation(false), nil)
	readUntil(t, conn, "waiting_trigger")
	send(t, conn, "reply", map[string]string{"text": "oi"})
	readUntil(t, conn, "waiting_reply")

	send(t, conn, "reply_media", map[string]string{"kind": "image", "url": "https://cdn.example.com/rg.jpg", "caption": "meu RG"})
	ignored, _ := readUntil(t, conn, "reply_ignored")
	if got := decode[replyIgnoredPayload](t, ignored); got.NodeID != "menu" || got.Reason != "interactive_reply_unhandled" {
		t.Fatalf("reply_ignored = %+v", got)
	}

	send(t, conn, "reply_selection", map[string]string{"kind": "button", "id": "sim", "title": "Sim"})
	readUntil(t, conn, "run_completed")
}

func TestAMalformedTesterReplyIsRefusedAndTheSessionGoesOn(t *testing.T) {
	conn := startSandboxSimulation(t, menuSimulation(true), nil)
	readUntil(t, conn, "waiting_trigger")
	send(t, conn, "reply_selection", map[string]string{"kind": "carousel", "id": "x"})
	refused, _ := readUntil(t, conn, "command_refused")
	if got := decode[map[string]string](t, refused); got["command"] != "reply_selection" || got["error"] == "" {
		t.Fatalf("command_refused = %v", got)
	}
	send(t, conn, "reply_location", map[string]interface{}{"latitude": -23.55, "longitude": -46.63, "name": "Casa"})
	_, frames := readUntil(t, conn, "waiting_reply")
	received := framesOf(frames, "message_received")
	if len(received) != 1 || !strings.Contains(string(received[0].Payload), `"location"`) {
		t.Fatalf("the location reply = %v", received)
	}
}

func TestTheSimulationWithholdsAProductionPinItsEditorCannotSee(t *testing.T) {
	cases := []struct {
		name    string
		sources PinSources
		served  bool
	}{
		{"visible conversation", everyRunVisible{}, true},
		{"hidden conversation", noRunVisible{}, false},
		{"no way to check", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newCountingAPI(t)
			wf := debugWorkflow(api.server.URL, &workflow.NodePin{
				Output: map[string]interface{}{"ok": true}, Edge: "sucesso", Source: workflow.PinSourceOfRun("run-1"), PinnedAt: productionPinAt,
			})
			conn := startSandboxSimulation(t, wf, func(d *WSWorkflowSimulationDeps) { d.PinSources = tc.sources })

			started, _ := readUntil(t, conn, "sim_started")
			realCalls := decode[startedFrame](t, started).RealCallNodes
			last := "run_error"
			if tc.served {
				last = "run_completed"
			}
			_, frames := readUntil(t, conn, last)
			var login streamedStep
			var notice string
			for _, f := range frames {
				if f.Type != "node_executed" {
					continue
				}
				var s struct {
					streamedStep
					Notice string `json:"notice"`
				}
				_ = json.Unmarshal(f.Payload, &s)
				if s.NodeID == "login" {
					login, notice = s.streamedStep, s.Notice
				}
			}
			if api.hits.Load() != 0 {
				t.Fatal("no request may leave the test")
			}
			if tc.served {
				if !login.Pinned || notice != "" || len(realCalls) != 0 {
					t.Fatalf("login = %+v notice %q realCalls %v", login, notice, realCalls)
				}
				return
			}
			if login.Pinned || notice != workflow.PinWithheldNotice || login.ErrorKind != string(workflow.ErrorKindRealCallNotConfirmed) {
				t.Fatalf("login = %+v notice %q", login, notice)
			}
			if len(realCalls) != 1 || realCalls[0] != "login" {
				t.Fatalf("realCallNodes = %v, a withheld pin is no pin", realCalls)
			}
		})
	}
}
