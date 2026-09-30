package workflow_usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"vozko/domain/media"
)

type simFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func startVoiceSimulation(t *testing.T) *websocket.Conn {
	t.Helper()
	repo := NewMockWorkflowRepository()
	_ = repo.Create(voiceWorkflow("wf-1", "trunk-1", "greeting"))
	sim := NewWSWorkflowSimulationUseCase(WSWorkflowSimulationDeps{
		WorkflowRepo: repo,
		MediaRepo: audioMediaStub{items: map[string]*media.Media{
			"greeting": {ID: "greeting", WorkspaceID: "ws1", Type: media.MediaTypeAudio, URL: "https://files/greeting.mp3", Description: "Menu principal"},
		}},
	})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = sim.HandleSession(context.Background(), conn, "wf-1", "ws1")
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readUntil(t *testing.T, conn *websocket.Conn, want string) (simFrame, []simFrame) {
	t.Helper()
	var seen []simFrame
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var frame simFrame
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("waiting for %q, got %v after %d frames", want, err, len(seen))
		}
		if frame.Type == want {
			return frame, seen
		}
		seen = append(seen, frame)
	}
}

func TestTestingAVoiceWorkflowPlaysTheAudioAndAsksForAKey(t *testing.T) {
	conn := startVoiceSimulation(t)

	waiting, before := readUntil(t, conn, "waiting_key")
	var audio struct {
		MsgType  string `json:"msgType"`
		AudioURL string `json:"audioUrl"`
		Text     string `json:"text"`
	}
	for _, frame := range before {
		if frame.Type == "message_sent" {
			_ = json.Unmarshal(frame.Payload, &audio)
		}
	}
	if audio.MsgType != "audio" || audio.AudioURL != "https://files/greeting.mp3" {
		t.Fatalf("the panel did not receive the menu audio: %+v", audio)
	}
	var prompt waitingKeyPayload
	_ = json.Unmarshal(waiting.Payload, &prompt)
	if prompt.TimeoutSeconds <= 0 {
		t.Fatalf("prompt = %+v", prompt)
	}

	if err := conn.WriteJSON(map[string]interface{}{"type": "key", "data": map[string]string{"key": "1"}}); err != nil {
		t.Fatalf("send key: %v", err)
	}
	readUntil(t, conn, "run_completed")
}

func TestCancellingAVoiceTestHangsTheCallUp(t *testing.T) {
	conn := startVoiceSimulation(t)
	readUntil(t, conn, "waiting_key")
	if err := conn.WriteJSON(map[string]string{"type": "cancel"}); err != nil {
		t.Fatalf("send cancel: %v", err)
	}
	readUntil(t, conn, "run_cancelled")
}
