package emailcampaign

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	audience "vozko/domain/campaignaudience"
	domain "vozko/domain/emailcampaign"
)

func TestTheListAndMergePoliciesOfADraftRoundTripAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	repo := NewCampaignRepository(db)
	listID := uuid.NewString()
	policies := []audience.Variable{{Key: "first_name", Missing: audience.MissingText, Text: "cliente"}, {Key: "field.serie", Missing: audience.MissingExclude}}
	c := &domain.Campaign{WorkspaceID: uuid.NewString(), Name: "Matrículas", Status: domain.StatusDraft, ListID: listID, MergePolicies: policies}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, c.WorkspaceID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListID != listID || got.Selection != nil || !reflect.DeepEqual(got.MergePolicies, policies) {
		t.Fatalf("round trip = %+v", got)
	}
	got.ListID, got.MergePolicies = "", nil
	if err := repo.Update(ctx, got, got.Status); err != nil {
		t.Fatal(err)
	}
	cleared, err := repo.Get(ctx, c.WorkspaceID, c.ID)
	if err != nil || cleared.ListID != "" || cleared.MergePolicies != nil {
		t.Fatalf("cleared = %+v, %v", cleared, err)
	}
}
