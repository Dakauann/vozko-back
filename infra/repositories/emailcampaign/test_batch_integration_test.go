package emailcampaign

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	domain "vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
)

func TestTheTestBatchIsKeptWithItsStateAndCountedWithoutScans(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	repo := NewCampaignRepository(db)
	recipients := NewRecipientRepository(db)
	c := &domain.Campaign{WorkspaceID: uuid.NewString(), Name: "Lista nova", Status: domain.StatusDraft, Subject: "Oi",
		Document: emaildesign.Document{Version: 1}, CreatedBy: uuid.NewString()}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.MarkLaunched(ctx, c.ID, domain.StatusDraft, domain.Launch{StartedAt: time.Now().UTC()}); err != nil || !ok {
		t.Fatalf("launch %v %v", ok, err)
	}
	var rows []domain.Recipient
	for i := 0; i < 10; i++ {
		rows = append(rows, domain.Recipient{Address: emailaddress.Address(fmt.Sprintf("p%d@loja.com.br", i)), Status: domain.RecipientPending, Variant: domain.VariantA})
	}
	if err := recipients.Replace(ctx, c.WorkspaceID, c.ID, rows); err != nil {
		t.Fatal(err)
	}
	ids, err := recipients.PendingIDs(ctx, c.ID, []domain.Variant{domain.VariantA}, "", 4)
	if err != nil || len(ids) != 4 {
		t.Fatalf("ids = %v %v", ids, err)
	}
	tb := domain.TestBatch{Size: 4, State: domain.TestBatchTesting, LastID: ids[3]}
	if ok, err := repo.SetTestBatch(ctx, c.ID, domain.TestBatchNone, tb); err != nil || !ok {
		t.Fatalf("set = %v %v", ok, err)
	}
	if ok, _ := repo.SetTestBatch(ctx, c.ID, domain.TestBatchNone, tb); ok {
		t.Fatal("a test batch is set once")
	}
	testing_, err := repo.ListTesting(ctx, 10)
	if err != nil || len(testing_) != 1 || testing_[0].TestBatch == nil || testing_[0].TestBatch.LastID != ids[3] {
		t.Fatalf("testing = %+v %v", testing_, err)
	}
	pending, err := recipients.PendingThrough(ctx, c.ID, ids[3])
	if err != nil || pending != 4 {
		t.Fatalf("pending through = %d %v", pending, err)
	}
	if _, err := recipients.MarkSent(ctx, ids[0], "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if pending, _ = recipients.PendingThrough(ctx, c.ID, ids[3]); pending != 3 {
		t.Fatalf("pending through = %d after one send", pending)
	}
	drained, decided := time.Now().UTC().Truncate(time.Microsecond), time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	failed := tb
	failed.State, failed.DrainedAt, failed.DecidedAt = domain.TestBatchFailed, &drained, &decided
	if ok, err := repo.SetTestBatch(ctx, c.ID, domain.TestBatchTesting, failed); err != nil || !ok {
		t.Fatalf("fail = %v %v", ok, err)
	}
	got, err := repo.Get(ctx, c.WorkspaceID, c.ID)
	if err != nil || got.TestBatch == nil || got.TestBatch.State != domain.TestBatchFailed || !got.TestBatch.DrainedAt.Equal(drained) || !got.TestBatch.DecidedAt.Equal(decided) {
		t.Fatalf("got = %+v %v", got.TestBatch, err)
	}
	if left, _ := repo.ListTesting(ctx, 10); len(left) != 0 {
		t.Fatal("a decided test batch leaves the testing list")
	}
	var plan []string
	if err := db.Raw("EXPLAIN "+pendingThroughSQL, c.ID, string(domain.RecipientPending), ids[3]).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("pending through plan: %v", plan)
}
