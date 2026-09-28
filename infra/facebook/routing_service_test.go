package facebook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"vozko/infra/meta/metatest"
)

func TestThreadOwnerAndTakeControl(t *testing.T) {
	var lastBody map[string]any
	var lastPath string
	svc, err := NewRoutingService(GraphConfig{HTTPClient: metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("recipient") != "psid" {
				t.Errorf("recipient = %s", r.URL.Query().Get("recipient"))
			}
			_, _ = w.Write([]byte(`{"data":[{"thread_owner":{"app_id":263902037430900}}]}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &lastBody)
		_, _ = w.Write([]byte(`{"success":true}`))
	})})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := svc.ThreadOwner(context.Background(), "page", "pt", "psid")
	if err != nil || owner != "263902037430900" {
		t.Fatalf("owner=%q err=%v", owner, err)
	}
	if err := svc.TakeControl(context.Background(), "page", "pt", "psid", "vozko:operator"); err != nil {
		t.Fatal(err)
	}
	if lastPath != "/v25.0/page/take_thread_control" || lastBody["metadata"] != "vozko:operator" {
		t.Fatalf("path=%s body=%v", lastPath, lastBody)
	}
}

func TestUnacknowledgedControlIsAnError(t *testing.T) {
	svc, _ := NewRoutingService(GraphConfig{HTTPClient: metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false}`))
	})})
	if err := svc.ReleaseControl(context.Background(), "page", "pt", "psid"); err == nil {
		t.Fatal("unacknowledged release reported as success")
	}
}
