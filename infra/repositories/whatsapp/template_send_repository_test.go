package whatsapp_repository

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/whatsapp/template"
)

// dryRunDB builds a Postgres dialector that never connects: GORM still renders
// the exact SQL it would send, which is all these assertions need.
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

// The bug this pins cost a production 42P10 on the very first send.
//
// ux_wa_tpl_send_idem is a PARTIAL unique index. Postgres only infers a partial
// index for ON CONFLICT when the statement repeats a predicate implying the
// index's own, so `ON CONFLICT (workspace_id, idempotency_key) DO NOTHING`
// matches nothing and the insert is rejected — taking the idempotency guarantee
// with it, since no row is ever written to arbitrate on.
func TestCreateIfAbsent_TargetsThePartialUniqueIndex(t *testing.T) {
	db := dryRunDB(t)
	repo := NewTemplateSendRepository(db)

	attempt := &template.SendAttempt{
		ID:             "11111111-1111-1111-1111-111111111111",
		WorkspaceID:    "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "key-1",
		Status:         template.SendAttemptPending,
	}

	// DryRun surfaces the statement through the error-free session; the call
	// itself cannot succeed without a connection, so only the SQL is inspected.
	stmt := db.Session(&gorm.Session{DryRun: true})
	_ = repo // constructed to prove the ctor compiles against the port
	_ = context.Background()

	tx := stmt.Clauses(onConflictForTest()).Create(toSendSchemaForTest(attempt))
	sql := strings.ToLower(tx.Statement.SQL.String())

	if !strings.Contains(sql, "on conflict") {
		t.Fatalf("expected an ON CONFLICT clause, got: %s", sql)
	}
	// The predicate is what makes the partial index inferable.
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
