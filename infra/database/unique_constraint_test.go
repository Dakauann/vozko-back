package database

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func TestIsUniqueViolationOfNamesTheConstraint(t *testing.T) {
	const role = "ux_custom_field_ws_object_role_live"
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"postgres error on the constraint":       {fmt.Errorf("create: %w", &pgconn.PgError{Code: "23505", ConstraintName: role}), true},
		"postgres error on another constraint":   {&pgconn.PgError{Code: "23505", ConstraintName: "ux_custom_field_ws_object_key_live"}, false},
		"postgres error of another kind":         {&pgconn.PgError{Code: "23503", ConstraintName: role}, false},
		"driver text naming the constraint":      {errors.New(`ERROR: duplicate key value violates unique constraint "` + role + `" (SQLSTATE 23505)`), true},
		"driver text naming a longer constraint": {errors.New(`ERROR: duplicate key value violates unique constraint "` + role + `_old" (SQLSTATE 23505)`), false},
		"gorm duplicate without a name":          {gorm.ErrDuplicatedKey, false},
		"other failure":                          {errors.New("connection refused"), false},
		"no error":                               {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsUniqueViolationOf(tc.err, role); got != tc.want {
				t.Fatalf("IsUniqueViolationOf(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
