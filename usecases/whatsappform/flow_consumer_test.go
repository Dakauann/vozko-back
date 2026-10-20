package whatsappform_usecase

import (
	"encoding/json"
	"errors"
	"testing"

	wf "vozko/domain/whatsappform"
	webhook_usecase "vozko/usecases/webhook"
)

func TestFlowEventsDedupOnTheirBodyAndDropUnreadablePayloads(t *testing.T) {
	a, b := json.RawMessage(`{"entry":[1]}`), json.RawMessage(`{"entry":[2]}`)
	if flowDedupKey(&a) == flowDedupKey(&b) || flowDedupKey(&a) != flowDedupKey(&a) {
		t.Fatal("the dedup key follows the body")
	}
	if classifyFlowFailure(wf.ErrFlowEventInvalid) != webhook_usecase.DispositionDrop || classifyFlowFailure(errors.New("db")) != webhook_usecase.DispositionRetry {
		t.Fatal("unreadable payloads are dropped, everything else retried")
	}
}
