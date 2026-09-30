package unofficial_whatsapp

import (
	"encoding/json"
	"testing"
)

func batchedHistoryBody(t *testing.T, extra map[string]any) []byte {
	t.Helper()
	body := map[string]any{
		"EventType":          "history",
		"BaseUrl":            "https://host.test",
		"instanceName":       "vozko-dev",
		"owner":              "5584999999999",
		"token":              "secret",
		"batchNumber":        2,
		"batchTotal":         "5",
		"batchChunkOrder":    1,
		"batchHistoryStatus": "INITIAL_BOOTSTRAP",
	}
	for k, v := range extra {
		body[k] = v
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return encoded
}

func TestBatchedHistoryDecodesItsMessagesArray(t *testing.T) {
	body := batchedHistoryBody(t, map[string]any{
		"messages": []map[string]any{
			{"messageid": "m1", "chatid": "5511111111111@s.whatsapp.net", "text": "oi", "messageTimestamp": 1759230000000},
			{"messageid": "m2", "chatid": "5511111111111@s.whatsapp.net", "fromMe": true, "text": "olá", "messageTimestamp": 1759230060000},
		},
	})

	env, err := DecodeEnvelope(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	events := NormalizeEnvelope("inst-1", env)

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for _, ev := range events {
		if !ev.Backfill {
			t.Errorf("event %s is not marked backfill", ev.ProviderMessageID)
		}
		if ev.ChatID != "5511111111111@s.whatsapp.net" {
			t.Errorf("chat id = %q", ev.ChatID)
		}
	}
	if events[0].Kind != EventInboundMessage || events[1].Kind != EventOutboundFromDevice {
		t.Errorf("kinds = %s, %s", events[0].Kind, events[1].Kind)
	}
}

func TestBatchedHistoryCarriesItsProgress(t *testing.T) {
	env, err := DecodeEnvelope(batchedHistoryBody(t, map[string]any{"messages": []any{}}))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Batch.Number != 2 || env.Batch.Total != 5 || env.Batch.Status != "INITIAL_BOOTSTRAP" {
		t.Errorf("batch = %+v", env.Batch)
	}
}

func TestBatchedHistoryWithoutMessagesYieldsNothingInsteadOfAPhantomMessage(t *testing.T) {
	for _, key := range []string{"chats", "labels"} {
		t.Run(key, func(t *testing.T) {
			body := batchedHistoryBody(t, map[string]any{key: []map[string]any{{"id": "x"}}})
			env, err := DecodeEnvelope(body)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if events := NormalizeEnvelope("inst-1", env); len(events) != 0 {
				t.Errorf("a %s-only batch produced %d events; the whole body was read as a message", key, len(events))
			}
		})
	}
}

func TestHistoryEnvelopeWrapsAPolledPageAsBackfill(t *testing.T) {
	page := json.RawMessage(`[{"messageid":"m9","chatid":"5511222222222@s.whatsapp.net","text":"antigo","messageTimestamp":1759230000000}]`)

	events := NormalizeEnvelope("inst-1", HistoryEnvelope(page))

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if !events[0].Backfill || events[0].Kind != EventInboundMessage {
		t.Errorf("event = %+v", events[0])
	}
	if !events[0].IsHistoryImport() {
		t.Error("a polled page must be recognised as a history import")
	}
}

func TestLiveMessagesAreNotHistoryImports(t *testing.T) {
	body := []byte(`{"EventType":"messages","message":{"messageid":"m1","chatid":"5511@s.whatsapp.net","text":"oi"}}`)
	env, err := DecodeEnvelope(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.IsHistory() {
		t.Error("a live messages envelope must not be treated as history")
	}
}

func TestItemHelpersDescribeAProviderArray(t *testing.T) {
	data := json.RawMessage(`[{"key":{"id":"x"}},{"key":{"id":"y"}}]`)

	if !HasItems(data) || ItemCount(data) != 2 {
		t.Errorf("count = %d", ItemCount(data))
	}
	if keys := DescribeUnknownBody(FirstItem(data)); len(keys) != 1 || keys[0] != "key" {
		t.Errorf("first item keys = %v", keys)
	}
	if HasItems(json.RawMessage(`{"id":"x"}`)) || HasItems(nil) {
		t.Error("an object or nothing is not a list of items")
	}
}
