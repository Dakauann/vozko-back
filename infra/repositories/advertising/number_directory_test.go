package advertising_repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	ads "vozko/domain/advertising"
)

func TestNumberDirectoryListsLiveOfficialAndUnofficialNumbersOfTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM whatsapp_business_phone_numbers .*owner_workspace_id = \$1 AND deleted_at IS NULL.*UNION ALL.*FROM unofficial_whatsapp_instances .*workspace_id = \$2 AND deleted_at IS NULL`).
		WithArgs("ws-1", "ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"kind", "label", "number", "portfolio_id"}).
			AddRow("official", "Loja", "+55 11 98888-7777", "biz-1").
			AddRow("unofficial", "Vendas", "5511977776666", ""))

	got, err := NewNumberDirectory(db).List(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != ads.NumberOfficial || got[1].Kind != ads.NumberUnofficial || got[1].Label != "Vendas" || got[0].PortfolioID != "biz-1" || got[1].PortfolioID != "" {
		t.Fatalf("numbers %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNumberDirectoryRefusesABlankWorkspace(t *testing.T) {
	db, _ := newMockDB(t)
	if _, err := NewNumberDirectory(db).List(context.Background(), " "); err == nil {
		t.Fatal("listed numbers without a workspace")
	}
}
