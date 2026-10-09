package leadarea_repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
)

const (
	ws    = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	owner = "7d3e1f00-1111-4c2b-8f00-aa00bb00cc01"
	north = "11111111-1111-4111-8111-111111111111"
)

var at = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func square() leadarea.Area {
	return leadarea.Area{
		ID: north, WorkspaceID: ws, OwnerID: owner, Visibility: shared.VisibilityShared, Name: "Centro",
		Shape: geo.Shape{Kind: geo.ShapeRectangle, Ring: []geo.Point{
			{Lat: -23.57, Lng: -46.67}, {Lat: -23.57, Lng: -46.64}, {Lat: -23.55, Lng: -46.64}, {Lat: -23.55, Lng: -46.67},
		}},
		CreatedAt: at, UpdatedAt: at,
	}
}

func TestRingTextPutsLongitudeFirst(t *testing.T) {
	got := ringText(square().Ring())
	want := "((-46.67,-23.57),(-46.64,-23.57),(-46.64,-23.55),(-46.67,-23.55))"
	if got != want {
		t.Fatalf("ring = %q, want %q", got, want)
	}
}

func TestCreateWritesTheRingInTheSameStatement(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(numbered(`INSERT INTO lead_areas (id, workspace_id, owner_id, visibility, name, kind, shape, ring, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?::polygon, ?, ?)`)).
		WithArgs(north, ws, owner, "shared", "Centro", "rectangle", sqlmock.AnyArg(), "((-46.67,-23.57),(-46.64,-23.57),(-46.64,-23.55),(-46.67,-23.55))", at, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewRepository(db).Create(context.Background(), square()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAndDeleteTouchOnlyALiveAreaOfTheWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(numbered(`UPDATE lead_areas SET visibility = ?, name = ?, kind = ?, shape = ?::jsonb, ring = ?::polygon, updated_at = ? WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(numbered(`UPDATE lead_areas SET deleted_at = ?, updated_at = ? WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL`)).
		WithArgs(at, at, north, ws).
		WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewRepository(db)
	if err := repo.Update(context.Background(), square()); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("updating a gone area = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(context.Background(), ws, north, at); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("deleting a gone area = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindLiveBindsOneArrayAndSkipsMalformedIDs(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(numbered(`SELECT id, workspace_id, owner_id, visibility, name, kind, shape, created_at, updated_at FROM lead_areas WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND deleted_at IS NULL`)).
		WithArgs(ws, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewRepository(db)
	if _, err := repo.FindLive(context.Background(), ws, []string{north, "not-a-uuid"}); err != nil {
		t.Fatal(err)
	}
	if found, err := repo.FindLive(context.Background(), ws, []string{"not-a-uuid"}); err != nil || len(found) != 0 {
		t.Fatalf("only malformed ids query nothing: %v %v", found, err)
	}
	if _, err := repo.Get(context.Background(), ws, "not-a-uuid"); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("a malformed id = %v, want ErrNotFound without a query", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListReadableMirrorsTheSharedReadRule(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	query := `SELECT id, workspace_id, owner_id, visibility, name, kind, shape, created_at, updated_at FROM lead_areas WHERE workspace_id = ? AND deleted_at IS NULL AND (visibility = ? OR owner_id = ?) ORDER BY lower(name), id LIMIT ?`
	mock.ExpectQuery(numbered(query)).
		WithArgs(ws, "shared", owner, leadarea.MaxListed).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewRepository(db).ListReadable(context.Background(), ws, owner); err != nil {
		t.Fatal(err)
	}
	if strings.Count(query, "?") != 4 {
		t.Fatal("the list binds four values")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func numbered(query string) string {
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return regexp.QuoteMeta(b.String())
}

func TestCountLiveCountsTheLiveAreasOfTheWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM lead_areas WHERE workspace_id = $1 AND deleted_at IS NULL`)).
		WithArgs(ws).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))
	n, err := NewRepository(db).CountLive(context.Background(), ws)
	if err != nil || n != 42 {
		t.Fatalf("CountLive() = %d, %v", n, err)
	}
	if _, err := NewRepository(db).CountLive(context.Background(), "nope"); !errors.Is(err, leadarea.ErrWorkspaceRequired) {
		t.Fatalf("a malformed workspace = %v, want ErrWorkspaceRequired", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
