package shortlink

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

func domainLink() *shortlink.ShortLink {
	now := time.Now()
	return &shortlink.ShortLink{
		ID:           "id-1",
		WorkspaceID:  "ws-1",
		DepartmentID: "dep-1",
		CreatedBy:    "user-1",
		Code:         "abc123",
		TargetURL:    "https://example.com",
		RedirectType: shortlink.RedirectTemporary,
		Status:       shortlink.LinkStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func fullRow() *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows(shortLinkColumns).AddRow(
		"id-1", "ws-1", "dep-1", "user-1", "abc123", "https://example.com",
		"Title", "302", "active", "hash", nil, nil,
		int64(3), int64(2), nil, now, now, nil,
	)
}

func nullRow() *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows(shortLinkColumns).AddRow(
		"id-2", "ws-1", nil, nil, "def456", "https://example.org",
		"", "301", "active", "", nil, nil,
		int64(0), int64(0), nil, now, now, nil,
	)
}

func TestShortLinkRepo_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`INSERT INTO "short_links"`).WillReturnResult(sqlmock.NewResult(1, 1))
		if err := NewShortLinkRepository(db).Create(context.Background(), domainLink()); err != nil {
			t.Fatalf("create: %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`INSERT INTO "short_links"`).WillReturnError(errors.New("db"))
		if err := NewShortLinkRepository(db).Create(context.Background(), domainLink()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
		if err := NewShortLinkRepository(db).Update(context.Background(), domainLink()); err != nil {
			t.Fatalf("update: %v", err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET`).WillReturnResult(sqlmock.NewResult(0, 0))
		if err := NewShortLinkRepository(db).Update(context.Background(), domainLink()); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET`).WillReturnError(errors.New("db"))
		if err := NewShortLinkRepository(db).Update(context.Background(), domainLink()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_SoftDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET "deleted_at"`).WillReturnResult(sqlmock.NewResult(0, 1))
		if err := NewShortLinkRepository(db).SoftDelete(context.Background(), "ws", "id"); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET "deleted_at"`).WillReturnResult(sqlmock.NewResult(0, 0))
		if err := NewShortLinkRepository(db).SoftDelete(context.Background(), "ws", "id"); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET "deleted_at"`).WillReturnError(errors.New("db"))
		if err := NewShortLinkRepository(db).SoftDelete(context.Background(), "ws", "id"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_FindByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnRows(fullRow())
		link, err := NewShortLinkRepository(db).FindByID(context.Background(), "ws-1", "id-1")
		if err != nil || link.DepartmentID != "dep-1" || link.CreatedBy != "user-1" || !link.HasPassword {
			t.Fatalf("find = %v %+v", err, link)
		}
	})
	t.Run("not found", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnRows(sqlmock.NewRows(shortLinkColumns))
		if _, err := NewShortLinkRepository(db).FindByID(context.Background(), "ws", "id"); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).FindByID(context.Background(), "ws", "id"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_FindByCode(t *testing.T) {
	t.Run("found null fields", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnRows(nullRow())
		link, err := NewShortLinkRepository(db).FindByCode(context.Background(), "def456")
		if err != nil || link.DepartmentID != "" || link.CreatedBy != "" {
			t.Fatalf("find = %v %+v", err, link)
		}
	})
	t.Run("not found", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnRows(sqlmock.NewRows(shortLinkColumns))
		if _, err := NewShortLinkRepository(db).FindByCode(context.Background(), "x"); err != shortlink.ErrShortLinkNotFound {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).FindByCode(context.Background(), "x"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_CodeExists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		exists, err := NewShortLinkRepository(db).CodeExists(context.Background(), "abc")
		if err != nil || !exists {
			t.Fatalf("exists = %v %v", exists, err)
		}
	})
	t.Run("absent", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		exists, _ := NewShortLinkRepository(db).CodeExists(context.Background(), "abc")
		if exists {
			t.Fatal("should not exist")
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).CodeExists(context.Background(), "abc"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_List(t *testing.T) {
	dep := "dep-1"
	t.Run("with department", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnRows(fullRow())
		res, err := NewShortLinkRepository(db).ListByWorkspace(context.Background(), "ws-1", &dep, shared.Pagination{Page: 1, PageSize: 20})
		if err != nil || res.TotalItems != 1 || len(res.Items) != 1 {
			t.Fatalf("list = %v %+v", err, res)
		}
	})
	t.Run("no department", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnRows(sqlmock.NewRows(shortLinkColumns))
		res, err := NewShortLinkRepository(db).ListByWorkspace(context.Background(), "ws-1", nil, shared.Pagination{})
		if err != nil || res.TotalItems != 0 {
			t.Fatalf("list = %v %+v", err, res)
		}
	})
	t.Run("count error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).ListByWorkspace(context.Background(), "ws", nil, shared.Pagination{}); err == nil {
			t.Fatal("expected count error")
		}
	})
	t.Run("find error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(`SELECT \* FROM "short_links"`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).ListByWorkspace(context.Background(), "ws", nil, shared.Pagination{}); err == nil {
			t.Fatal("expected find error")
		}
	})
}

func TestShortLinkRepo_CountAndSum(t *testing.T) {
	t.Run("count success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
		n, err := NewShortLinkRepository(db).CountByWorkspace(context.Background(), "ws")
		if err != nil || n != 7 {
			t.Fatalf("count = %v %d", err, n)
		}
	})
	t.Run("count error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).CountByWorkspace(context.Background(), "ws"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("sum success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT COALESCE\(SUM`).WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(42))
		sum, err := NewShortLinkRepository(db).SumClicksByWorkspace(context.Background(), "ws")
		if err != nil || sum != 42 {
			t.Fatalf("sum = %v %d", err, sum)
		}
	})
	t.Run("sum error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`SELECT COALESCE\(SUM`).WillReturnError(errors.New("db"))
		if _, err := NewShortLinkRepository(db).SumClicksByWorkspace(context.Background(), "ws"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortLinkRepo_ApplyClick(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
		if err := NewShortLinkRepository(db).ApplyClick(context.Background(), "id", 1, time.Now()); err != nil {
			t.Fatalf("apply: %v", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		mock.ExpectExec(`UPDATE "short_links" SET`).WillReturnError(errors.New("db"))
		if err := NewShortLinkRepository(db).ApplyClick(context.Background(), "id", 0, time.Now()); err == nil {
			t.Fatal("expected error")
		}
	})
}
