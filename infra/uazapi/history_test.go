package uazapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func historyServer(t *testing.T, handle func(body map[string]any) (int, string)) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message/find" || r.Method != http.MethodPost {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("token") != "inst-token" {
			t.Errorf("history must be read with the instance token, got %q", r.Header.Get("token"))
		}
		if r.Header.Get("admintoken") != "" {
			t.Error("the admin token must never reach the history path")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, body)
		status, payload := handle(body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestFindMessagesSweepsTheWholeInstanceByOffset(t *testing.T) {
	srv, requests := historyServer(t, func(map[string]any) (int, string) {
		return http.StatusOK, `{"messages":[{"messageid":"m1","chatid":"5511@s.whatsapp.net"},{"messageid":"m2","chatid":"5522@s.whatsapp.net"}],
			"hasMore":false,"limit":2,"offset":4,"nextOffset":6,"returnedMessages":2}`
	})

	page, err := NewClient(Config{}).FindMessages(context.Background(),
		uw.InstanceRef{BaseURL: srv.URL, Token: "inst-token"}, uw.FindMessagesInput{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("find messages: %v", err)
	}

	sent := (*requests)[0]
	if sent["limit"] != float64(2) || sent["offset"] != float64(4) {
		t.Errorf("request = %v", sent)
	}
	if _, ok := sent["chatid"]; ok {
		t.Error("the sweep must not narrow to a chat; without chatid the host returns the whole instance")
	}
	if page.Returned != 2 {
		t.Errorf("returned = %d, want 2", page.Returned)
	}
	if uw.ItemCount(page.Messages) != 2 {
		t.Errorf("messages = %s", page.Messages)
	}
}

func TestFindMessagesCountsWhatArrivedNotWhatTheHostClaims(t *testing.T) {
	srv, _ := historyServer(t, func(map[string]any) (int, string) {
		return http.StatusOK, `{"messages":[{"messageid":"m1"},{"messageid":"m2"},{"messageid":"m3"}],"hasMore":false,"returnedMessages":1}`
	})

	page, err := NewClient(Config{}).FindMessages(context.Background(),
		uw.InstanceRef{BaseURL: srv.URL, Token: "inst-token"}, uw.FindMessagesInput{Limit: 3})
	if err != nil {
		t.Fatalf("find messages: %v", err)
	}
	if page.Returned != 3 {
		t.Errorf("returned = %d; the page size must come from the items, hasMore and returnedMessages have both lied", page.Returned)
	}
}

func TestFindMessagesEmptyPage(t *testing.T) {
	srv, _ := historyServer(t, func(map[string]any) (int, string) {
		return http.StatusOK, `{"messages":null,"hasMore":false}`
	})

	page, err := NewClient(Config{}).FindMessages(context.Background(),
		uw.InstanceRef{BaseURL: srv.URL, Token: "inst-token"}, uw.FindMessagesInput{Limit: 50})
	if err != nil {
		t.Fatalf("find messages: %v", err)
	}
	if page.Returned != 0 || uw.HasItems(page.Messages) {
		t.Errorf("page = %+v", page)
	}
}

func TestFindMessagesSurfacesProviderErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		retryable bool
		reconnect bool
	}{
		{"cold gateway timeout", http.StatusGatewayTimeout, true, false},
		{"rate limited", http.StatusTooManyRequests, true, false},
		{"session gone", http.StatusUnauthorized, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := historyServer(t, func(map[string]any) (int, string) {
				return tc.status, `{"error":"nope"}`
			})
			_, err := NewClient(Config{}).FindMessages(context.Background(),
				uw.InstanceRef{BaseURL: srv.URL, Token: "inst-token"}, uw.FindMessagesInput{Limit: 1})
			provErr, ok := uw.AsProviderError(err)
			if !ok {
				t.Fatalf("err = %v, want a ProviderError", err)
			}
			if provErr.Retryable() != tc.retryable || provErr.NeedsReconnect() != tc.reconnect {
				t.Errorf("retryable=%v reconnect=%v", provErr.Retryable(), provErr.NeedsReconnect())
			}
		})
	}
}

func TestFindMessagesRefusesAPageLimitBelowOne(t *testing.T) {
	_, err := NewClient(Config{}).FindMessages(context.Background(),
		uw.InstanceRef{BaseURL: "http://unused", Token: "inst-token"}, uw.FindMessagesInput{Limit: 0})
	if err == nil {
		t.Error("a zero limit must be refused rather than sent")
	}
}
