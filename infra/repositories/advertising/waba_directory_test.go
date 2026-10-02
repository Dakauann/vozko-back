package advertising_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	ads "vozko/domain/advertising"
)

func TestWABAOfReadsTheWorkspacesOwnNumber(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT waba_id FROM whatsapp_business_phone_numbers WHERE id = \$1 AND owner_workspace_id = \$2 AND deleted_at IS NULL`).
		WithArgs("phone-1", "ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"waba_id"}).AddRow("waba-9"))
	got, err := NewWABADirectory(db).WABAOf(context.Background(), "ws-1", "phone-1")
	if err != nil || got != "waba-9" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestWABAOfAnotherWorkspacesNumberIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT waba_id`).WithArgs("phone-1", "ws-2").WillReturnRows(sqlmock.NewRows([]string{"waba_id"}))
	if _, err := NewWABADirectory(db).WABAOf(context.Background(), "ws-2", "phone-1"); !errors.Is(err, ads.ErrBusinessPhoneNotFound) {
		t.Fatalf("err %v", err)
	}
}
