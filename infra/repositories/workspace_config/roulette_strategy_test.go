package workspace_config_repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	ia "vozko/domain/inbox_assignment"
)

func TestRouletteRecipients_RoundTrip(t *testing.T) {
	list := []ia.Recipient{{UserID: "ana", Tier: 1, Weight: 3}, {UserID: "bob", Tier: 2, Weight: 1}}
	raw, err := encodeRouletteRecipients(list)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeRouletteRecipients("ws-1", raw); !reflect.DeepEqual(got, list) {
		t.Fatalf("got %+v", got)
	}
}

func TestRouletteRecipients_EmptyListIsStoredAsNull(t *testing.T) {
	raw, err := encodeRouletteRecipients(nil)
	if err != nil || raw != nil {
		t.Fatalf("got %v, %v", raw, err)
	}
}

func TestRouletteRecipients_UnreadableDocumentReadsAsNoRecipients(t *testing.T) {
	broken := `{"not":"a list"`
	if got := decodeRouletteRecipients("ws-1", &broken); got != nil {
		t.Fatalf("an unreadable list must let nobody through, got %+v", got)
	}
	invalid := `[{"userId":"ana","tier":0,"weight":1}]`
	if got := decodeRouletteRecipients("ws-1", &invalid); got != nil {
		t.Fatalf("a list that breaks the rules must let nobody through, got %+v", got)
	}
}

func TestGetByWorkspaceID_ReadsTheStrategyColumns(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "workspace_configs"`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "workspace_id", "roulette_mode", "roulette_strategy", "roulette_max_open_per_member", "roulette_recipients"}).
			AddRow("cfg-1", "ws-1", "online", "priority", 7, `[{"userId":"ana","tier":1,"weight":2}]`),
	)

	cfg, err := NewRepository(db).GetByWorkspaceID(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RouletteStrategy != ia.StrategyPriority || cfg.RouletteMaxOpenPerMember != 7 {
		t.Fatalf("got %+v", cfg)
	}
	if want := []ia.Recipient{{UserID: "ana", Tier: 1, Weight: 2}}; !reflect.DeepEqual(cfg.RouletteRecipients, want) {
		t.Fatalf("got %+v", cfg.RouletteRecipients)
	}
}
