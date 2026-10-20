package database

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestAffectedOne(t *testing.T) {
	missing := errors.New("missing")
	boom := errors.New("boom")
	if err := AffectedOne(&gorm.DB{Error: boom}, missing); !errors.Is(err, boom) {
		t.Errorf("a query error wins: %v", err)
	}
	if err := AffectedOne(&gorm.DB{RowsAffected: 0}, missing); !errors.Is(err, missing) {
		t.Errorf("no row affected is not found: %v", err)
	}
	if err := AffectedOne(&gorm.DB{RowsAffected: 1}, missing); err != nil {
		t.Errorf("one row affected: %v", err)
	}
}
