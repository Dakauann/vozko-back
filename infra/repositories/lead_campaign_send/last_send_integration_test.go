package lead_campaign_send

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/infra/repositories/repotest"
)

func TestGetLastSendTimesBatchKeepsTheLatestSendPerLeadAcrossSeventyThousandIDs(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_campaign_sends")
	if err := db.Exec(`CREATE TABLE lead_campaign_sends (
		id uuid PRIMARY KEY, lead_id uuid NOT NULL, business_phone_id uuid NOT NULL, campaign_id uuid NOT NULL,
		sent_at timestamptz NOT NULL, created_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	phone, otherPhone := uuid.NewString(), uuid.NewString()
	lead := uuid.NewString()
	older := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		phone string
		at    time.Time
	}{{phone, older}, {phone, newer}, {otherPhone, newer.Add(time.Hour)}} {
		if err := db.Exec(`INSERT INTO lead_campaign_sends (id, lead_id, business_phone_id, campaign_id, sent_at) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), lead, row.phone, uuid.NewString(), row.at).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := NewRepository(db).GetLastSendTimesBatch(append(leadIDs(70_000), lead), phone)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[lead].Equal(newer) {
		t.Fatalf("got %v, want the latest send on this phone only", got)
	}
}
