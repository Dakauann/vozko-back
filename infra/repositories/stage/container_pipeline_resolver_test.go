package stage_repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func resolverDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestResolverFallsThroughTheModelsInOrder(t *testing.T) {
	db, mock := resolverDB(t)
	mock.ExpectQuery(`SELECT "pipeline_id" FROM "unofficial_whatsapp_campaigns" WHERE id = \$1 AND "unofficial_whatsapp_campaigns"."deleted_at" IS NULL`).WillReturnRows(sqlmock.NewRows([]string{"pipeline_id"}))
	mock.ExpectQuery(`SELECT "pipeline_id" FROM "unofficial_whatsapp_instances"`).WillReturnRows(sqlmock.NewRows([]string{"pipeline_id"}).AddRow(" pipe-9 "))
	resolver := NewContainerPipelineResolver(db,
		func() any { return &schema.UnofficialWhatsAppCampaign{} },
		func() any { return &schema.UnofficialWhatsAppInstance{} })
	got, err := resolver.PipelineIDForContainer(context.Background(), "c-1")
	if err != nil || got != "pipe-9" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolverSkipsTheDatabaseForAnEmptyContainer(t *testing.T) {
	db, _ := resolverDB(t)
	got, err := NewContainerPipelineResolver(db, func() any { return &schema.FacebookPage{} }).PipelineIDForContainer(context.Background(), " ")
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestEveryBoardChannelHasAPipelineResolver(t *testing.T) {
	resolvers := ChannelPipelineResolvers(nil)
	for _, e := range shared.CRMTaggableEntryTypes() {
		if _, ok := resolvers[e]; !ok {
			t.Errorf("%s has no pipeline resolver; its cards would fall back to the default funnel", e)
		}
	}
}
