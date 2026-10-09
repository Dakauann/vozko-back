package customfield_repository

import (
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/customfield"
	"vozko/infra/database/schema"
)

const (
	mockWorkspace  = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	mockDefinition = "11111111-1111-4111-8111-111111111111"
)

const (
	insertDefinitionSQL  = `INSERT INTO "custom_field_definitions" ("id","workspace_id","object_type","key","label","type","options","option_tones","required","sensitive","legal_basis","role","position","created_at","updated_at","deleted_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`
	updateDefinitionSQL  = `UPDATE "custom_field_definitions" SET "label"=$1,"legal_basis"=$2,"option_tones"=$3,"options"=$4,"position"=$5,"required"=$6,"role"=$7,"sensitive"=$8,"type"=$9,"updated_at"=$10 WHERE (id = $11 AND workspace_id = $12) AND "custom_field_definitions"."deleted_at" IS NULL`
	insertDefinitionArgs = 16
	updateDefinitionArgs = 12
)

func newMockRepository(t *testing.T) (customfield.Store, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return NewRepository(db), mock
}

func anyArgs(n int) []driver.Value {
	args := make([]driver.Value, n)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	return args
}

func roleDefinition() *customfield.Definition {
	return &customfield.Definition{
		ID:          mockDefinition,
		WorkspaceID: mockWorkspace,
		ObjectType:  customfield.ObjectLead,
		Key:         "classificacao",
		Label:       "Classificacao",
		Type:        customfield.TypeSelect,
		Options:     []string{"Quente", "Frio"},
		OptionTones: map[string]customfield.Tone{"Quente": customfield.ToneChart1},
		Role:        customfield.RoleClassification,
	}
}

func uniqueViolation(constraint string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: constraint}
}

func TestCreatePinsTheInsertAndItsPlaceholders(t *testing.T) {
	if got := countPlaceholders(insertDefinitionSQL); got != insertDefinitionArgs {
		t.Fatalf("insert has %d placeholders, want %d", got, insertDefinitionArgs)
	}
	repo, mock := newMockRepository(t)
	mock.ExpectExec(insertDefinitionSQL).WithArgs(anyArgs(insertDefinitionArgs)...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.Create(roleDefinition()); err != nil {
		t.Fatalf("Create = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateMapsUniqueViolationsByConstraint(t *testing.T) {
	cases := []struct {
		name       string
		constraint string
		want       error
	}{
		{"role index", schema.CustomFieldLiveRoleIndex, customfield.ErrRoleTaken},
		{"key index", schema.CustomFieldLiveKeyIndex, customfield.ErrKeyExists},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newMockRepository(t)
			mock.ExpectExec(insertDefinitionSQL).WithArgs(anyArgs(insertDefinitionArgs)...).WillReturnError(uniqueViolation(tc.constraint))
			if err := repo.Create(roleDefinition()); !errors.Is(err, tc.want) {
				t.Fatalf("Create = %v, want %v", err, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCreatePassesOtherErrorsThrough(t *testing.T) {
	repo, mock := newMockRepository(t)
	refused := &pgconn.PgError{Code: "23503", ConstraintName: "fk_custom_field_workspace"}
	mock.ExpectExec(insertDefinitionSQL).WithArgs(anyArgs(insertDefinitionArgs)...).WillReturnError(refused)
	err := repo.Create(roleDefinition())
	if errors.Is(err, customfield.ErrRoleTaken) || errors.Is(err, customfield.ErrKeyExists) || !errors.As(err, new(*pgconn.PgError)) {
		t.Fatalf("Create = %v, want the driver error", err)
	}
}

func TestUpdatePinsTheStatementAndItsPlaceholders(t *testing.T) {
	if got := countPlaceholders(updateDefinitionSQL); got != updateDefinitionArgs {
		t.Fatalf("update has %d placeholders, want %d", got, updateDefinitionArgs)
	}
	repo, mock := newMockRepository(t)
	mock.ExpectExec(updateDefinitionSQL).WithArgs(anyArgs(updateDefinitionArgs)...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.Update(roleDefinition()); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateMapsTheRoleIndexToRoleTaken(t *testing.T) {
	repo, mock := newMockRepository(t)
	mock.ExpectExec(updateDefinitionSQL).WithArgs(anyArgs(updateDefinitionArgs)...).WillReturnError(uniqueViolation(schema.CustomFieldLiveRoleIndex))
	if err := repo.Update(roleDefinition()); !errors.Is(err, customfield.ErrRoleTaken) {
		t.Fatalf("Update = %v, want ErrRoleTaken", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRefusesAMissingDefinition(t *testing.T) {
	repo, mock := newMockRepository(t)
	mock.ExpectExec(updateDefinitionSQL).WithArgs(anyArgs(updateDefinitionArgs)...).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.Update(roleDefinition()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update = %v, want ErrNotFound", err)
	}
}

func countPlaceholders(sql string) int {
	return len(regexp.MustCompile(`\$\d+`).FindAllString(sql, -1))
}
