package gormmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainmcp "vozko/domain/agent/mcp"
)

func TestBindingRepoUpsertSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`INSERT INTO "mcp_builtin_bindings"`).WillReturnResult(sqlmock.NewResult(0, 1))
	r := NewBuiltinBindingRepo(db)
	exp := time.Now()
	b := &domainmcp.BuiltinBinding{
		ID: "i", WorkspaceID: "ws", ServerKey: "k", Status: domainmcp.StatusConnected,
		Credential: &domainmcp.Credential{Mode: domainmcp.AuthAPIKey, Cipher: []byte{1}, KEKVersion: 1, ExpiresAt: &exp},
		Metadata:   map[string]any{"a": 1},
	}
	if err := r.Upsert(context.Background(), b); err != nil {
		t.Fatal(err)
	}
}

func TestBindingRepoDeleteSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "mcp_builtin_bindings"`).WillReturnResult(sqlmock.NewResult(0, 1))
	r := NewBuiltinBindingRepo(db)
	if err := r.Delete(context.Background(), "ws", "k"); err != nil {
		t.Fatal(err)
	}
}

func TestBindingRepoGetSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_builtin_bindings"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "server_key", "status"}).AddRow("i", "ws", "k", "connected"))
	r := NewBuiltinBindingRepo(db)
	got, err := r.GetByID(context.Background(), "ws", "k")
	if err != nil || got.ID != "i" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestBindingRepoListByWorkspace(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_builtin_bindings"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "server_key"}).AddRow("i", "ws", "k"))
	r := NewBuiltinBindingRepo(db)
	rows, err := r.ListByWorkspace(context.Background(), "ws")
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %+v", err, rows)
	}
}

func TestBindingRepoListByWorkspaceErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_builtin_bindings"`).WillReturnError(errors.New("x"))
	r := NewBuiltinBindingRepo(db)
	if _, err := r.ListByWorkspace(context.Background(), "ws"); err == nil {
		t.Fatal()
	}
}

func TestRemoteRepoCreateSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectExec(`INSERT INTO "mcp_remote_servers"`).WillReturnResult(sqlmock.NewResult(0, 1))
	r := NewRemoteServerRepo(db)
	s := &domainmcp.RemoteMCPServer{ID: "id", WorkspaceID: "ws", Name: "n", URL: "https://x", Transport: domainmcp.TransportStreamableHTTP, Status: domainmcp.StatusPending}
	if err := r.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteRepoUpdateSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "mcp_remote_servers"`).WillReturnResult(sqlmock.NewResult(0, 1))
	r := NewRemoteServerRepo(db)
	s := &domainmcp.RemoteMCPServer{ID: "id", WorkspaceID: "ws", URL: "https://x"}
	if err := r.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteRepoGetSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_remote_servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "url"}).AddRow("id", "ws", "n", "https://x"))
	r := NewRemoteServerRepo(db)
	if _, err := r.Get(context.Background(), "ws", "id"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteRepoListByWorkspace(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_remote_servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "url"}).AddRow("id", "ws", "https://x"))
	r := NewRemoteServerRepo(db)
	rows, err := r.ListByWorkspace(context.Background(), "ws")
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %+v", err, rows)
	}
}

func TestRemoteRepoListByWorkspaceErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_remote_servers"`).WillReturnError(errors.New("x"))
	r := NewRemoteServerRepo(db)
	if _, err := r.ListByWorkspace(context.Background(), "ws"); err == nil {
		t.Fatal()
	}
}

func TestRemoteRepoDeleteSuccess(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "mcp_remote_servers"`).WillReturnResult(sqlmock.NewResult(0, 1))
	r := NewRemoteServerRepo(db)
	if err := r.Delete(context.Background(), "ws", "id"); err != nil {
		t.Fatal(err)
	}
}

func TestToolCacheReplaceWithTools(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "mcp_cached_tools"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO "mcp_cached_tools"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	r := NewToolCacheRepo(db)
	tools := []domainmcp.CachedTool{{SourceID: "src", WorkspaceID: "ws", Name: "n", InputSchema: []byte(`{}`), Hash: "h"}}
	if err := r.Replace(context.Background(), "src", "ws", tools); err != nil {
		t.Fatal(err)
	}
}

func TestToolCacheReplaceInsertErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "mcp_cached_tools"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO "mcp_cached_tools"`).WillReturnError(errors.New("x"))
	mock.ExpectRollback()
	r := NewToolCacheRepo(db)
	tools := []domainmcp.CachedTool{{SourceID: "src", WorkspaceID: "ws", Name: "n", InputSchema: []byte(`{}`), Hash: "h"}}
	if err := r.Replace(context.Background(), "src", "ws", tools); err == nil {
		t.Fatal()
	}
}

func TestToolCacheList(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_cached_tools"`).
		WillReturnRows(sqlmock.NewRows([]string{"source_id", "workspace_id", "name", "input_schema", "hash"}).AddRow("src", "ws", "n", []byte(`{}`), "h"))
	r := NewToolCacheRepo(db)
	rows, err := r.List(context.Background(), "src", "ws")
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %+v", err, rows)
	}
}

func TestToolCacheListErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_cached_tools"`).WillReturnError(errors.New("x"))
	r := NewToolCacheRepo(db)
	if _, err := r.List(context.Background(), "src", "ws"); err == nil {
		t.Fatal()
	}
}
