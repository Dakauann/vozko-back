package dealautomation_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var (
	qrNumber   = dealautomation.Channel{EntryType: shared.EntryTypeUnofficialWhatsApp, Kind: conversation.ContainerKindAccount}
	qrCampaign = dealautomation.Channel{EntryType: shared.EntryTypeUnofficialWhatsApp, Kind: conversation.ContainerKindCampaign}
)

func TestSettingsAreSavedPerChannelAndReplacedInPlace(t *testing.T) {
	repo := New(repotest.IsolatedDB(t, "deal_auto_test", &schema.DealAutomation{}))
	ws, container := uuid.New().String(), uuid.New().String()

	if got, err := repo.Find(ws, qrNumber, container); err != nil || got != nil {
		t.Fatalf("Find() before saving = %+v, %v", got, err)
	}

	first, second := uuid.New().String(), uuid.New().String()
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, pipeline := range []string{first, second} {
		if err := repo.Save(dealautomation.Setting{WorkspaceID: ws, Channel: qrNumber, ContainerID: container, PipelineID: pipeline, UpdatedBy: "u1", UpdatedAt: at}); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}
	got, err := repo.Find(ws, qrNumber, container)
	if err != nil || got == nil || got.PipelineID != second || got.Channel != qrNumber {
		t.Fatalf("Find() = %+v, %v, want the second funnel", got, err)
	}

	if other, _ := repo.Find(ws, qrCampaign, container); other != nil {
		t.Fatalf("a QR campaign with the same id must not inherit the number's setting: %+v", other)
	}
	if other, _ := repo.Find(uuid.New().String(), qrNumber, container); other != nil {
		t.Fatalf("another workspace must not see the setting: %+v", other)
	}

	if err := repo.Delete(ws, qrNumber, container); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got, _ := repo.Find(ws, qrNumber, container); got != nil {
		t.Fatalf("Find() after delete = %+v", got)
	}
}
