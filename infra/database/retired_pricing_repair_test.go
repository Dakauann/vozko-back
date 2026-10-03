package database

import (
	"testing"
)

func TestRetiredSIPTrunkPricesAreDroppedAndTheRestKept(t *testing.T) {
	tx := repairTx(t)
	if err := tx.Exec(`CREATE TEMP TABLE plan_pricing_items (
		id uuid PRIMARY KEY DEFAULT gen_random_uuid(), plan_definition_id uuid NOT NULL, category varchar(50) NOT NULL,
		service varchar(100) NOT NULL, metric varchar(100) NOT NULL, price_micros bigint NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("table: %v", err)
	}
	if err := tx.Exec(`INSERT INTO plan_pricing_items (plan_definition_id, category, service, metric, price_micros) VALUES
		('11111111-1111-1111-1111-111111111111', 'telephony', 'sip_trunk', 'per_minute', 8333),
		('11111111-1111-1111-1111-111111111111', 'telephony', 'whatsapp_calls', 'per_minute', 13333),
		('22222222-2222-2222-2222-222222222222', 'telephony', 'sip_calls', 'per_minute', 0),
		('22222222-2222-2222-2222-222222222222', 'whatsapp', 'sip_trunk', 'per_message', 5)`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for range 2 {
		if err := dropRetiredSIPTrunkPrices(tx); err != nil {
			t.Fatalf("repair: %v", err)
		}
	}

	var services []string
	if err := tx.Raw(`SELECT category || ':' || service FROM plan_pricing_items ORDER BY 1`).Scan(&services).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{"telephony:sip_calls", "telephony:whatsapp_calls", "whatsapp:sip_trunk"}
	if len(services) != len(want) {
		t.Fatalf("left %v, want %v", services, want)
	}
	for i := range want {
		if services[i] != want[i] {
			t.Fatalf("left %v, want %v", services, want)
		}
	}
}
