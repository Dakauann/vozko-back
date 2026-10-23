package emailsending_repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	es "vozko/domain/emailsending"
)

func seedReplyDomain(t *testing.T, domains es.DomainRepository, ws string, kind es.DomainKind, name string, retired bool) *es.SendingDomain {
	t.Helper()
	d := &es.SendingDomain{WorkspaceID: ws, AccountID: uuid.NewString(), Domain: name, Kind: kind, Status: es.DomainVerified,
		ConnectMethod: es.ConnectManual, MailFromDomain: "bounce." + name, ReplyDomain: "reply." + name, Retired: retired}
	if err := domains.Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d
}

func seedWallet(t *testing.T, db *gorm.DB, ws string, amount int64) {
	t.Helper()
	if err := db.Exec("INSERT INTO balances (id, workspace_id, amount) VALUES (?, ?, ?)", uuid.NewString(), ws, amount).Error; err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationReplyPauses(t *testing.T) {
	db := integrationDB(t)
	if err := db.Exec("CREATE TABLE balances (id uuid PRIMARY KEY, workspace_id uuid NOT NULL UNIQUE, amount bigint NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	domains := NewDomainRepository(db)
	pauses := NewReplyPauseRepository(db)
	inDebt, toppedUp, settled, zero, removedOnly := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	debtDomain := seedReplyDomain(t, domains, inDebt, es.DomainOwn, "devendo.com.br", false)
	toppedDomain := seedReplyDomain(t, domains, toppedUp, es.DomainOwn, "pagou.com.br", false)
	seedReplyDomain(t, domains, settled, es.DomainOwn, "tranquila.com.br", false)
	zeroDomain := seedReplyDomain(t, domains, zero, es.DomainOwn, "zerada.com.br", false)
	seedReplyDomain(t, domains, removedOnly, es.DomainOwn, "removida.com.br", true)
	seedWallet(t, db, inDebt, -1)
	seedWallet(t, db, toppedUp, 0)
	seedWallet(t, db, settled, 100)
	seedWallet(t, db, zero, 0)
	seedWallet(t, db, removedOnly, -50)

	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	if err := pauses.MarkRepliesPaused(ctx, toppedUp, toppedDomain.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := pauses.MarkRepliesPaused(ctx, toppedUp, toppedDomain.ID, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := domains.Get(ctx, toppedUp, toppedDomain.ID)
	if err != nil || got.RepliesPausedAt == nil || !got.RepliesPausedAt.Equal(at) {
		t.Fatalf("a second pause keeps the first stamp: %+v, %v", got, err)
	}
	got.FailureReason = "touched"
	if err := domains.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := domains.Get(ctx, toppedUp, toppedDomain.ID); again.RepliesPausedAt == nil {
		t.Fatal("an ordinary domain update must never clear the pause")
	}
	if err := pauses.MarkRepliesPaused(ctx, inDebt, toppedDomain.ID, at); !errors.Is(err, es.ErrDomainNotFound) {
		t.Fatalf("another workspace's domain = %v", err)
	}

	holds, err := pauses.ListReplyHoldMismatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{inDebt: -1, toppedUp: 0}
	if len(holds) != len(want) {
		t.Fatalf("holds = %+v, want %v", holds, want)
	}
	for _, h := range holds {
		if balance, ok := want[h.WorkspaceID]; !ok || balance != h.BalanceMicros || es.InboundPaused(h.BalanceMicros) != (h.WorkspaceID == inDebt) {
			t.Fatalf("hold %+v disagrees with the domain rule", h)
		}
	}

	if err := pauses.ClearRepliesPaused(ctx, toppedUp, toppedDomain.ID); err != nil {
		t.Fatal(err)
	}
	if err := pauses.MarkRepliesPaused(ctx, inDebt, debtDomain.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := pauses.MarkRepliesPaused(ctx, zero, zeroDomain.ID, at); err != nil {
		t.Fatal(err)
	}
	holds, err = pauses.ListReplyHoldMismatches(ctx)
	if err != nil || len(holds) != 1 || holds[0].WorkspaceID != zero {
		t.Fatalf("after settling: %+v, %v", holds, err)
	}

	all, err := pauses.ListAll(ctx)
	if err != nil || len(all) != 5 {
		t.Fatalf("all = %d, %v", len(all), err)
	}
	var paused []string
	for _, d := range all {
		if d.RepliesPaused() {
			paused = append(paused, d.ID)
		}
	}
	slices.Sort(paused)
	wantPaused := []string{debtDomain.ID, zeroDomain.ID}
	slices.Sort(wantPaused)
	if !slices.Equal(paused, wantPaused) {
		t.Fatalf("paused = %v, want %v", paused, wantPaused)
	}
}
