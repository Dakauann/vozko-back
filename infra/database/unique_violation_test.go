package database

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"
)

func TestIsUniqueViolation(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"gorm duplicate":         {fmt.Errorf("create: %w", gorm.ErrDuplicatedKey), true},
		"postgres duplicate key": {errors.New(`ERROR: duplicate key value violates unique constraint "ux_cm_entry_external_msgid" (SQLSTATE 23505)`), true},
		"sqlstate only":          {errors.New("pq: 23505"), true},
		"other failure":          {errors.New("connection refused"), false},
		"no error":               {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
