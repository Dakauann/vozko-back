package emailhygiene_repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"vozko/domain/emailaddress"
)

const (
	capacityRows       = 200000
	capacityWorkspaces = 1000
	capacityProbe      = 1000
	capacityWorkspace  = 7
)

func capacityWorkspaceID(db *gorm.DB, t *testing.T) string {
	t.Helper()
	var id string
	if err := db.Raw("SELECT md5(?::text)::uuid::text", fmt.Sprint(capacityWorkspace)).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func loadCapacity(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		fmt.Sprintf(`INSERT INTO email_address_activity (workspace_id, address_hash, first_delivered_at, last_delivered_at, last_engaged_at, sunset_since,
  campaigns_since_engaged, soft_streak, last_outcome_message_id, touched_at)
SELECT md5((g %% %d)::text)::uuid, sha256(convert_to('a' || g || '@x' || (g %% 50) || '.com', 'UTF8')), now(), now(), now(), now(), 0, 0, '', now() - (g || ' minutes')::interval
FROM generate_series(1, %d) AS g`, capacityWorkspaces, capacityRows),
		fmt.Sprintf(`INSERT INTO email_bad_addresses (address_hash, reason, first_seen_at, last_seen_at)
SELECT sha256(convert_to('a' || g || '@x' || (g %% 50) || '.com', 'UTF8')), 'hard_bounce', now(), now() FROM generate_series(1, %d) AS g WHERE g %% 3 = 0`, capacityRows),
		fmt.Sprintf(`INSERT INTO email_suppressions (id, workspace_id, address, reason, source_message_id, suppressed_at, created_at, updated_at)
SELECT md5('s' || g)::uuid, md5((g %% %d)::text)::uuid, 'a' || g || '@x' || (g %% 50) || '.com', 'manual', '', now(), now(), now()
FROM generate_series(1, %d) AS g WHERE g %% 2 = 0`, capacityWorkspaces, capacityRows),
		fmt.Sprintf(`INSERT INTO email_domain_checks (domain, state, detail, expires_at, due_at, attempts, created_at, updated_at)
SELECT 'd' || g || '.com.br', 'accepts', 'mx', now() + interval '7 days', CASE WHEN g %% 100 = 0 THEN now() END, 0, now(), now() - (g || ' minutes')::interval
FROM generate_series(1, %d) AS g`, capacityRows),
		"ANALYZE email_address_activity", "ANALYZE email_bad_addresses", "ANALYZE email_suppressions", "ANALYZE email_domain_checks",
	}
	for _, s := range statements {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("load: %v\n%s", err, s)
		}
	}
}

func explain(t *testing.T, db *gorm.DB, sql string, args ...any) string {
	t.Helper()
	var lines []string
	if err := db.Raw("EXPLAIN "+sql, args...).Scan(&lines).Error; err != nil {
		t.Fatalf("explain: %v", err)
	}
	return strings.Join(lines, "\n")
}

func assertIndexOnly(t *testing.T, plan string, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if strings.Contains(plan, "Seq Scan on "+table) {
			t.Fatalf("sequential scan on %s:\n%s", table, plan)
		}
		if !strings.Contains(plan, table) {
			t.Fatalf("%s is not in the plan:\n%s", table, plan)
		}
	}
}

func TestIntegrationCapacityStandingReadUsesIndexesForAThousandAddresses(t *testing.T) {
	if testing.Short() {
		t.Skip("capacity load")
	}
	db := hygieneDB(t)
	loadCapacity(t, db)
	ws := capacityWorkspaceID(db, t)
	addresses := make([]emailaddress.Address, 0, capacityProbe)
	for g := capacityWorkspace; len(addresses) < capacityProbe; g += capacityWorkspaces / 4 {
		addresses = append(addresses, emailaddress.Address(fmt.Sprintf("a%d@x%d.com", g, g%50)))
	}
	plan := explain(t, db, StandingSQL, StandingArgs(addresses).Args(ws)...)
	assertIndexOnly(t, plan, "email_suppressions", "email_bad_addresses", "email_domain_checks", "email_address_activity")
	if !strings.Contains(plan, "email_address_activity_pkey") {
		t.Fatalf("the activity of a chunk must be read by its primary key, never by scanning the workspace:\n%s", plan)
	}
	reader := NewStanding(db)
	started := time.Now()
	got, err := reader.Standing(context.Background(), ws, addresses)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != capacityProbe {
		t.Fatalf("standing for %d addresses, want %d", len(got), capacityProbe)
	}
	t.Logf("standing of %d addresses over %d rows per table took %s\n%s", capacityProbe, capacityRows, time.Since(started), plan)
}

func TestIntegrationCapacityClaimAndPruneUseTheirIndexes(t *testing.T) {
	if testing.Short() {
		t.Skip("capacity load")
	}
	db := hygieneDB(t)
	loadCapacity(t, db)
	now := time.Now().UTC()
	claim := explain(t, db, domainClaimSQL, now.Add(time.Minute), now, now, 200)
	assertIndexOnly(t, claim, "email_domain_checks")
	if !strings.Contains(claim, "idx_email_domain_checks_due") {
		t.Fatalf("the claim must walk the due index:\n%s", claim)
	}
	activity := explain(t, db, activityPruneSQL, now.AddDate(0, 0, -100), 5000)
	assertIndexOnly(t, activity, "email_address_activity")
	if !strings.Contains(activity, "idx_email_address_activity_touched") {
		t.Fatalf("activity pruning must walk the touched index:\n%s", activity)
	}
	domains := explain(t, db, domainPruneSQL, now.AddDate(0, 0, -100), 5000)
	assertIndexOnly(t, domains, "email_domain_checks")
	if !strings.Contains(domains, "idx_email_domain_checks_updated") {
		t.Fatalf("domain pruning must walk the updated index:\n%s", domains)
	}
	t.Logf("claim:\n%s\nactivity prune:\n%s\ndomain prune:\n%s", claim, activity, domains)
}
