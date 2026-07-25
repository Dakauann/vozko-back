package memory

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/agent/mcp"
)

func TestBuiltinBindingRepoCRUD(t *testing.T) {
	r := NewBuiltinBindingRepo()
	ctx := context.Background()

	if err := r.Upsert(ctx, nil); !errors.Is(err, mcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
	if err := r.Upsert(ctx, &mcp.BuiltinBinding{}); !errors.Is(err, mcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
	b := &mcp.BuiltinBinding{ID: "id", WorkspaceID: "ws", ServerKey: "k"}
	if err := r.Upsert(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetByID(ctx, "ws", "id")
	if err != nil {
		t.Fatal(err)
	}
	if got == b {
		t.Fatal("must return copy")
	}
	if _, err := r.GetByID(ctx, "ws", "missing"); !errors.Is(err, mcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
	if _, err := r.GetByID(ctx, "missing", "id"); !errors.Is(err, mcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
	list, _ := r.ListByWorkspace(ctx, "ws")
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	if l, _ := r.ListByWorkspace(ctx, "missing"); len(l) != 0 {
		t.Fatal("expected empty")
	}
	if err := r.Delete(ctx, "ws", "missing"); !errors.Is(err, mcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "missing", "id"); !errors.Is(err, mcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "ws", "id"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetByID(ctx, "ws", "id"); !errors.Is(err, mcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
}

func TestRemoteServerRepoCRUD(t *testing.T) {
	r := NewRemoteServerRepo()
	ctx := context.Background()

	if err := r.Create(ctx, nil); !errors.Is(err, mcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
	s := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: "https://x"}
	if err := r.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, &mcp.RemoteMCPServer{ID: "b", WorkspaceID: "ws", URL: "https://x"}); !errors.Is(err, mcp.ErrDuplicate) {
		t.Fatal(err)
	}
	got, err := r.Get(ctx, "ws", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got == s {
		t.Fatal("must copy")
	}
	if _, err := r.Get(ctx, "ws", "missing"); !errors.Is(err, mcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, "missing", "a"); !errors.Is(err, mcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
	if err := r.Update(ctx, nil); !errors.Is(err, mcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
	if err := r.Update(ctx, &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "missing"}); !errors.Is(err, mcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
	if err := r.Update(ctx, &mcp.RemoteMCPServer{ID: "missing", WorkspaceID: "ws"}); !errors.Is(err, mcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
	s.Status = mcp.StatusConnected
	if err := r.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	g2, _ := r.Get(ctx, "ws", "a")
	if g2.Status != mcp.StatusConnected {
		t.Fatal("update did not persist")
	}
	list, _ := r.ListByWorkspace(ctx, "ws")
	if len(list) != 1 {
		t.Fatal("list len")
	}
	if l, _ := r.ListByWorkspace(ctx, "missing"); len(l) != 0 {
		t.Fatal("expected empty list")
	}
	if err := r.Delete(ctx, "ws", "missing"); !errors.Is(err, mcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "missing", "a"); !errors.Is(err, mcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "ws", "a"); err != nil {
		t.Fatal(err)
	}
}

func TestToolCacheRepo(t *testing.T) {
	r := NewToolCacheRepo()
	ctx := context.Background()
	tools := []mcp.CachedTool{{SourceID: "s", WorkspaceID: "ws", Name: "n"}}
	if err := r.Replace(ctx, "s", "ws", tools); err != nil {
		t.Fatal(err)
	}
	got, _ := r.List(ctx, "s", "ws")
	if len(got) != 1 || got[0].Name != "n" {
		t.Fatalf("%+v", got)
	}
	if err := r.Purge(ctx, "s", "ws"); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.List(ctx, "s", "ws")
	if len(got2) != 0 {
		t.Fatal("must be empty after purge")
	}
}
