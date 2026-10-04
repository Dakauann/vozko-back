package advertising_repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	ads "vozko/domain/advertising"
)

func TestNumberDirectoryListsLiveOfficialAndUnofficialNumbersOfTheWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM whatsapp_business_phone_numbers .*owner_workspace_id = \$1 AND deleted_at IS NULL.*UNION ALL.*FROM unofficial_whatsapp_instances .*workspace_id = \$2 AND deleted_at IS NULL.*`+
		`FROM ad_page_whatsapp_links l\s+WHERE l\.workspace_id = \$3 AND l\.number = regexp_replace\(n\.number, '\[\^0-9\]', '', 'g'\)`).
		WithArgs("ws-1", "ws-1", "ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"kind", "label", "number", "portfolio_id", "linked_pages"}).
			AddRow("official", "Loja", "+55 11 98888-7777", "biz-1", "page-1,page-2").
			AddRow("unofficial", "Vendas", "5511977776666", "", ""))

	got, err := NewNumberDirectory(db).List(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != ads.NumberOfficial || got[1].Kind != ads.NumberUnofficial || got[1].Label != "Vendas" || got[0].PortfolioID != "biz-1" || got[1].PortfolioID != "" ||
		len(got[0].LinkedPageIDs) != 2 || got[0].LinkedPageIDs[1] != "page-2" || len(got[1].LinkedPageIDs) != 0 {
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

func TestNumberDirectoryRecordsAPageLinkByTheNumbersDigits(t *testing.T) {
	db, mock := newMockDB(t)
	at := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	mock.ExpectExec(`INSERT INTO ad_page_whatsapp_links \(workspace_id, page_id, number, linked_at\) VALUES .*ON CONFLICT \(workspace_id, page_id, number\) DO UPDATE SET linked_at = EXCLUDED\.linked_at`).
		WithArgs("ws-1", "page-1", "5511965467700", at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	directory := &numberDirectory{db: db, now: func() time.Time { return at }}
	if err := directory.RecordPageLink(context.Background(), "ws-1", " page-1 ", "+55 11 96546-7700"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][3]string{{" ", "page-1", "5511965467700"}, {"ws-1", " ", "5511965467700"}, {"ws-1", "page-1", "abc"}} {
		if err := directory.RecordPageLink(context.Background(), args[0], args[1], args[2]); err == nil {
			t.Fatalf("%v recorded", args)
		}
	}
}
