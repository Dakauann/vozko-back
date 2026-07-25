package gormmcp

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	domainmcp "vozko/domain/agent/mcp"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func TestBindingRepoUpsertWorkspaceRequired(t *testing.T) {
	r := NewBuiltinBindingRepo(nil)
	if err := r.Upsert(context.Background(), nil); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
	if err := r.Upsert(context.Background(), &domainmcp.BuiltinBinding{}); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
}

func TestBindingRepoGetNotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_builtin_bindings"`).WillReturnError(gorm.ErrRecordNotFound)
	r := NewBuiltinBindingRepo(db)
	if _, err := r.GetByID(context.Background(), "ws", "k"); !errors.Is(err, domainmcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
}

func TestBindingRepoGetOtherErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_builtin_bindings"`).WillReturnError(errors.New("x"))
	r := NewBuiltinBindingRepo(db)
	if _, err := r.GetByID(context.Background(), "ws", "k"); err == nil {
		t.Fatal()
	}
}

func TestBindingRepoDeleteNotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 0))
	r := NewBuiltinBindingRepo(db)
	if err := r.Delete(context.Background(), "ws", "k"); !errors.Is(err, domainmcp.ErrBindingNotFound) {
		t.Fatal(err)
	}
}

func TestBindingRepoDeleteErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnError(errors.New("x"))
	r := NewBuiltinBindingRepo(db)
	if err := r.Delete(context.Background(), "ws", "k"); err == nil {
		t.Fatal()
	}
}

func TestRemoteRepoCreateWorkspaceRequired(t *testing.T) {
	r := NewRemoteServerRepo(nil)
	if err := r.Create(context.Background(), nil); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
}

func TestRemoteRepoCreateDuplicate(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	r := NewRemoteServerRepo(db)
	s := &domainmcp.RemoteMCPServer{WorkspaceID: "ws", URL: "https://x"}
	if err := r.Create(context.Background(), s); !errors.Is(err, domainmcp.ErrDuplicate) {
		t.Fatal(err)
	}
}

func TestRemoteRepoCreateCountErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("x"))
	r := NewRemoteServerRepo(db)
	s := &domainmcp.RemoteMCPServer{WorkspaceID: "ws", URL: "https://x"}
	if err := r.Create(context.Background(), s); err == nil {
		t.Fatal()
	}
}

func TestRemoteRepoUpdateValidation(t *testing.T) {
	r := NewRemoteServerRepo(nil)
	if err := r.Update(context.Background(), nil); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
}

func TestRemoteRepoUpdateNotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 0))
	r := NewRemoteServerRepo(db)
	s := &domainmcp.RemoteMCPServer{ID: "id", WorkspaceID: "ws", URL: "https://x"}
	if err := r.Update(context.Background(), s); !errors.Is(err, domainmcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
}

func TestRemoteRepoUpdateErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnError(errors.New("x"))
	r := NewRemoteServerRepo(db)
	s := &domainmcp.RemoteMCPServer{ID: "id", WorkspaceID: "ws", URL: "https://x"}
	if err := r.Update(context.Background(), s); err == nil {
		t.Fatal()
	}
}

func TestRemoteRepoGetNotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_remote_servers"`).WillReturnError(gorm.ErrRecordNotFound)
	r := NewRemoteServerRepo(db)
	if _, err := r.Get(context.Background(), "ws", "id"); !errors.Is(err, domainmcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
}

func TestRemoteRepoGetOtherErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "mcp_remote_servers"`).WillReturnError(errors.New("x"))
	r := NewRemoteServerRepo(db)
	if _, err := r.Get(context.Background(), "ws", "id"); err == nil {
		t.Fatal()
	}
}

func TestRemoteRepoDeleteNotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 0))
	r := NewRemoteServerRepo(db)
	if err := r.Delete(context.Background(), "ws", "id"); !errors.Is(err, domainmcp.ErrRemoteServerNotFound) {
		t.Fatal(err)
	}
}

func TestRemoteRepoDeleteErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnError(errors.New("x"))
	r := NewRemoteServerRepo(db)
	if err := r.Delete(context.Background(), "ws", "id"); err == nil {
		t.Fatal()
	}
}

func TestToolCacheReplaceEmpty(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	r := NewToolCacheRepo(db)
	if err := r.Replace(context.Background(), "src", "ws", nil); err != nil {
		t.Fatal(err)
	}
}

func TestToolCacheReplaceDeleteErr(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE`).WillReturnError(errors.New("x"))
	mock.ExpectRollback()
	r := NewToolCacheRepo(db)
	if err := r.Replace(context.Background(), "src", "ws", nil); err == nil {
		t.Fatal()
	}
}

func TestToolCachePurge(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 0))
	r := NewToolCacheRepo(db)
	if err := r.Purge(context.Background(), "src", "ws"); err != nil {
		t.Fatal(err)
	}
}
