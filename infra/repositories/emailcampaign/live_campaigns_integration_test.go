package emailcampaign

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domain "vozko/domain/emailcampaign"
)

func TestLiveCampaignsCountsScheduledRunningAndHeldAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	repo := NewCampaignRepository(db)
	live := NewLiveCampaigns(db)
	for _, s := range domain.AllStatuses() {
		ws := uuid.NewString()
		c := &domain.Campaign{WorkspaceID: ws, Name: "Campanha", Status: domain.StatusDraft, Subject: "Oi", CreatedBy: uuid.NewString()}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE email_campaigns SET status = ? WHERE id = ?", string(s), c.ID).Error; err != nil {
			t.Fatal(err)
		}
		got, err := live.HasLiveCampaign(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if got != s.KeepsSenderAwake() {
			t.Errorf("%s: HasLiveCampaign = %v, want %v", s, got, s.KeepsSenderAwake())
		}
	}
	if got, err := live.HasLiveCampaign(ctx, uuid.NewString()); err != nil || got {
		t.Fatalf("a workspace without campaigns = %v, %v", got, err)
	}
}
