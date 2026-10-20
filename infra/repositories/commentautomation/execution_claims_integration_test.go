package commentautomation_repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestConcurrentClaimsForOneCommentHaveExactlyOneWinner(t *testing.T) {
	db := repotest.IsolatedDB(t, "cre_test", &schema.CommentRuleExecution{})
	claims := NewExecutionClaimRepository(db)
	ctx := context.Background()

	const contenders = 8
	var wg sync.WaitGroup
	results := make(chan bool, contenders)
	errs := make(chan error, contenders)
	start := make(chan struct{})
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			won, err := claims.Claim(ctx, shared.EntryTypeInstagram, "c-1", "rule-1")
			if err != nil {
				errs <- err
				return
			}
			results <- won
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("claim: %v", err)
	}
	winners := 0
	for won := range results {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}

	if won, err := claims.Claim(ctx, shared.EntryTypeFacebook, "c-1", "rule-2"); err != nil || !won {
		t.Fatalf("the same comment id on another source is its own claim: %v %v", won, err)
	}

	var stored schema.CommentRuleExecution
	if err := db.First(&stored, "source = ? AND comment_id = ?", "instagram", "c-1").Error; err != nil {
		t.Fatal(err)
	}
	if stored.RuleID != "rule-1" || stored.ClaimedAt.IsZero() {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestExecutionClaimsOlderThanTheCutoffArePurged(t *testing.T) {
	db := repotest.IsolatedDB(t, "cre_test", &schema.CommentRuleExecution{})
	claims := NewExecutionClaimRepository(db)
	ctx := context.Background()

	for _, id := range []string{"old", "fresh"} {
		if won, err := claims.Claim(ctx, shared.EntryTypeInstagram, id, "rule-1"); err != nil || !won {
			t.Fatalf("claim %s: %v %v", id, won, err)
		}
	}
	old := time.Now().UTC().Add(-31 * 24 * time.Hour)
	if err := db.Model(&schema.CommentRuleExecution{}).Where("comment_id = ?", "old").Update("claimed_at", old).Error; err != nil {
		t.Fatal(err)
	}

	purged, err := claims.PurgeOlderThan(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil || purged != 1 {
		t.Fatalf("purged %d, %v", purged, err)
	}
	if won, err := claims.Claim(ctx, shared.EntryTypeInstagram, "fresh", "rule-1"); err != nil || won {
		t.Fatalf("a fresh claim survives the purge: %v %v", won, err)
	}
}
