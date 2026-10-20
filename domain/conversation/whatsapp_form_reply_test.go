package conversation

import (
	"encoding/json"
	"testing"
)

func TestFormReplyIsReadFromAnNfmReply(t *testing.T) {
	m := &WhatsAppMessage{Type: "interactive", Interactive: &WhatsAppInteractive{Type: WhatsAppFormReplyType, NFMReply: &WhatsAppNFMReply{ResponseJSON: `{"a":1}`, Name: "flow"}}}
	reply, ok := m.FormReply()
	if !ok || reply.ResponseJSON != `{"a":1}` {
		t.Fatalf("reply = %+v %v", reply, ok)
	}
	for _, other := range []*WhatsAppMessage{nil, {}, {Interactive: &WhatsAppInteractive{Type: "button_reply", ButtonReply: &WhatsAppButtonReply{ID: "x"}}}} {
		if _, ok := other.FormReply(); ok {
			t.Fatalf("%+v is not a form reply", other)
		}
	}
}

func TestWithMetadataValueAddsAKeyToRecordedMetadata(t *testing.T) {
	merged, err := WithMetadataValue(json.RawMessage(`{"context":{"id":"x"}}`), "form", map[string]string{"kind": "response"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(merged, &fields); err != nil || string(fields["form"]) != `{"kind":"response"}` || len(fields["context"]) == 0 {
		t.Fatalf("merged = %s %v", merged, err)
	}
}
