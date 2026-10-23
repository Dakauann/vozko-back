package emailhygiene_repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func hygieneDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "email_hygiene_test", schema.EmailSendingModels()...)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(20)
	return db
}

var clock = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func TestSQLPlaceholdersMatchTheirArguments(t *testing.T) {
	cases := map[string]int{
		domainRequestSQL: 6, domainClaimSQL: 4, domainSaveSQL: 9, domainPruneSQL: 2, StandingSQL: 6,
		badAddressSQL: 3, deliveredSQL: 9, softBouncedSQL: 5, streakSQL: 2, engagedSQL: 5, activityPruneSQL: 2, keepNewestSQL: 1,
	}
	for sql, args := range cases {
		if got := strings.Count(sql, "?"); got != args {
			t.Fatalf("%d placeholders for %d arguments in %s", got, args, sql)
		}
	}
}

func TestIntegrationDomainChecksRequestOnlyQueuesMissingOrStaleDomains(t *testing.T) {
	db := hygieneDB(t)
	store := NewDomainChecks(db)
	ctx := context.Background()
	n, err := store.Request(ctx, []string{"loja.com.br", "loja.com.br", "gmail.com"}, clock)
	if err != nil || n != 2 {
		t.Fatalf("request = %d, %v, want two new domains", n, err)
	}
	if n, err = store.Request(ctx, []string{"loja.com.br"}, clock.Add(time.Minute)); err != nil || n != 0 {
		t.Fatalf("an already queued domain is not queued again, got %d %v", n, err)
	}
	claimed, err := store.ClaimDue(ctx, clock.Add(time.Minute), 10, time.Minute)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claimed = %+v, %v", claimed, err)
	}
	if again, _ := store.ClaimDue(ctx, clock.Add(90*time.Second), 10, time.Minute); len(again) != 0 {
		t.Fatalf("a leased domain is not claimed twice, got %+v", again)
	}
	accepts := emailhygiene.DomainVerdict{State: emailhygiene.DomainAccepts, Detail: emailhygiene.DetailMX}
	if err := store.Save(ctx, []emailhygiene.DomainResult{
		{Domain: "loja.com.br", Verdict: accepts, Settled: emailhygiene.Settle(accepts, 1, clock)},
		{Domain: "gmail.com", Verdict: accepts, Settled: emailhygiene.Settle(accepts, 1, clock)},
	}, clock); err != nil {
		t.Fatal(err)
	}
	if n, _ = store.Request(ctx, []string{"loja.com.br"}, clock.Add(time.Hour)); n != 0 {
		t.Fatalf("a fresh verdict is not queued, got %d", n)
	}
	if n, _ = store.Request(ctx, []string{"loja.com.br"}, clock.Add(8*24*time.Hour)); n != 1 {
		t.Fatalf("a stale verdict is queued again, got %d", n)
	}
	read, err := store.Read(ctx, []string{"loja.com.br", "nowhere.com"})
	if err != nil {
		t.Fatal(err)
	}
	if read["loja.com.br"].State != emailhygiene.DomainAccepts {
		t.Fatalf("a stale verdict keeps its state until refreshed, got %+v", read["loja.com.br"])
	}
	if _, ok := read["nowhere.com"]; ok {
		t.Fatal("an unknown domain has no row")
	}
}

func TestIntegrationDomainClaimsNeverOverlapAcrossWorkers(t *testing.T) {
	db := hygieneDB(t)
	store := NewDomainChecks(db)
	ctx := context.Background()
	domains := make([]string, 300)
	for i := range domains {
		domains[i] = fmt.Sprintf("d%03d.com.br", i)
	}
	if _, err := store.Request(ctx, domains, clock); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				got, err := store.ClaimDue(ctx, clock.Add(time.Second), 25, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if len(got) == 0 {
					return
				}
				mu.Lock()
				for _, c := range got {
					seen[c.Domain]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != len(domains) {
		t.Fatalf("claimed %d domains, want %d", len(seen), len(domains))
	}
	for d, n := range seen {
		if n != 1 {
			t.Fatalf("%s was claimed %d times", d, n)
		}
	}
}

func TestIntegrationUnknownVerdictComesBackWhenDue(t *testing.T) {
	db := hygieneDB(t)
	store := NewDomainChecks(db)
	ctx := context.Background()
	unknown := emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailTimeout}
	if err := store.Save(ctx, []emailhygiene.DomainResult{{Domain: "slow.com.br", Verdict: unknown, Attempts: 1, Settled: emailhygiene.Settle(unknown, 1, clock)}}, clock); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ClaimDue(ctx, clock.Add(29*time.Minute), 10, time.Minute); len(got) != 0 {
		t.Fatalf("not due yet, got %+v", got)
	}
	got, err := store.ClaimDue(ctx, clock.Add(31*time.Minute), 10, time.Minute)
	if err != nil || len(got) != 1 || got[0].Attempts != 1 || got[0].State != emailhygiene.DomainUnknown {
		t.Fatalf("claimed = %+v, %v", got, err)
	}
}

func TestIntegrationStandingJoinsEverySignalInOneRead(t *testing.T) {
	db := hygieneDB(t)
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	suppressed := emailaddress.Address("ana@loja.com.br")
	bad := emailaddress.Address("gone@loja.com.br")
	nomail := emailaddress.Address("bia@nomail.com.br")
	fresh := emailaddress.Address("caio@new.com.br")
	if err := db.Create(&schema.EmailSuppression{ID: uuid.NewString(), WorkspaceID: ws, Address: string(suppressed), Reason: "complaint", SuppressedAt: clock, CreatedAt: clock, UpdatedAt: clock}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schema.EmailSuppression{ID: uuid.NewString(), WorkspaceID: other, Address: string(bad), Reason: "manual", SuppressedAt: clock, CreatedAt: clock, UpdatedAt: clock}).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewBadAddresses(db).Add(ctx, emailhygiene.HashOf(bad), clock); err != nil {
		t.Fatal(err)
	}
	checks := NewDomainChecks(db)
	accepts := emailhygiene.DomainVerdict{State: emailhygiene.DomainAccepts}
	noMail := emailhygiene.DomainVerdict{State: emailhygiene.DomainNoMail, Detail: emailhygiene.DetailNullMX}
	if err := checks.Save(ctx, []emailhygiene.DomainResult{
		{Domain: "loja.com.br", Verdict: accepts, Settled: emailhygiene.Settle(accepts, 1, clock)},
		{Domain: "nomail.com.br", Verdict: noMail, Settled: emailhygiene.Settle(noMail, 1, clock)},
	}, clock); err != nil {
		t.Fatal(err)
	}
	if err := NewActivity(db).Delivered(ctx, ws, emailhygiene.HashOf(suppressed), "m1", true, clock); err != nil {
		t.Fatal(err)
	}
	reader := NewStanding(db)
	reader.now = func() time.Time { return clock }
	got, err := reader.Standing(ctx, ws, []emailaddress.Address{suppressed, bad, nomail, fresh, suppressed})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("one standing per distinct address, got %d", len(got))
	}
	if s := got[suppressed]; s.Suppression != es.SuppressComplaint || s.Domain != emailhygiene.DomainAccepts || !s.Activity.EverDelivered() || s.Activity.CampaignsSinceEngaged != 1 {
		t.Fatalf("suppressed = %+v", s)
	}
	if s := got[bad]; !s.BadAddress || s.Suppression != "" || s.Refusal() != es.SuppressBadAddress {
		t.Fatalf("another workspace's suppression never leaks, the shared hard bounce does: %+v", s)
	}
	if s := got[nomail]; s.Domain != emailhygiene.DomainNoMail || s.Refusal() != es.SuppressNoMailDomain {
		t.Fatalf("nomail = %+v", s)
	}
	if s := got[fresh]; s.Domain != emailhygiene.DomainPending || !s.DomainStale || s.Activity.Known {
		t.Fatalf("an unchecked domain is pending and stale: %+v", s)
	}
}

func TestIntegrationActivityIsIdempotentPerMessage(t *testing.T) {
	db := hygieneDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	hash := emailhygiene.HashOf("ana@loja.com.br")
	store := NewActivity(db)
	for i := 0; i < 3; i++ {
		if err := store.Delivered(ctx, ws, hash, "m1", true, clock); err != nil {
			t.Fatal(err)
		}
	}
	read := func() emailhygiene.Activity {
		reader := NewStanding(db)
		got, err := reader.Standing(ctx, ws, []emailaddress.Address{"ana@loja.com.br"})
		if err != nil {
			t.Fatal(err)
		}
		return got["ana@loja.com.br"].Activity
	}
	if a := read(); a.CampaignsSinceEngaged != 1 || !a.SunsetSince.Equal(clock) {
		t.Fatalf("a replayed delivery counts once: %+v", a)
	}
	var streak emailhygiene.SoftStreak
	for i, msg := range []string{"m2", "m2", "m3"} {
		got, err := store.SoftBounced(ctx, ws, hash, msg, clock.Add(time.Duration(i)*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		streak = got
	}
	if streak.Count != 2 || !streak.StartedAt.Equal(clock) {
		t.Fatalf("a replayed soft bounce counts once and the streak keeps its start: %+v", streak)
	}
	if err := store.Delivered(ctx, ws, hash, "m4", false, clock.Add(5*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if a := read(); a.SoftStreak != 0 || a.SoftStreakStartedAt != nil || a.CampaignsSinceEngaged != 1 {
		t.Fatalf("a delivery resets the streak and a one-to-one delivery is not a campaign: %+v", a)
	}
	if err := store.Engaged(ctx, ws, hash, clock.Add(6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Engaged(ctx, ws, hash, clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	a := read()
	if a.CampaignsSinceEngaged != 0 || !a.LastEngagedAt.Equal(clock.Add(6*24*time.Hour)) || !a.SunsetSince.Equal(clock.Add(6*24*time.Hour)) {
		t.Fatalf("engagement restarts the sunset clock and a late older event never moves it back: %+v", a)
	}
}

func TestIntegrationBadAddressesAreKeptOnceAndForever(t *testing.T) {
	db := hygieneDB(t)
	ctx := context.Background()
	store := NewBadAddresses(db)
	hash := emailhygiene.HashOf("gone@loja.com.br")
	for _, at := range []time.Time{clock.Add(time.Hour), clock, clock.Add(time.Hour)} {
		if err := store.Add(ctx, hash, at); err != nil {
			t.Fatal(err)
		}
	}
	var rows []schema.EmailBadAddress
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].FirstSeenAt.Equal(clock) || !rows[0].LastSeenAt.Equal(clock.Add(time.Hour)) || len(rows[0].AddressHash) != 32 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestIntegrationPruningRunsInBoundedBatches(t *testing.T) {
	db := hygieneDB(t)
	ctx := context.Background()
	activity := NewActivity(db)
	ws := uuid.NewString()
	for i := 0; i < 7; i++ {
		at := clock.AddDate(-3, 0, 0)
		if i >= 5 {
			at = clock
		}
		if err := activity.Engaged(ctx, ws, emailhygiene.HashOf(emailaddress.Address(fmt.Sprintf("p%d@loja.com.br", i))), at); err != nil {
			t.Fatal(err)
		}
	}
	n, err := activity.Prune(ctx, clock.AddDate(-2, 0, 0), 3)
	if err != nil || n != 3 {
		t.Fatalf("first batch = %d, %v", n, err)
	}
	if n, _ = activity.Prune(ctx, clock.AddDate(-2, 0, 0), 3); n != 2 {
		t.Fatalf("second batch = %d", n)
	}
	if n, _ = activity.Prune(ctx, clock.AddDate(-2, 0, 0), 3); n != 0 {
		t.Fatalf("recent rows stay, got %d", n)
	}
	checks := NewDomainChecks(db)
	accepts := emailhygiene.DomainVerdict{State: emailhygiene.DomainAccepts}
	if err := checks.Save(ctx, []emailhygiene.DomainResult{{Domain: "old.com.br", Verdict: accepts, Settled: emailhygiene.Settle(accepts, 1, clock)}}, clock.AddDate(-1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := checks.Request(ctx, []string{"queued.com.br"}, clock.AddDate(-1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if n, err = checks.Prune(ctx, clock.AddDate(0, -6, 0), 100); err != nil || n != 1 {
		t.Fatalf("an idle checked domain is pruned and a queued one is kept, got %d %v", n, err)
	}
	lists := NewDisposableLists(db)
	for i := 0; i < 5; i++ {
		if err := lists.Save(ctx, fmt.Sprintf("list%d.com\n", i), 1, clock.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err = lists.KeepNewest(ctx, 3); err != nil || n != 2 {
		t.Fatalf("keep newest = %d, %v", n, err)
	}
	latest, err := lists.Latest(ctx)
	if err != nil || latest == nil || latest.Content != "list4.com\n" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
}

func TestIntegrationLatestRefusesATamperedList(t *testing.T) {
	db := hygieneDB(t)
	ctx := context.Background()
	lists := NewDisposableLists(db)
	if latest, err := lists.Latest(ctx); latest != nil || err != nil {
		t.Fatalf("no stored list yet answers nothing: %+v %v", latest, err)
	}
	if err := lists.Save(ctx, "a.com\n", 1, clock); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE email_disposable_lists SET content = 'b.com'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := lists.Latest(ctx); err == nil {
		t.Fatal("a list whose checksum no longer matches is refused")
	}
}
