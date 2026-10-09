package lead

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/lead"
)

func TestOtherHoldersReadsAFewHoldersPerNumberFormInOneArrayAndTheirPhones(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	formats := pq.StringArray{"5511987654321", "551187654321"}
	perForm := lead.SharedNumberHolderLimit + 1
	mock.ExpectQuery("^"+regexp.QuoteMeta(numberedPlaceholders(otherHoldersSQL))+"$").
		WithArgs(wsUUID, formats, wsUUID, leadUUID, perForm, wsUUID, leadUUID, perForm).
		WillReturnRows(leadRow("Maria", "manual", 4))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_phones" WHERE workspace_id = $1 AND lead_id = ANY($2::uuid[]) ORDER BY lead_id, position`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "number", "label", "position"}).
			AddRow(phoneUUID, wsUUID, leadUUID, "551133334444", "landline", 0))

	got, err := NewNumberDirectory(db).OtherHolders(context.Background(), wsUUID, leadUUID, []string{"5511987654321"})
	if err != nil {
		t.Fatalf("OtherHolders: %v", err)
	}
	if len(got) != 1 || !got[0].HoldsNumber("551133334444") || got[0].Addresses != nil {
		t.Fatalf("OtherHolders = %+v, want the holder with its phones only", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func numberedPlaceholders(sql string) string {
	var out strings.Builder
	n := 0
	for _, r := range sql {
		if r == '?' {
			n++
			out.WriteString("$" + strconv.Itoa(n))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func TestOtherHoldersBindsEveryPlaceholder(t *testing.T) {
	if got := strings.Count(otherHoldersSQL, "?"); got != 8 {
		t.Fatalf("placeholders = %d, want the 8 OtherHolders binds", got)
	}
	if !strings.Contains(otherHoldersSQL, " AND p.lead_id <> ?::uuid ORDER BY p.lead_id LIMIT ?)") {
		t.Fatal("the contact phone holders must be the same sample on every read, taken in index order")
	}
	if strings.Count(otherHoldersSQL, "ORDER BY") != 1 {
		t.Fatal("only the contact phone bucket is ordered: a WhatsApp number has one live holder per form")
	}
}

func TestOtherHoldersQueriesNothingWithoutNumbersAndRefusesWithoutAWorkspaceOrALead(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	directory := NewNumberDirectory(db)
	if got, err := directory.OtherHolders(context.Background(), wsUUID, leadUUID, []string{" "}); err != nil || len(got) != 0 {
		t.Fatalf("OtherHolders = %+v, %v, want nobody", got, err)
	}
	if _, err := directory.OtherHolders(context.Background(), " ", leadUUID, []string{"5511987654321"}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("err = %v, want the workspace refusal", err)
	}
	if _, err := directory.OtherHolders(context.Background(), wsUUID, "lead-1", []string{"5511987654321"}); !errors.Is(err, lead.ErrLeadRequired) {
		t.Fatalf("err = %v, want the lead refusal", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
