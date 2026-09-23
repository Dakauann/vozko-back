package stage_repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/stage"
	instagram_repository "vozko/infra/repositories/instagram"
	telegram_repository "vozko/infra/repositories/telegram"
	unofficial_whatsapp_campaign_repository "vozko/infra/repositories/unofficial_whatsapp_campaign"
	whatsapp_campaign_repository "vozko/infra/repositories/whatsapp_campaign"
)

func pipelineIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 to run against postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

func TestEveryChannelPipelineLookupRunsAgainstPostgres(t *testing.T) {
	db := pipelineIntegrationDB(t)

	const absentID = "9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f8"

	resolvers := map[shared.EntryType]stage.ContainerPipelineResolver{
		shared.EntryTypeWhatsApp:           whatsapp_campaign_repository.NewContainerPipelineResolver(db),
		shared.EntryTypeInstagram:          instagram_repository.NewContainerPipelineResolver(db),
		shared.EntryTypeTelegram:           telegram_repository.NewContainerPipelineResolver(db),
		shared.EntryTypeUnofficialWhatsApp: unofficial_whatsapp_campaign_repository.NewContainerPipelineResolver(db),
	}

	for entryType, resolver := range resolvers {
		pipelineID, err := resolver.PipelineIDForContainer(context.Background(), absentID)
		if err != nil {
			t.Errorf("%s: %v", entryType, err)
			continue
		}
		if pipelineID != "" {
			t.Errorf("%s resolved %q for a container that does not exist", entryType, pipelineID)
		}
	}
}

func TestPipelineLookupToleratesAnEmptyContainerID(t *testing.T) {
	db := pipelineIntegrationDB(t)

	resolver := instagram_repository.NewContainerPipelineResolver(db)
	pipelineID, err := resolver.PipelineIDForContainer(context.Background(), "")
	if err != nil {
		t.Fatalf("an empty container id must not reach the database: %v", err)
	}
	if pipelineID != "" {
		t.Fatalf("resolved %q for an empty container id", pipelineID)
	}
}

func TestUnofficialWhatsAppFallsFromCampaignToInstance(t *testing.T) {
	db := pipelineIntegrationDB(t)
	resolver := unofficial_whatsapp_campaign_repository.NewContainerPipelineResolver(db)

	var instanceIDs []string
	if err := db.Raw(`SELECT id::text FROM unofficial_whatsapp_instances
		WHERE deleted_at IS NULL LIMIT 1`).Scan(&instanceIDs).Error; err != nil {
		t.Fatalf("read an instance: %v", err)
	}
	if len(instanceIDs) == 0 {
		t.Skip("no unofficial whatsapp instance in this database")
	}

	pipelineID, err := resolver.PipelineIDForContainer(context.Background(), instanceIDs[0])
	if err != nil {
		t.Fatalf("an instance id must be resolvable, not an error: %v", err)
	}
	t.Logf("instance %s resolved pipeline %q", instanceIDs[0], pipelineID)
}

func TestAnInstanceWithAFunnelResolvesIt(t *testing.T) {
	db := pipelineIntegrationDB(t)

	var instanceIDs []string
	if err := db.Raw(`SELECT id::text FROM unofficial_whatsapp_instances
		WHERE deleted_at IS NULL LIMIT 1`).Scan(&instanceIDs).Error; err != nil {
		t.Fatalf("read an instance: %v", err)
	}
	if len(instanceIDs) == 0 {
		t.Skip("no unofficial whatsapp instance in this database")
	}
	instanceID := instanceIDs[0]

	var pipelineIDs []string
	if err := db.Raw(`SELECT id::text FROM pipelines
		WHERE object_type = 'conversation' LIMIT 1`).Scan(&pipelineIDs).Error; err != nil {
		t.Fatalf("read a pipeline: %v", err)
	}
	if len(pipelineIDs) == 0 {
		t.Skip("no conversation pipeline in this database")
	}

	tx := db.Begin()
	defer tx.Rollback()

	if err := tx.Exec(`UPDATE unofficial_whatsapp_instances SET pipeline_id = ?::uuid WHERE id = ?::uuid`,
		pipelineIDs[0], instanceID).Error; err != nil {
		t.Fatalf("bind the funnel: %v", err)
	}

	resolved, err := unofficial_whatsapp_campaign_repository.
		NewContainerPipelineResolver(tx).
		PipelineIDForContainer(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved != pipelineIDs[0] {
		t.Fatalf("resolved %q, want the funnel bound to the number (%q)", resolved, pipelineIDs[0])
	}
}
