package lead

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/selection"
)

func TestEmailFactsReadsOneQueryPerChunk(t *testing.T) {
	db, mock, _ := newMockDB(t)
	ids := make([]string, selection.Chunk+1)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	query := regexp.QuoteMeta("FROM leads l LEFT JOIN lead_companies c")
	mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "custom_fields", "opted_out", "blocked", "company"}).
		AddRow(ids[0], "Ana Souza", "ana@example.com", []byte(`{"plano":"Ouro"}`), false, false, "Loja"))
	mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "custom_fields", "opted_out", "blocked", "company"}).
		AddRow(ids[selection.Chunk], "Bia", "", nil, true, true, ""))
	got, err := NewEmailFacts(db).EmailFacts(context.Background(), "ws", ids)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	first := got[ids[0]]
	if first.Email != "ana@example.com" || first.Company != "Loja" || first.CustomFields["plano"] != "Ouro" {
		t.Fatalf("first = %+v", first)
	}
	last := got[ids[selection.Chunk]]
	if !last.OptedOut || !last.Blocked || last.CustomFields != nil {
		t.Fatalf("last = %+v", last)
	}
}

func TestEmailFactsNeedsAWorkspaceAndSkipsEmptyInput(t *testing.T) {
	db, mock, _ := newMockDB(t)
	if _, err := NewEmailFacts(db).EmailFacts(context.Background(), " ", []string{uuid.NewString()}); err == nil {
		t.Fatal("a workspace is required")
	}
	got, err := NewEmailFacts(db).EmailFacts(context.Background(), "ws", []string{"not-a-uuid"})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
