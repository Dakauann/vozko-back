package googleaccount_repository

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/googleaccount"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/testenv"
)

func withPII(t *testing.T) {
	t.Helper()
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
}

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
	withPII(t)
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "gacct_test_" + uuid.New().String()[:8]
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
	if err := db.AutoMigrate(schema.GoogleModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{"ux_google_account_workspace_email", "idx_google_acct_refresh_due", "idx_google_acct_email"} {
		stmt, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return db
}

func connected(workspaceID, email string) *googleaccount.Account {
	return &googleaccount.Account{
		WorkspaceID: workspaceID, Email: email, AccessToken: "access-" + email, RefreshToken: "refresh-" + email,
		AccessExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second), Scopes: googleaccount.ProductSheets.Scopes(),
	}
}

func TestSaveConnectedKeepsOneRowPerWorkspaceAndEmail(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()

	first := connected(ws, "Ana@Example.com")
	if err := repo.SaveConnected(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := connected(ws, "ana@example.com")
	second.AccessToken = "rotated"
	if err := repo.SaveConnected(ctx, second); err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("reconnecting the same email created a second row: %s vs %s", first.ID, second.ID)
	}
	stored, err := repo.FindByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "rotated" || stored.Email != "ana@example.com" || stored.Status != googleaccount.StatusConnected {
		t.Fatalf("stored = %+v", stored)
	}
	var raw []byte
	if err := db.Raw("SELECT access_token FROM google_accounts WHERE id = ?", first.ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("rotated")) {
		t.Fatal("the access token is stored in plaintext")
	}
}

func TestBindFindAndUnbind(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	account := connected(ws, "ops@example.com")
	if err := repo.SaveConnected(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBound(ctx, ws, googleaccount.ProductSheets); !errors.Is(err, googleaccount.ErrNotConnected) {
		t.Fatalf("unbound FindBound err = %v", err)
	}
	for _, p := range googleaccount.Products() {
		if err := repo.Bind(ctx, googleaccount.Binding{WorkspaceID: ws, Product: p, AccountID: account.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Bind(ctx, googleaccount.Binding{WorkspaceID: ws, Product: googleaccount.ProductSheets, AccountID: account.ID}); err != nil {
		t.Fatalf("binding twice must be an update: %v", err)
	}
	bound, err := repo.FindBound(ctx, ws, googleaccount.ProductSheets)
	if err != nil || bound.ID != account.ID {
		t.Fatalf("FindBound = %+v, %v", bound, err)
	}
	if n, _ := repo.CountBindings(ctx, account.ID); n != 2 {
		t.Fatalf("CountBindings = %d, want 2", n)
	}
	list, err := repo.ListBound(ctx, ws)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListBound = %+v, %v", list, err)
	}
	removed, err := repo.Unbind(ctx, ws, googleaccount.ProductSheets)
	if err != nil || removed != account.ID {
		t.Fatalf("Unbind = %q, %v", removed, err)
	}
	if _, err := repo.Unbind(ctx, ws, googleaccount.ProductSheets); !errors.Is(err, googleaccount.ErrNotConnected) {
		t.Fatalf("second Unbind err = %v", err)
	}
}

func TestDisconnectWipesTokensAndConnectedElsewhereSeesOtherWorkspaces(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	a := connected(uuid.NewString(), "shared@example.com")
	b := connected(uuid.NewString(), "shared@example.com")
	for _, acc := range []*googleaccount.Account{a, b} {
		if err := repo.SaveConnected(ctx, acc); err != nil {
			t.Fatal(err)
		}
	}
	elsewhere, err := repo.ConnectedElsewhere(ctx, "shared@example.com", a.ID)
	if err != nil || !elsewhere {
		t.Fatalf("ConnectedElsewhere = %v, %v; want true", elsewhere, err)
	}
	if err := repo.Disconnect(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.FindByID(ctx, b.ID)
	if stored.Status != googleaccount.StatusDisconnected || stored.AccessToken != "" || stored.RefreshToken != "" {
		t.Fatalf("disconnected account kept its tokens: %+v", stored)
	}
	elsewhere, err = repo.ConnectedElsewhere(ctx, "shared@example.com", a.ID)
	if err != nil || elsewhere {
		t.Fatalf("ConnectedElsewhere after disconnect = %v, %v; want false", elsewhere, err)
	}
}

func TestLockForRefreshLetsOneReplicaRefresh(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	account := connected(uuid.NewString(), "lock@example.com")
	if err := repo.SaveConnected(ctx, account); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = repo.LockForRefresh(ctx, account.ID, func(ctx context.Context, a *googleaccount.Account) (googleaccount.RefreshOutcome, error) {
			close(entered)
			<-release
			return googleaccount.RefreshOutcome{Tokens: &googleaccount.TokenUpdate{
				AccessToken: "fresh", RefreshToken: a.RefreshToken, AccessExpiresAt: time.Now().UTC().Add(time.Hour),
			}}, nil
		})
	}()
	<-entered
	acquired, err := repo.LockForRefresh(ctx, account.ID, func(context.Context, *googleaccount.Account) (googleaccount.RefreshOutcome, error) {
		t.Fatal("a second replica entered the refresh")
		return googleaccount.RefreshOutcome{}, nil
	})
	if err != nil || acquired {
		t.Fatalf("second LockForRefresh = %v, %v; want skipped", acquired, err)
	}
	close(release)
	wg.Wait()
	stored, _ := repo.FindByID(ctx, account.ID)
	if stored.AccessToken != "fresh" {
		t.Fatalf("refresh not stored: %q", stored.AccessToken)
	}
}

func TestRefreshOutcomeNeedsReconnectWipesTokens(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	account := connected(uuid.NewString(), "lost@example.com")
	if err := repo.SaveConnected(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.LockForRefresh(ctx, account.ID, func(context.Context, *googleaccount.Account) (googleaccount.RefreshOutcome, error) {
		return googleaccount.RefreshOutcome{Status: googleaccount.StatusNeedsReconnect, StatusReason: "invalid_grant"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.FindByID(ctx, account.ID)
	if stored.Status != googleaccount.StatusNeedsReconnect || stored.RefreshToken != "" {
		t.Fatalf("stored = %+v", stored)
	}
	due, err := repo.ListDueForRefresh(ctx, time.Now().Add(24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range due {
		if d.ID == account.ID {
			t.Fatal("an account that needs reconnect is still listed for refresh")
		}
	}
}
