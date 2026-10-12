package workflow_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/ai"
)

type scriptedConn struct {
	inbound  chan []byte
	outbound chan aiBuilderServerMsg
}

func newScriptedConn() *scriptedConn {
	return &scriptedConn{inbound: make(chan []byte, 4), outbound: make(chan aiBuilderServerMsg, 32)}
}

func (c *scriptedConn) ReadMessage() (int, []byte, error) {
	raw, ok := <-c.inbound
	if !ok {
		return 0, nil, errors.New("closed")
	}
	return 1, raw, nil
}

func (c *scriptedConn) WriteJSON(v interface{}) error {
	c.outbound <- v.(aiBuilderServerMsg)
	return nil
}

func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }

func (c *scriptedConn) send(t *testing.T, kind string, data interface{}) {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(aiBuilderClientMsg{Type: kind, Data: body})
	if err != nil {
		t.Fatal(err)
	}
	c.inbound <- raw
}

func (c *scriptedConn) next(t *testing.T, kind string) aiBuilderServerMsg {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-c.outbound:
			if msg.Type == kind {
				return msg
			}
		case <-timeout:
			t.Fatalf("no %q event arrived", kind)
		}
	}
}

type countingCatalogAI struct {
	catalogAI
	calls *atomic.Int32
}

func (c countingCatalogAI) GenerateStream(context.Context, ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	c.calls.Add(1)
	return nil, errors.New("the model must not be called")
}

func TestBuilderSessionRefusesAModelWithoutToolsOnChoiceAndBeforeTheRequest(t *testing.T) {
	calls := &atomic.Int32{}
	blind := builderBlindModel()
	blind.ID = "lab/blind"
	service := countingCatalogAI{catalogAI: catalogAI{models: []ai.ModelInfo{builderThinker(), blind}}, calls: calls}
	uc := NewAIBuilderUseCase(AIBuilderUseCaseDeps{AIService: service, Model: "lab/thinker"})
	conn := newScriptedConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = uc.HandleSession(ctx, conn, "", "ws1")
	}()
	conn.next(t, "builder_ready")

	conn.send(t, "set_model", setModelData{Model: "lab/blind"})
	chosen := conn.next(t, "done").Payload.(donePayload)
	if chosen.Valid || chosen.Code != ai.RefusalModelWithoutTools {
		t.Fatalf("set_model done = %+v, want a model_without_tools refusal", chosen)
	}

	conn.send(t, "user_prompt", promptData{Text: "crie um fluxo"})
	asked := conn.next(t, "done").Payload.(donePayload)
	if asked.Valid || asked.Code != ai.RefusalModelWithoutTools {
		t.Fatalf("prompt done = %+v, want a model_without_tools refusal", asked)
	}
	if calls.Load() != 0 {
		t.Fatalf("the model was called %d times although it has no tools", calls.Load())
	}

	close(conn.inbound)
	<-finished
}
