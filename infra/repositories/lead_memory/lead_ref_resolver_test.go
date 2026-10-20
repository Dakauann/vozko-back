package lead_memory_repository

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
)

func TestLeadRefCoversEveryKnownEntryType(t *testing.T) {
	for _, entryType := range shared.KnownEntryTypes() {
		_, viaContacts := leadLinkedContactTables[entryType]
		_, direct := leadRefIsTheLead[entryType]
		if viaContacts == direct {
			t.Errorf("%s: the contact ref must resolve either as the lead itself or through exactly one contacts table (contacts=%v, direct=%v)", entryType, viaContacts, direct)
		}
	}
}

func TestLeadRefResolvesFacebookContacts(t *testing.T) {
	if leadLinkedContactTables[shared.EntryTypeFacebook] != "facebook_contacts" {
		t.Fatalf("facebook contacts must resolve to their lead, got %q", leadLinkedContactTables[shared.EntryTypeFacebook])
	}
	if !strings.Contains(leadViaContactsSQL(), "FROM facebook_contacts c") {
		t.Fatal("the generated query skips facebook_contacts")
	}
}

func TestLeadViaContactsQueryHasOneBranchPerTableAndMatchingArgs(t *testing.T) {
	query := leadViaContactsSQL()
	for _, table := range leadLinkedContactTables {
		if strings.Count(query, "FROM "+table+" c WHERE") != 1 {
			t.Errorf("%s: want exactly one branch in\n%s", table, query)
		}
	}
	args := leadViaContactsArgs("ws", "ref")
	if got := strings.Count(query, "?"); got != len(args) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
	if query != leadViaContactsSQL() {
		t.Fatal("the generated query must be stable between calls")
	}
}

func TestResolveLeadRefFallsBackToTheContactsOfEveryChannel(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	workspaceID := "11111111-1111-1111-1111-111111111111"
	ref := "22222222-2222-2222-2222-222222222222"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM leads WHERE workspace_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`FROM facebook_contacts c WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("33333333-3333-3333-3333-333333333333"))

	got := NewLeadRefResolver(db).ResolveLeadRef(workspaceID, ref)
	if got != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("lead = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
