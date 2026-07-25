package coexistence

import (
	"encoding/json"
	"testing"
)

func TestPhoneCoexistenceStatus_IsCoexistence(t *testing.T) {
	tests := []struct {
		name     string
		status   PhoneCoexistenceStatus
		expected bool
	}{
		{
			name:     "coexistence: on biz app + cloud api",
			status:   PhoneCoexistenceStatus{IsOnBizApp: true, PlatformType: "CLOUD_API"},
			expected: true,
		},
		{
			name:     "not coexistence: not on biz app",
			status:   PhoneCoexistenceStatus{IsOnBizApp: false, PlatformType: "CLOUD_API"},
			expected: false,
		},
		{
			name:     "not coexistence: on premise",
			status:   PhoneCoexistenceStatus{IsOnBizApp: true, PlatformType: "ON_PREMISE"},
			expected: false,
		},
		{
			name:     "not coexistence: both false",
			status:   PhoneCoexistenceStatus{IsOnBizApp: false, PlatformType: "ON_PREMISE"},
			expected: false,
		},
		{
			name:     "not coexistence: empty platform type",
			status:   PhoneCoexistenceStatus{IsOnBizApp: true, PlatformType: ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsCoexistence()
			if result != tt.expected {
				t.Errorf("IsCoexistence() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPhoneCoexistenceStatus_JSONParsing(t *testing.T) {
	raw := `{"is_on_biz_app":true,"platform_type":"CLOUD_API","id":"123456789"}`
	var status PhoneCoexistenceStatus
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !status.IsOnBizApp {
		t.Error("expected IsOnBizApp=true")
	}
	if status.PlatformType != "CLOUD_API" {
		t.Errorf("expected PlatformType=CLOUD_API, got %s", status.PlatformType)
	}
	if status.PhoneID != "123456789" {
		t.Errorf("expected PhoneID=123456789, got %s", status.PhoneID)
	}
	if !status.IsCoexistence() {
		t.Error("expected IsCoexistence()=true")
	}
}

func TestSyncRequest_JSONSerialization(t *testing.T) {
	req := SyncRequest{
		MessagingProduct: "whatsapp",
		SyncType:         SyncTypeHistory,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	expected := `{"messaging_product":"whatsapp","sync_type":"history"}`
	if string(data) != expected {
		t.Errorf("got %s, want %s", string(data), expected)
	}
}

func TestSyncRequest_ContactsType(t *testing.T) {
	req := SyncRequest{
		MessagingProduct: "whatsapp",
		SyncType:         SyncTypeContacts,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	expected := `{"messaging_product":"whatsapp","sync_type":"smb_app_state_sync"}`
	if string(data) != expected {
		t.Errorf("got %s, want %s", string(data), expected)
	}
}

func TestSyncResponse_JSONParsing(t *testing.T) {
	raw := `{"messaging_product":"whatsapp","request_id":"req_abc123"}`
	var resp SyncResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.MessagingProduct != "whatsapp" {
		t.Errorf("expected messaging_product=whatsapp, got %s", resp.MessagingProduct)
	}
	if resp.RequestID != "req_abc123" {
		t.Errorf("expected request_id=req_abc123, got %s", resp.RequestID)
	}
}

func TestHistoryWebhookPayload_FullParsing(t *testing.T) {
	raw := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "WABA_ID",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "5511999999999",
						"phone_number_id": "PHONE_ID"
					},
					"history": [{
						"metadata": {
							"phase": 0,
							"chunk_order": 1,
							"progress": 50
						},
						"threads": [{
							"id": "5511888888888",
							"messages": [{
								"from": "5511888888888",
								"id": "wamid.abc123",
								"timestamp": "1700000000",
								"type": "text",
								"status": "DELIVERED",
								"text": {"body": "Hello there!"}
							},{
								"from": "5511999999999",
								"to": "5511888888888",
								"id": "wamid.def456",
								"timestamp": "1700000060",
								"type": "text",
								"status": "READ",
								"text": {"body": "Hi! How can I help?"}
							},{
								"from": "5511888888888",
								"id": "wamid.img789",
								"timestamp": "1700000120",
								"type": "image",
								"status": "DELIVERED",
								"image": {"id": "media_123", "mime_type": "image/jpeg", "caption": "Photo"}
							}]
						}]
					}]
				},
				"field": "history"
			}]
		}]
	}`

	var payload HistoryWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if payload.Object != "whatsapp_business_account" {
		t.Errorf("expected object=whatsapp_business_account, got %s", payload.Object)
	}
	if len(payload.Entry) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(payload.Entry))
	}
	entry := payload.Entry[0]
	if entry.ID != "WABA_ID" {
		t.Errorf("expected entry id=WABA_ID, got %s", entry.ID)
	}
	if len(entry.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(entry.Changes))
	}
	change := entry.Changes[0]
	if change.Field != "history" {
		t.Errorf("expected field=history, got %s", change.Field)
	}
	if change.Value.Metadata.PhoneNumberID != "PHONE_ID" {
		t.Errorf("expected phone_number_id=PHONE_ID, got %s", change.Value.Metadata.PhoneNumberID)
	}
	if change.Value.Metadata.DisplayPhoneNumber != "5511999999999" {
		t.Errorf("expected display_phone_number=5511999999999, got %s", change.Value.Metadata.DisplayPhoneNumber)
	}

	if len(change.Value.History) != 1 {
		t.Fatalf("expected 1 history chunk, got %d", len(change.Value.History))
	}
	chunk := change.Value.History[0]
	if chunk.Metadata.Phase != 0 {
		t.Errorf("expected phase=0, got %d", chunk.Metadata.Phase)
	}
	if chunk.Metadata.ChunkOrder != 1 {
		t.Errorf("expected chunk_order=1, got %d", chunk.Metadata.ChunkOrder)
	}
	if chunk.Metadata.Progress != 50 {
		t.Errorf("expected progress=50, got %d", chunk.Metadata.Progress)
	}

	if len(chunk.Threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(chunk.Threads))
	}
	thread := chunk.Threads[0]
	if thread.ID != "5511888888888" {
		t.Errorf("expected thread id=5511888888888, got %s", thread.ID)
	}
	if len(thread.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(thread.Messages))
	}

	msg0 := thread.Messages[0]
	if msg0.From != "5511888888888" {
		t.Errorf("msg0: expected from=5511888888888, got %s", msg0.From)
	}
	if msg0.Type != "text" {
		t.Errorf("msg0: expected type=text, got %s", msg0.Type)
	}
	if msg0.Text == nil || msg0.Text.Body != "Hello there!" {
		t.Error("msg0: text body mismatch")
	}
	if msg0.Status != "DELIVERED" {
		t.Errorf("msg0: expected status=DELIVERED, got %s", msg0.Status)
	}

	msg1 := thread.Messages[1]
	if msg1.To != "5511888888888" {
		t.Errorf("msg1: expected to=5511888888888, got %s", msg1.To)
	}
	if msg1.Status != "READ" {
		t.Errorf("msg1: expected status=READ, got %s", msg1.Status)
	}

	msg2 := thread.Messages[2]
	if msg2.Type != "image" {
		t.Errorf("msg2: expected type=image, got %s", msg2.Type)
	}
	if msg2.Image == nil {
		t.Fatal("msg2: expected image content")
	}
	if msg2.Image.ID != "media_123" {
		t.Errorf("msg2: expected image id=media_123, got %s", msg2.Image.ID)
	}
	if msg2.Image.MimeType != "image/jpeg" {
		t.Errorf("msg2: expected mime_type=image/jpeg, got %s", msg2.Image.MimeType)
	}
	if msg2.Image.Caption != "Photo" {
		t.Errorf("msg2: expected caption=Photo, got %s", msg2.Image.Caption)
	}
}

func TestHistoryWebhookPayload_WithError(t *testing.T) {
	raw := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "WABA_ID",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "5511999999999",
						"phone_number_id": "PHONE_ID"
					},
					"history": [{
						"metadata": {"phase": 0, "chunk_order": 0, "progress": 0},
						"errors": [{
							"code": 2593109,
							"title": "History sharing declined",
							"message": "The business chose not to share their messaging history",
							"error_data": {"details": "User declined"}
						}]
					}]
				},
				"field": "history"
			}]
		}]
	}`

	var payload HistoryWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	chunk := payload.Entry[0].Changes[0].Value.History[0]
	if len(chunk.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(chunk.Errors))
	}
	histErr := chunk.Errors[0]
	if histErr.Code != HistorySharingDeclinedCode {
		t.Errorf("expected error code=%d, got %d", HistorySharingDeclinedCode, histErr.Code)
	}
}

func TestHistoryWebhookPayload_CompletedSync(t *testing.T) {
	raw := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "WABA_ID",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "5511999999999",
						"phone_number_id": "PHONE_ID"
					},
					"history": [{
						"metadata": {"phase": 2, "chunk_order": 5, "progress": 100},
						"threads": []
					}]
				},
				"field": "history"
			}]
		}]
	}`

	var payload HistoryWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	chunk := payload.Entry[0].Changes[0].Value.History[0]
	if chunk.Metadata.Progress != 100 {
		t.Errorf("expected progress=100, got %d", chunk.Metadata.Progress)
	}
	if chunk.Metadata.Phase != 2 {
		t.Errorf("expected phase=2, got %d", chunk.Metadata.Phase)
	}
}

func TestSMBStateSyncPayload_Parsing(t *testing.T) {
	raw := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "WABA_ID",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "5511999999999",
						"phone_number_id": "PHONE_ID"
					},
					"state_sync": [{
						"type": "contact",
						"contact": {
							"full_name": "John Doe",
							"first_name": "John",
							"phone_number": "5511777777777"
						},
						"action": "add",
						"metadata": {"timestamp": "1700000000"}
					}]
				},
				"field": "smb_app_state_sync"
			}]
		}]
	}`

	var payload SMBStateSyncWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(payload.Entry) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(payload.Entry))
	}
	change := payload.Entry[0].Changes[0]
	if change.Field != "smb_app_state_sync" {
		t.Errorf("expected field=smb_app_state_sync, got %s", change.Field)
	}
	if len(change.Value.StateSync) != 1 {
		t.Fatalf("expected 1 state_sync item, got %d", len(change.Value.StateSync))
	}
	item := change.Value.StateSync[0]
	if item.Type != "contact" {
		t.Errorf("expected type=contact, got %s", item.Type)
	}
	if item.Action != "add" {
		t.Errorf("expected action=add, got %s", item.Action)
	}
	if item.Contact.FullName != "John Doe" {
		t.Errorf("expected full_name=John Doe, got %s", item.Contact.FullName)
	}
	if item.Contact.PhoneNumber != "5511777777777" {
		t.Errorf("expected phone_number=5511777777777, got %s", item.Contact.PhoneNumber)
	}
}

func TestSMBMessageEchoesPayload_Parsing(t *testing.T) {
	raw := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "WABA_ID",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "5511999999999",
						"phone_number_id": "PHONE_ID"
					},
					"message_echoes": [{
						"from": "5511999999999",
						"to": "5511888888888",
						"id": "wamid.echo123",
						"timestamp": "1700000000",
						"type": "text",
						"text": {"body": "Thanks for contacting us!"}
					}]
				},
				"field": "smb_message_echoes"
			}]
		}]
	}`

	var payload SMBMessageEchoesWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	change := payload.Entry[0].Changes[0]
	if change.Field != "smb_message_echoes" {
		t.Errorf("expected field=smb_message_echoes, got %s", change.Field)
	}
	if len(change.Value.MessageEchoes) != 1 {
		t.Fatalf("expected 1 echo, got %d", len(change.Value.MessageEchoes))
	}
	echo := change.Value.MessageEchoes[0]
	if echo.From != "5511999999999" {
		t.Errorf("expected from=5511999999999, got %s", echo.From)
	}
	if echo.To != "5511888888888" {
		t.Errorf("expected to=5511888888888, got %s", echo.To)
	}
	if echo.Type != "text" {
		t.Errorf("expected type=text, got %s", echo.Type)
	}
	if echo.Text == nil || echo.Text.Body != "Thanks for contacting us!" {
		t.Error("text body mismatch")
	}
}

func TestHistoryMessage_AllMediaTypes(t *testing.T) {
	tests := []struct {
		name    string
		msgJSON string
		msgType string
	}{
		{
			name:    "video message",
			msgJSON: `{"from":"user","id":"wamid.v1","timestamp":"1700000000","type":"video","video":{"id":"vid_123","mime_type":"video/mp4","caption":"Video"}}`,
			msgType: "video",
		},
		{
			name:    "audio message",
			msgJSON: `{"from":"user","id":"wamid.a1","timestamp":"1700000000","type":"audio","audio":{"id":"aud_123","mime_type":"audio/ogg"}}`,
			msgType: "audio",
		},
		{
			name:    "document message",
			msgJSON: `{"from":"user","id":"wamid.d1","timestamp":"1700000000","type":"document","document":{"id":"doc_123","mime_type":"application/pdf","caption":"Invoice"}}`,
			msgType: "document",
		},
		{
			name:    "sticker message",
			msgJSON: `{"from":"user","id":"wamid.s1","timestamp":"1700000000","type":"sticker","sticker":{"id":"stk_123","mime_type":"image/webp"}}`,
			msgType: "sticker",
		},
		{
			name:    "location message",
			msgJSON: `{"from":"user","id":"wamid.l1","timestamp":"1700000000","type":"location","location":{"latitude":-23.55,"longitude":-46.63,"name":"São Paulo","address":"Brazil"}}`,
			msgType: "location",
		},
		{
			name:    "contacts message",
			msgJSON: `{"from":"user","id":"wamid.c1","timestamp":"1700000000","type":"contacts","contacts":[{"name":{"formatted_name":"Jane Doe","first_name":"Jane"},"phones":[{"phone":"5511666666666","type":"CELL"}]}]}`,
			msgType: "contacts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg HistoryMessage
			if err := json.Unmarshal([]byte(tt.msgJSON), &msg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if msg.Type != tt.msgType {
				t.Errorf("expected type=%s, got %s", tt.msgType, msg.Type)
			}
		})
	}
}

func TestSyncJobStatus_Values(t *testing.T) {
	statuses := []SyncJobStatus{
		SyncJobStatusPending, SyncJobStatusSyncing, SyncJobStatusCompleted,
		SyncJobStatusFailed, SyncJobStatusDeclined,
	}
	expected := []string{"pending", "syncing", "completed", "failed", "declined"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], string(s))
		}
	}
}

func TestConstants(t *testing.T) {
	if EventFinishCoexistence != "FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING" {
		t.Errorf("unexpected EventFinishCoexistence: %s", EventFinishCoexistence)
	}
	if HistorySharingDeclinedCode != 2593109 {
		t.Errorf("unexpected HistorySharingDeclinedCode: %d", HistorySharingDeclinedCode)
	}
	if SyncTypeContacts != "smb_app_state_sync" {
		t.Errorf("unexpected SyncTypeContacts: %s", SyncTypeContacts)
	}
	if SyncTypeHistory != "history" {
		t.Errorf("unexpected SyncTypeHistory: %s", SyncTypeHistory)
	}
}
