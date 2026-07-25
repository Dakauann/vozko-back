package conversation

import "testing"

// TestMessageTypeIsCallEvent locks which message types are call/permission
// markers. These MUST be excluded from AI/analysis history so a "Chamada
// recebida" log never reads to the model as something the lead said.
func TestMessageTypeIsCallEvent(t *testing.T) {
	events := []MessageType{
		MessageTypeCallReceived,
		MessageTypeCallAnswered,
		MessageTypeCallMissed,
		MessageTypeCallEnded,
		MessageTypeCallPermissionRequest,
		MessageTypeCallPermissionGranted,
		MessageTypeCallPermissionRejected,
	}
	for _, mt := range events {
		if !mt.IsCallEvent() {
			t.Errorf("%q should be a call event (excluded from AI history)", mt)
		}
	}

	conversational := []MessageType{
		MessageTypeUserMessage,
		MessageTypeAIResponse,
		MessageTypeOperator,
		MessageTypeTemplate,
		MessageTypeAudio,
		MessageTypeMedia,
		MessageTypeSystem,
		MessageTypeToolCall,
		MessageTypeToolResult,
	}
	for _, mt := range conversational {
		if mt.IsCallEvent() {
			t.Errorf("%q must NOT be a call event", mt)
		}
	}
}
