package mcp

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestNewBuiltinBinding(t *testing.T) {
	t.Run("requires workspace", func(t *testing.T) {
		_, err := NewBuiltinBinding("id", "", "notion", "Notion", "")
		if !errors.Is(err, ErrWorkspaceRequired) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("requires key", func(t *testing.T) {
		_, err := NewBuiltinBinding("id", "ws", "", "Notion", "")
		if !errors.Is(err, ErrServerKeyRequired) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("ok", func(t *testing.T) {
		fixed := time.Unix(1700000000, 0)
		orig := Now
		Now = func() time.Time { return fixed }
		t.Cleanup(func() { Now = orig })
		b, err := NewBuiltinBinding("id", "ws", "notion", "Notion", "")
		if err != nil {
			t.Fatal(err)
		}
		if b.Status != StatusPending {
			t.Fatalf("status=%s", b.Status)
		}
		if !b.CreatedAt.Equal(fixed) {
			t.Fatalf("createdAt=%v", b.CreatedAt)
		}
	})
}

func TestNewRemoteMCPServer(t *testing.T) {
	t.Run("requires workspace", func(t *testing.T) {
		_, err := NewRemoteMCPServer("id", "", "n", "https://x.com/mcp", "")
		if !errors.Is(err, ErrWorkspaceRequired) {
			t.Fatal(err)
		}
	})
	t.Run("requires name", func(t *testing.T) {
		_, err := NewRemoteMCPServer("id", "ws", "   ", "https://x.com/mcp", "")
		if !errors.Is(err, ErrNameRequired) {
			t.Fatal(err)
		}
	})
	t.Run("requires url", func(t *testing.T) {
		_, err := NewRemoteMCPServer("id", "ws", "n", "", "")
		if !errors.Is(err, ErrURLRequired) {
			t.Fatal(err)
		}
	})
	t.Run("invalid url", func(t *testing.T) {
		_, err := NewRemoteMCPServer("id", "ws", "n", "://", "")
		if !errors.Is(err, ErrURLRequired) {
			t.Fatal(err)
		}
	})
	t.Run("must be https", func(t *testing.T) {
		_, err := NewRemoteMCPServer("id", "ws", "n", "http://example.com/mcp", "")
		if !errors.Is(err, ErrURLNotHTTPS) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("https ok", func(t *testing.T) {
		s, err := NewRemoteMCPServer("id", "ws", "n", "https://x.com/mcp", "")
		if err != nil {
			t.Fatal(err)
		}
		if s.Transport != TransportStreamableHTTP {
			t.Fatalf("transport=%s", s.Transport)
		}
		if s.Status != StatusPending {
			t.Fatal("must be pending")
		}
	})
	t.Run("http localhost allowed in dev", func(t *testing.T) {
		t.Setenv("VOZKO_ENV", "development")
		_, err := NewRemoteMCPServer("id", "ws", "n", "http://localhost:9000/mcp", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewRemoteMCPServer("id", "ws", "n", "http://127.0.0.1/mcp", "")
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("http localhost rejected outside dev", func(t *testing.T) {
		os.Unsetenv("VOZKO_ENV")
		_, err := NewRemoteMCPServer("id", "ws", "n", "http://localhost/mcp", "")
		if !errors.Is(err, ErrURLNotHTTPS) {
			t.Fatalf("err=%v", err)
		}
	})
}
