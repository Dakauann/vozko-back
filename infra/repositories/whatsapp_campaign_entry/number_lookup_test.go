package whatsapp_campaign_entry

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestTheOutreachLookupStaysInsideTheWorkspaceAndSkipsDeletedLeads(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`JOIN leads ON leads\.id = whatsapp_campaign_entries\.lead_id AND leads\.deleted_at IS NULL .*`+
		`WHERE leads\.number IN \(\$1,\$2\) AND whatsapp_campaigns\.business_phone_id = \$3 AND whatsapp_campaign_entries\.status <> \$4 `+
		`AND leads\.workspace_id = \$5 AND whatsapp_campaigns\.workspace_id = \$6`).
		WithArgs("5511987654321", "551187654321", "bp-1", string(wce.SendStatusNotEligiblePossibleSpam), "ws-a", "ws-a", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := repo.FindByNumberBusinessPhoneAndWorkspace("5511987654321", "bp-1", "ws-a")
	if !errors.Is(err, wce.ErrEntryNotFound) {
		t.Fatalf("err = %v, want ErrEntryNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheOutreachLookupRefusesWithoutAWorkspace(t *testing.T) {
	repo, _, sqlDB := newEntryDB(t)
	defer sqlDB.Close()
	if _, err := repo.FindByNumberBusinessPhoneAndWorkspace("5511987654321", "bp-1", " "); !errors.Is(err, wce.ErrEntryNotFound) {
		t.Fatalf("err = %v, want ErrEntryNotFound without touching the database", err)
	}
}

func TestInboundRoutingLooksAcrossWorkspacesOnPurpose(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`JOIN leads ON leads\.id = whatsapp_campaign_entries\.lead_id AND leads\.deleted_at IS NULL .*`+
		`WHERE leads\.number IN \(\$1,\$2\) AND whatsapp_campaigns\.business_phone_id = \$3 AND whatsapp_campaign_entries\.status <> \$4 .*ORDER BY`).
		WithArgs("5511987654321", "551187654321", "bp-1", string(wce.SendStatusNotEligiblePossibleSpam), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("e-other-workspace"))

	got, err := repo.FindInboundRouteByNumberAndBusinessPhone("5511987654321", "bp-1")
	if err != nil || got.ID != "e-other-workspace" {
		t.Fatalf("a granted phone receives messages for entries of other workspaces, got %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
