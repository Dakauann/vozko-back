package whatsapp_repository

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/whatsapp/template"
)

func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "", DriverName: "pgx", WithoutReturning: false, Conn: nil}),
		&gorm.Config{DryRun: true},
	)
	if err != nil {
		t.Skipf("dry-run dialector unavailable: %v", err)
	}
	return db
}

func TestCreateIfAbsent_TargetsThePartialUniqueIndex(t *testing.T) {
	db := dryRunDB(t)
	repo := NewTemplateSendRepository(db)

	attempt := &template.SendAttempt{
		ID:             "11111111-1111-1111-1111-111111111111",
		WorkspaceID:    "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "key-1",
		Status:         template.SendAttemptPending,
	}

	stmt := db.Session(&gorm.Session{DryRun: true})
	_ = repo
	_ = context.Background()

	row := toSendSchema(attempt)
	tx := stmt.Clauses(sendAttemptConflictClause()).Create(&row)
	sql := strings.ToLower(tx.Statement.SQL.String())

	if !strings.Contains(sql, "on conflict") {
		t.Fatalf("expected an ON CONFLICT clause, got: %s", sql)
	}
	if !strings.Contains(sql, "idempotency_key is not null") ||
		!strings.Contains(sql, "deleted_at is null") {
		t.Fatalf(
			"ON CONFLICT must repeat ux_wa_tpl_send_idem's predicate or Postgres raises 42P10; got: %s",
			sql,
		)
	}
	if !strings.Contains(sql, "do nothing") {
		t.Fatalf("expected DO NOTHING so the loser of the race does not overwrite: %s", sql)
	}
}
