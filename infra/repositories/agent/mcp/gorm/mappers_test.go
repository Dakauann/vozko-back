package gormmcp

import (
	"reflect"
	"testing"
	"time"

	domainmcp "vozko/domain/agent/mcp"
)

func TestBindingRoundTrip(t *testing.T) {
	exp := time.Now().UTC().Truncate(time.Second)
	b := &domainmcp.BuiltinBinding{
		ID:          "id1",
		WorkspaceID: "ws",
		ServerKey:   "notion",
		DisplayName: "Notion",
		Status:      domainmcp.StatusConnected,
		Credential: &domainmcp.Credential{
			Mode:       domainmcp.AuthAPIKey,
			Cipher:     []byte{1, 2, 3},
			KEKVersion: 7,
			ExpiresAt:  &exp,
		},
		Metadata: map[string]any{"k": "v"},
	}
	row := bindingToRow(b)
	got := rowToBinding(&row)
	if got.ID != b.ID || got.WorkspaceID != b.WorkspaceID || got.ServerKey != b.ServerKey {
		t.Fatal(got)
	}
	if got.Credential.Mode != domainmcp.AuthAPIKey || got.Credential.KEKVersion != 7 {
		t.Fatalf("cred=%+v", got.Credential)
	}
	if !reflect.DeepEqual(got.Metadata, b.Metadata) {
		t.Fatalf("md=%v", got.Metadata)
	}
}

func TestBindingRoundTripNilCred(t *testing.T) {
	b := &domainmcp.BuiltinBinding{ID: "i", WorkspaceID: "ws", ServerKey: "k"}
	row := bindingToRow(b)
	got := rowToBinding(&row)
	if got.Credential != nil {
		t.Fatal("cred should be nil")
	}
}

func TestRemoteRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := &domainmcp.RemoteMCPServer{
		ID: "r1", WorkspaceID: "ws", Name: "x", URL: "https://x.test",
		Transport: domainmcp.TransportStreamableHTTP, Status: domainmcp.StatusConnected,
		LastListedAt: &now,
		Credential:   &domainmcp.Credential{Mode: domainmcp.AuthAPIKey, Cipher: []byte{9}, KEKVersion: 2},
	}
	row := remoteToRow(s)
	got := rowToRemote(&row)
	if got.ID != "r1" || got.Credential.Mode != domainmcp.AuthAPIKey {
		t.Fatal(got)
	}
}

func TestRemoteRoundTripNilCred(t *testing.T) {
	s := &domainmcp.RemoteMCPServer{ID: "r", WorkspaceID: "ws", Name: "n", URL: "https://x", Transport: domainmcp.TransportStreamableHTTP, Status: domainmcp.StatusPending}
	row := remoteToRow(s)
	got := rowToRemote(&row)
	if got.Credential != nil {
		t.Fatal()
	}
}

func TestToolRoundTrip(t *testing.T) {
	tl := domainmcp.CachedTool{
		SourceID: "builtin:notion", WorkspaceID: "ws", Name: "search", Title: "S",
		Description: "d", InputSchema: []byte(`{}`), Hash: "abc",
	}
	row := toolToRow(tl)
	got := rowToTool(&row)
	if got.Name != tl.Name || string(got.InputSchema) != `{}` || got.Hash != "abc" {
		t.Fatalf("%+v", got)
	}
}
