package container

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/infra/netguard"
)

func TestWorkflowHTTPNodesReachTheNetworkOnlyThroughTheGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	egress := workflowHTTPEgress()
	for name, client := range map[string]*http.Client{"main": egress.Client, "legacy tls": egress.LegacyTLS} {
		if client == nil {
			t.Fatalf("%s client is missing, so the node would refuse every request", name)
		}
		if _, err := client.Get(server.URL); !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Fatalf("%s client reached an internal address: %v", name, err)
		}
	}
}
