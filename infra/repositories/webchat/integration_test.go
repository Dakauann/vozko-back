package webchat_repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/export"
	wcdomain "vozko/domain/webchat"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
	"vozko/infra/database/schema"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	piigorm.SetService(s)
	t.Cleanup(func() { piigorm.SetService(nil) })

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "wc_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&schema.WebchatWidget{}, &schema.WebchatVisitor{}, &schema.WebchatConversation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX ux_wc_widget_public_key ON webchat_widgets (public_key)`,
		`CREATE UNIQUE INDEX ux_wc_visitor_widget_external ON webchat_visitors (widget_id, external_id) WHERE external_id IS NOT NULL AND deleted_at IS NULL`,
		`CREATE UNIQUE INDEX ux_wc_conversation_widget_visitor ON webchat_conversations (widget_id, visitor_id) WHERE deleted_at IS NULL`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index: %v", err)
		}
	}
	return db
}

func TestWebchatRepositoriesAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	widgets, visitors, conversations := NewWidgetRepository(db), NewVisitorRepository(db), NewConversationRepository(db)

	workspaceID, departmentID := uuid.NewString(), uuid.NewString()
	widget := &wcdomain.Widget{
		WorkspaceID: workspaceID, DepartmentID: &departmentID, Name: "Loja", PublicKey: "pk-" + uuid.NewString()[:8],
		AllowedOrigins: []string{"https://loja.example.com", "https://*.example.org"},
		IdentityMode:   wcdomain.IdentityRequired, IdentitySecret: "s3cret",
	}
	widget.Normalize()
	if err := widgets.Create(ctx, widget); err != nil {
		t.Fatal(err)
	}

	t.Run("widget round trip keeps origins and decrypts the secret", func(t *testing.T) {
		got, err := widgets.FindByPublicKey(ctx, widget.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.AllowedOrigins) != 2 || got.IdentitySecret != "s3cret" || got.Status != wcdomain.StatusActive {
			t.Fatalf("widget = %+v", got)
		}
		var raw []byte
		db.Raw("SELECT identity_secret FROM webchat_widgets WHERE id = ?", widget.ID).Scan(&raw)
		if bytes.Contains(raw, []byte("s3cret")) {
			t.Fatal("the identity secret is stored in clear text")
		}
		if _, err := widgets.FindByID(ctx, uuid.NewString(), widget.ID); !errors.Is(err, wcdomain.ErrWidgetNotFound) {
			t.Fatalf("widget from another workspace = %v", err)
		}
	})

	visitor := &wcdomain.Visitor{WorkspaceID: workspaceID, WidgetID: widget.ID}
	if err := visitors.Create(ctx, visitor); err != nil {
		t.Fatal(err)
	}

	t.Run("one thread per visitor even under concurrent first messages", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make([]string, 8)
		for i := range ids {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				conv, err := conversations.FindOrCreate(ctx, wcdomain.FindOrCreateConversationInput{
					WorkspaceID: workspaceID, WidgetID: widget.ID, VisitorID: visitor.ID,
				})
				if err != nil {
					t.Error(err)
					return
				}
				ids[i] = conv.ID
			}(i)
		}
		wg.Wait()
		for _, id := range ids {
			if id != ids[0] {
				t.Fatalf("concurrent first messages created several threads: %v", ids)
			}
		}
	})

	conv, _ := conversations.FindByVisitor(ctx, widget.ID, visitor.ID)

	t.Run("pending options are stored and cleared", func(t *testing.T) {
		if err := conversations.SetPendingOptions(ctx, conv.ID, []wcdomain.Option{{ID: "a", Title: "Vendas"}}); err != nil {
			t.Fatal(err)
		}
		got, _ := conversations.FindByID(ctx, conv.ID)
		if len(got.PendingOptions) != 1 || got.PendingOptions[0].Title != "Vendas" {
			t.Fatalf("options = %+v", got.PendingOptions)
		}
		if err := conversations.SetPendingOptions(ctx, conv.ID, nil); err != nil {
			t.Fatal(err)
		}
		got, _ = conversations.FindByID(ctx, conv.ID)
		if len(got.PendingOptions) != 0 {
			t.Fatalf("options not cleared: %+v", got.PendingOptions)
		}
	})

	t.Run("department and workspace come from the widget", func(t *testing.T) {
		dept, err := conversations.DepartmentIDForEntry(ctx, conv.ID)
		if err != nil || dept != departmentID {
			t.Fatalf("department = %q %v", dept, err)
		}
		ws, err := conversations.WorkspaceIDForEntry(ctx, conv.ID)
		if err != nil || ws != workspaceID {
			t.Fatalf("workspace = %q %v", ws, err)
		}
	})

	t.Run("verified visitors are unique per widget", func(t *testing.T) {
		external := "customer-42"
		first := &wcdomain.Visitor{WorkspaceID: workspaceID, WidgetID: widget.ID, ExternalID: &external}
		if err := visitors.Create(ctx, first); err != nil {
			t.Fatal(err)
		}
		dup := &wcdomain.Visitor{WorkspaceID: workspaceID, WidgetID: widget.ID, ExternalID: &external}
		if err := visitors.Create(ctx, dup); err == nil {
			t.Fatal("a second visitor with the same verified id was created")
		}
		found, err := visitors.FindByExternalID(ctx, widget.ID, external)
		if err != nil || found.ID != first.ID {
			t.Fatalf("found = %+v %v", found, err)
		}
	})

	t.Run("intake and block are persisted", func(t *testing.T) {
		at := time.Now().UTC().Truncate(time.Second)
		leadID := uuid.NewString()
		if err := visitors.SaveIntake(ctx, visitor.ID, wcdomain.Intake{Name: "Ana", Phone: "5511999990000"}, &leadID, at); err != nil {
			t.Fatal(err)
		}
		if err := visitors.SetBlocked(ctx, visitor.ID, true, at); err != nil {
			t.Fatal(err)
		}
		got, _ := visitors.FindByID(ctx, visitor.ID)
		if got.Name != "Ana" || got.LeadID == nil || *got.LeadID != leadID || !got.Blocked || got.IntakeCompletedAt == nil {
			t.Fatalf("visitor = %+v", got)
		}
	})

	t.Run("export lists the widget's conversations with the visitor identity", func(t *testing.T) {
		var rows []export.ChannelEntry
		err := NewExportRepository(db).ListForExport(ctx, export.Scope{WorkspaceID: workspaceID, ContainerID: widget.ID},
			func(e export.ChannelEntry) error { rows = append(rows, e); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "Ana" || rows[0].Number != "+5511999990000" {
			t.Fatalf("rows = %+v", rows)
		}
	})
}
