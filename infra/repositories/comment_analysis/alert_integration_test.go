package comment_analysis_repository

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	ca "vozko/domain/comment_analysis"
)

// Integration tests for the alert claim, opt-in behind VOZKO_TEST_DB=1 like the
// rest of this package.
//
// These exist because the claim is the ONLY thing standing between "a rule
// fired" and "a real person got the same WhatsApp message four times". sqlmock
// cannot verify it: the property is about what Postgres does when several
// connections run the same UPDATE at once.

func seedAlertRule(t *testing.T, repo ca.AlertRuleRepository, ws string, mutate func(*ca.AlertRule)) *ca.AlertRule {
	t.Helper()
	rule := &ca.AlertRule{
		WorkspaceID: ws,
		Source:      ca.SourceInstagram,
		AccountID:   integrationRef().AccountID,
		Name:        "Comentário grave",
		Enabled:     true,
		Metric:      ca.AlertMetricCommentSeverity,
		Threshold:   80,
		Channel:     ca.AlertChannelUnofficial,
		Recipient:   "5511999999999",
	}
	if mutate != nil {
		mutate(rule)
	}
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		t.Fatalf("fixture is invalid: %v", err)
	}
	if err := repo.Create(context.Background(), rule); err != nil {
		t.Fatalf("create: %v", err)
	}
	return rule
}

// THE test. Several replicas evaluate the same batch, all decide the rule
// should fire, and exactly one is allowed to send.
func TestIntegration_AlertClaimIsExclusive(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, nil)

	const racers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)
	start := make(chan struct{})
	now := time.Now().UTC()

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, now)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if claimed {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Fatalf("%d of %d racers claimed the firing, want exactly 1", winners, racers)
	}

	stored, err := repo.FindByID(context.Background(), ws, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FiredToday != 1 {
		t.Fatalf("tally = %d, want 1: the losers must not have counted", stored.FiredToday)
	}
	if stored.LastFiredAt == nil {
		t.Fatal("the winner must have stamped the firing")
	}
}

// The cooldown is enforced by the claim itself, against the row's own setting,
// so a second firing inside it is refused even if the caller asks.
func TestIntegration_AlertClaimHonoursTheCooldown(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, func(r *ca.AlertRule) { r.CooldownMinutes = 30 })

	now := time.Now().UTC()
	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, now); err != nil || !claimed {
		t.Fatalf("first claim: %v %v", claimed, err)
	}

	// One minute later: still quiet.
	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, now.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("inside the cooldown: claimed=%v err=%v", claimed, err)
	}
	// Twenty-nine minutes later: still quiet.
	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, now.Add(29*time.Minute)); err != nil || claimed {
		t.Fatalf("still inside the cooldown: claimed=%v err=%v", claimed, err)
	}
	// Past it: fires again.
	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, now.Add(31*time.Minute)); err != nil || !claimed {
		t.Fatalf("past the cooldown: claimed=%v err=%v", claimed, err)
	}

	stored, _ := repo.FindByID(context.Background(), ws, rule.ID)
	if stored.FiredToday != 2 {
		t.Fatalf("tally = %d, want 2", stored.FiredToday)
	}
}

// The daily cap is the backstop for a condition that persists, and it has to
// roll over rather than silence the rule forever.
func TestIntegration_AlertClaimHonoursTheDailyCap(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, func(r *ca.AlertRule) {
		r.CooldownMinutes = ca.MinAlertCooldownMinutes
		r.MaxPerDay = 3
	})

	base := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	step := time.Duration(ca.MinAlertCooldownMinutes+1) * time.Minute

	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * step)
		if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, at); err != nil || !claimed {
			t.Fatalf("claim %d: claimed=%v err=%v", i+1, claimed, err)
		}
	}

	// The fourth is refused even though the cooldown has passed.
	fourth := base.Add(3 * step)
	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, fourth); err != nil || claimed {
		t.Fatalf("over the daily cap: claimed=%v err=%v", claimed, err)
	}

	// Tomorrow the tally resets.
	tomorrow := base.Add(24 * time.Hour)
	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, tomorrow); err != nil || !claimed {
		t.Fatalf("new day: claimed=%v err=%v", claimed, err)
	}
	stored, _ := repo.FindByID(context.Background(), ws, rule.ID)
	if stored.FiredToday != 1 {
		t.Fatalf("tally = %d on the new day, want 1", stored.FiredToday)
	}
}

// A rule switched off between being listed and being claimed does not fire.
func TestIntegration_AlertClaimRefusesADisabledRule(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, func(r *ca.AlertRule) { r.Enabled = false })

	if claimed, err := repo.ClaimFire(context.Background(), ws, rule.ID, time.Now().UTC()); err != nil || claimed {
		t.Fatalf("a disabled rule was claimed: claimed=%v err=%v", claimed, err)
	}
}

// Another workspace cannot claim, read, edit or delete this rule.
func TestIntegration_AlertRuleIsWorkspaceScoped(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	mine := "11111111-1111-1111-1111-111111111111"
	theirs := "33333333-3333-3333-3333-333333333333"
	rule := seedAlertRule(t, repo, mine, nil)

	ctx := context.Background()
	if claimed, err := repo.ClaimFire(ctx, theirs, rule.ID, time.Now().UTC()); err != nil || claimed {
		t.Fatalf("claimed across workspaces: claimed=%v err=%v", claimed, err)
	}
	if _, err := repo.FindByID(ctx, theirs, rule.ID); err != ca.ErrNotFound {
		t.Fatalf("find across workspaces: %v", err)
	}
	if err := repo.Delete(ctx, theirs, rule.ID); err != ca.ErrNotFound {
		t.Fatalf("delete across workspaces: %v", err)
	}

	other := *rule
	other.WorkspaceID = theirs
	other.Name = "roubado"
	if err := repo.Update(ctx, &other); err != ca.ErrNotFound {
		t.Fatalf("update across workspaces: %v", err)
	}
	still, err := repo.FindByID(ctx, mine, rule.ID)
	if err != nil || still.Name != rule.Name {
		t.Fatalf("the rule was modified from another workspace: %v %+v", err, still)
	}
}

// Saving a rule must not reset its cooldown. An operator toggling a setting
// mid-incident would otherwise let the alert fire again immediately.
func TestIntegration_UpdatingARuleDoesNotResetItsCooldown(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, func(r *ca.AlertRule) { r.CooldownMinutes = 60 })
	ctx := context.Background()

	now := time.Now().UTC()
	if claimed, _ := repo.ClaimFire(ctx, ws, rule.ID, now); !claimed {
		t.Fatal("first claim should win")
	}

	edited, err := repo.FindByID(ctx, ws, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited.Name = "Outro nome"
	edited.Threshold = 70
	if err := repo.Update(ctx, edited); err != nil {
		t.Fatal(err)
	}

	if claimed, err := repo.ClaimFire(ctx, ws, rule.ID, now.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("an edit reopened the cooldown: claimed=%v err=%v", claimed, err)
	}
	stored, _ := repo.FindByID(ctx, ws, rule.ID)
	if stored.Name != "Outro nome" || stored.Threshold != 70 {
		t.Fatalf("the edit was not saved: %+v", stored)
	}
	if stored.FiredToday != 1 {
		t.Fatalf("the tally was reset by an edit: %d", stored.FiredToday)
	}
}

// ListArmed is the engine's hot read: only enabled rules, only this account.
func TestIntegration_ListArmedReturnsOnlyLiveRules(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	ctx := context.Background()

	live := seedAlertRule(t, repo, ws, func(r *ca.AlertRule) { r.Name = "ligada" })
	seedAlertRule(t, repo, ws, func(r *ca.AlertRule) {
		r.Name = "desligada"
		r.Enabled = false
	})
	seedAlertRule(t, repo, ws, func(r *ca.AlertRule) {
		r.Name = "outra conta"
		r.AccountID = "44444444-4444-4444-4444-444444444444"
	})

	armed, err := repo.ListArmed(ctx, ca.SourceInstagram, integrationRef().AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(armed) != 1 || armed[0].ID != live.ID {
		t.Fatalf("armed = %d rules, want only the enabled one of this account", len(armed))
	}

	all, err := repo.ListByAccount(ctx, ws, ca.SourceInstagram, integrationRef().AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("the config list must show disabled rules too, got %d", len(all))
	}
}

// A failure is recorded for the operator without releasing the claim.
func TestIntegration_RecordFailureKeepsTheClaim(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	if claimed, _ := repo.ClaimFire(ctx, ws, rule.ID, now); !claimed {
		t.Fatal("first claim should win")
	}
	if err := repo.RecordFailure(ctx, ws, rule.ID, "whatsapp offline", now); err != nil {
		t.Fatal(err)
	}

	stored, _ := repo.FindByID(ctx, ws, rule.ID)
	if !strings.Contains(stored.LastError, "offline") {
		t.Fatalf("last error = %q", stored.LastError)
	}
	if claimed, _ := repo.ClaimFire(ctx, ws, rule.ID, now.Add(time.Minute)); claimed {
		t.Fatal("a failure must not reopen the rule for an immediate retry")
	}
}

// An oversized error message must not blow the column.
func TestIntegration_RecordFailureBoundsTheMessage(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, nil)

	huge := strings.Repeat("erro ", 500)
	if err := repo.RecordFailure(context.Background(), ws, rule.ID, huge, time.Now().UTC()); err != nil {
		t.Fatalf("a long provider error must not fail the write: %v", err)
	}
}

// The optional ids are uuid columns; leaving them blank must store NULL rather
// than be refused as an invalid uuid.
func TestIntegration_AlertRuleStoresBlankOptionalIDs(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, func(r *ca.AlertRule) { r.InstanceID = "" })
	ctx := context.Background()

	stored, err := repo.FindByID(ctx, ws, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.InstanceID != "" || stored.BusinessPhoneID != "" || stored.TemplateID != "" {
		t.Fatalf("blank ids came back as %+v", stored)
	}
	// And an update that clears them is equally fine.
	stored.InstanceID = ""
	if err := repo.Update(ctx, stored); err != nil {
		t.Fatalf("update with blank ids: %v", err)
	}
}
