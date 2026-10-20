package audience_repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	ca "vozko/domain/audience"
	"vozko/infra/database/schema"
	"vozko/infra/database/schemascan"
)

func TestTheAccountPurgeCoversEveryAudienceTableKeyedByAccount(t *testing.T) {
	models, err := schemascan.Models(filepath.Join("..", "..", "database", "schema"))
	if err != nil {
		t.Fatal(err)
	}
	purged := map[string]bool{}
	for _, table := range accountTables {
		purged[table] = true
	}
	for _, m := range models {
		if !strings.HasPrefix(m.Table, "audience_") {
			continue
		}
		if !m.Has("Source", "AccountID") {
			t.Errorf("%s is an audience table without source and account; decide how it is purged", m.Table)
			continue
		}
		if !purged[m.Table] {
			t.Errorf("%s holds account data but the account purge skips it", m.Table)
		}
		delete(purged, m.Table)
	}
	for table := range purged {
		t.Errorf("%s is purged but no schema model declares it", table)
	}
}

func TestPurgeAccountDeletesOnlyThatAccountOfThatSource(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	target, other := uuid.NewString(), uuid.NewString()
	workspace := uuid.NewString()
	for _, row := range []schema.AudienceSettings{
		{Source: "tiktok", AccountID: target, WorkspaceID: workspace, Topics: datatypes.JSON("[]")},
		{Source: "tiktok", AccountID: other, WorkspaceID: workspace, Topics: datatypes.JSON("[]")},
		{Source: "instagram", AccountID: target, WorkspaceID: workspace, Topics: datatypes.JSON("[]")},
	} {
		row := row
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := NewAccountPurger(db).PurgeAccount(ctx, ca.Source("tiktok"), target); err != nil {
		t.Fatalf("purge against the real schema: %v", err)
	}
	var left []schema.AudienceSettings
	if err := db.Order("source, account_id").Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 {
		t.Fatalf("rows left = %+v", left)
	}
	for _, row := range left {
		if row.Source == "tiktok" && row.AccountID == target {
			t.Fatal("the purged account kept its settings")
		}
	}
	if err := NewAccountPurger(db).PurgeAccount(ctx, ca.Source("tiktok"), " "); err == nil {
		t.Fatal("an empty account must be refused")
	}
}
