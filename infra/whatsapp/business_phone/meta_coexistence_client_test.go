package whatsapp_business_phone

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/coexistence"
)

func TestCheckCoexistenceStatus_Coexistence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/PHONE_123" {
			t.Errorf("expected path /PHONE_123, got %s", r.URL.Path)
		}
		fields := r.URL.Query().Get("fields")
		if fields != "is_on_biz_app,platform_type" {
			t.Errorf("expected fields=is_on_biz_app,platform_type, got %s", fields)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %s", auth)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_on_biz_app": true,
			"platform_type": "CLOUD_API",
			"id":            "PHONE_123",
		})
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	status, err := client.CheckCoexistenceStatus("PHONE_123", "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.IsOnBizApp {
		t.Error("expected IsOnBizApp=true")
	}
	if status.PlatformType != "CLOUD_API" {
		t.Errorf("expected PlatformType=CLOUD_API, got %s", status.PlatformType)
	}
	if !status.IsCoexistence() {
		t.Error("expected IsCoexistence()=true")
	}
}

func TestCheckCoexistenceStatus_NotCoexistence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_on_biz_app": false,
			"platform_type": "CLOUD_API",
			"id":            "PHONE_456",
		})
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	status, err := client.CheckCoexistenceStatus("PHONE_456", "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.IsCoexistence() {
		t.Error("expected IsCoexistence()=false")
	}
}

func TestCheckCoexistenceStatus_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid phone number ID",
				"type":    "OAuthException",
				"code":    100,
			},
		})
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	_, err := client.CheckCoexistenceStatus("BAD_ID", "test-token")
	if err == nil {
		t.Fatal("expected error for bad request")
	}
}

func TestInitiateContactsSync_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/PHONE_123/smb_app_data" {
			t.Errorf("expected path /PHONE_123/smb_app_data, got %s", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type=application/json, got %s", ct)
		}

		var body coexistence.SyncRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.MessagingProduct != "whatsapp" {
			t.Errorf("expected messaging_product=whatsapp, got %s", body.MessagingProduct)
		}
		if body.SyncType != coexistence.SyncTypeContacts {
			t.Errorf("expected sync_type=%s, got %s", coexistence.SyncTypeContacts, body.SyncType)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messaging_product": "whatsapp",
			"request_id":        "contacts_req_001",
		})
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	resp, err := client.InitiateContactsSync("PHONE_123", "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RequestID != "contacts_req_001" {
		t.Errorf("expected request_id=contacts_req_001, got %s", resp.RequestID)
	}
}

func TestInitiateHistorySync_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/PHONE_123/smb_app_data" {
			t.Errorf("expected path /PHONE_123/smb_app_data, got %s", r.URL.Path)
		}

		var body coexistence.SyncRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.SyncType != coexistence.SyncTypeHistory {
			t.Errorf("expected sync_type=%s, got %s", coexistence.SyncTypeHistory, body.SyncType)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messaging_product": "whatsapp",
			"request_id":        "history_req_001",
		})
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	resp, err := client.InitiateHistorySync("PHONE_123", "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RequestID != "history_req_001" {
		t.Errorf("expected request_id=history_req_001, got %s", resp.RequestID)
	}
}

func TestInitiateHistorySync_SharingDeclined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Business declined to share messaging history",
				"code":    2593109,
			},
		})
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	_, err := client.InitiateHistorySync("PHONE_123", "test-token")
	if err == nil {
		t.Fatal("expected error for sharing declined")
	}

	errStr := err.Error()
	if !contains(errStr, "2593109") && !contains(errStr, "declined") {
		t.Errorf("error should mention declined: %s", errStr)
	}
}

func TestInitiateSync_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"Internal error","code":1}}`))
	}))
	defer srv.Close()

	client := &MetaCoexistenceClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}

	_, err := client.InitiateContactsSync("PHONE_123", "test-token")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
