package campaigndraft_repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaigndraft"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
	uwc_repo "vozko/infra/repositories/unofficial_whatsapp_campaign"
	wc_repo "vozko/infra/repositories/whatsapp_campaign"
)

func TestIntegrationADraftIsNeverSentListedScheduledOrCounted(t *testing.T) {
	db := repotest.IsolatedDB(t, "campaign_draft_isolation",
		&schema.CampaignDraft{}, &schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{},
		&schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaignEntry{})
	ctx := context.Background()
	ws := uuid.NewString()
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	drafts := NewRepository(db)
	for _, ch := range campaigndraft.Channels() {
		payload := json.RawMessage(`{"request":{"name":"Natal","status":"RUNNING","scheduledStart":"` + past + `","phoneNumbers":[{"number":"5511900010142"}]}}`)
		d, err := campaigndraft.New(uuid.NewString(), ws, uuid.NewString(), ch, "Natal", payload, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := drafts.Create(ctx, &d); err != nil {
			t.Fatal(err)
		}
	}

	official := wc_repo.NewRepository(db)
	listed, err := official.List(wc.ListCampaignsInput{WorkspaceID: ws, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	if listed.TotalItems != 0 || len(listed.Items) != 0 {
		t.Fatalf("an official list showed a draft: %+v", listed)
	}
	for _, status := range []wc.Status{wc.CampaignStatusRunning, wc.CampaignStatusStopped, wc.CampaignStatusPaused} {
		running, err := official.ListByStatus(status)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range running {
			if c.WorkspaceID == ws {
				t.Fatalf("a draft is an official %s campaign", status)
			}
		}
	}
	due, err := official.ListScheduledToStart(time.Now().Add(365*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range due {
		if c.WorkspaceID == ws {
			t.Fatal("the official start job would pick a draft")
		}
	}

	unofficial := uwc_repo.NewRepository(db)
	ulisted, err := unofficial.List(uwc.ListCampaignsInput{WorkspaceID: ws, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	if ulisted.TotalItems != 0 || len(ulisted.Items) != 0 {
		t.Fatalf("an unofficial list showed a draft: %+v", ulisted)
	}
	udue, err := unofficial.ListScheduledToStart(time.Now().Add(365*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range udue {
		if c.WorkspaceID == ws {
			t.Fatal("the unofficial start job would pick a draft")
		}
	}
	counts, err := uwc_repo.NewSummaryRepository(db).CountByStatusForWorkspace(uwc.WorkspaceSummaryFilter{WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	if counts != nil && (counts.Total != 0 || counts.Pending != 0) {
		t.Fatalf("a draft was counted: %+v", counts)
	}

	var entries int64
	if err := db.Model(&schema.WhatsAppCampaignEntry{}).Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("a draft created %d entries", entries)
	}
	var stored []schema.CampaignDraft
	if err := db.Where("workspace_id = ?", ws).Find(&stored).Error; err != nil || len(stored) != 2 {
		t.Fatalf("drafts %d %v", len(stored), err)
	}
}
