package analytics_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	analytics_domain "vozko/domain/analytics"
)

func TestNumbersCountMessagesFirstThenJoinTheirPhone(t *testing.T) {
	db, mock, sqlDB := newMetaCostDB(t)
	defer sqlDB.Close()
	input := metaCostInput()
	first := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(`SET LOCAL jit = off`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET LOCAL statement_timeout`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`WITH per_phone AS MATERIALIZED \([\s\S]*MIN\(cm\.created_at\) FILTER[\s\S]*FROM conversation_messages cm[\s\S]*GROUP BY wc\.business_phone_id\s*\)[\s\S]*FROM per_phone pp\s+JOIN whatsapp_business_phone_numbers p ON p\.id = pp\.phone_id`).
		WithArgs(input.StartDate, input.EndDate, string(analytics_domain.ServiceMessageProviderMeta), metaCostDetailLimit).
		WillReturnRows(sqlmock.NewRows([]string{"phone_id", "display_phone_number", "provider", "workspace_name", "service_messages", "answered", "charged", "first_charged_at"}).
			AddRow("p-1", "+55 11 96546-7700", "meta", "Vozko", 120, 90, 60, first))
	mock.ExpectCommit()

	numbers, err := (&repository{db: db}).MetaCostNumbers(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(numbers) != 1 || numbers[0].Charged != 60 || numbers[0].FirstChargedAt == nil || !numbers[0].FirstChargedAt.Equal(first) {
		t.Fatalf("numbers %+v", numbers)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUnlinkedMessagesAreGroupedByTheNumberTheyCameIn(t *testing.T) {
	db, mock, sqlDB := newMetaCostDB(t)
	defer sqlDB.Close()
	input := metaCostInput()

	mock.ExpectQuery(`FROM whatsapp_unattributed_service_messages u[\s\S]*LEFT JOIN whatsapp_business_phone_numbers p ON p\.meta_phone_number_id = u\.phone_number_id AND p\.deleted_at IS NULL[\s\S]*u\.first_seen_at >= \$1 AND u\.first_seen_at < \$2`).
		WithArgs(input.StartDate, input.EndDate, metaCostDetailLimit).
		WillReturnRows(sqlmock.NewRows([]string{"phone_number_id", "display_phone_number", "messages"}).AddRow("885", "+55 85 91000-3321", 27))

	unlinked, err := (&repository{db: db}).UnlinkedServiceMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(unlinked) != 1 || unlinked[0].Messages != 27 || unlinked[0].DisplayPhoneNumber != "+55 85 91000-3321" {
		t.Fatalf("unlinked %+v", unlinked)
	}
}

func TestTheUnattributedProviderHasNoNumbersToList(t *testing.T) {
	db, _, sqlDB := newMetaCostDB(t)
	defer sqlDB.Close()
	input := metaCostInput()
	input.Provider = analytics_domain.ServiceMessageProviderUnattributed
	numbers, err := (&repository{db: db}).MetaCostNumbers(input)
	if err != nil || len(numbers) != 0 {
		t.Fatalf("numbers %+v err %v", numbers, err)
	}
}
