package database

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBareInstanceJIDsDropsTheOldIndexBeforeStripping(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta("DROP INDEX IF EXISTS ux_uw_instance_jid")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE unofficial_whatsapp_instances AS i") +
		`.*SET status = 'DISCONNECTED'` +
		`.*PARTITION BY regexp_replace\(jid, ':\[0-9\]\+@', '@'\)` +
		`.*ORDER BY created_at DESC` +
		`.*WHERE ranked\.id = i\.id AND ranked\.rank > 1`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE unofficial_whatsapp_instances") +
		`.*SET jid = regexp_replace\(jid, ':\[0-9\]\+@', '@'\)` +
		`.*WHERE jid ~ ':\[0-9\]\+@'`).
		WillReturnResult(sqlmock.NewResult(0, 3))

	if err := bareInstanceJIDs(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBareInstanceJIDsIsRegistered(t *testing.T) {
	for _, name := range repairNames() {
		if name == "uw_bare_instance_jids" {
			return
		}
	}
	t.Fatal("the bare instance jid repair is not registered in runDataRepairs")
}

func TestLiveJIDIndexOnlyCoversConnectedInstances(t *testing.T) {
	if !strings.Contains(uwLiveInstanceJIDIndexSQL, "WHERE jid <> '' AND deleted_at IS NULL AND status = 'CONNECTED'") {
		t.Fatalf("a disconnected link must not block relinking the number:\n%s", uwLiveInstanceJIDIndexSQL)
	}
}
