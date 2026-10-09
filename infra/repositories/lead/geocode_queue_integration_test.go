package lead

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
	"vozko/infra/database"
)

func geocodeQueueDB(t *testing.T) (*repository, *GeocodeQueue) {
	t.Helper()
	db, repo := collectionsDB(t)
	sql, ok := database.ConcurrentIndexSQL(database.GeocodeQueueIndex)
	if !ok {
		t.Fatal("the geocode queue index is not declared")
	}
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("queue index: %v", err)
	}
	return repo, NewGeocodeQueue(db, nil)
}

func leadWithAddress(t *testing.T, repo *repository, ws, name string, p address.Postal) *lead.Lead {
	t.Helper()
	return newLead(t, repo, ws, lead.Draft{Name: name, Addresses: []lead.AddressInput{{Label: lead.AddressHome, Primary: true, Postal: p}}})
}

func claimRequest(token string, now time.Time, limit, turn int) geocoding.ClaimRequest {
	return geocoding.ClaimRequest{Token: token, Now: now, Lease: geocoding.Lease, Limit: limit, WorkspaceTurn: turn}
}

func TestTwoOverlappingSweepsNeverClaimTheSameAddressAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	for i := 0; i < 120; i++ {
		leadWithAddress(t, repo, ws, "Pessoa "+uuid.NewString()[:6], paulista)
	}
	now := time.Now().UTC().Add(time.Minute)
	var mu sync.Mutex
	seen := map[string]string{}
	var wg sync.WaitGroup
	for _, token := range []string{uuid.NewString(), uuid.NewString(), uuid.NewString()} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			for {
				claims, err := q.Claim(context.Background(), claimRequest(token, now, 25, 25))
				if err != nil {
					t.Errorf("Claim() err = %v", err)
					return
				}
				if len(claims) == 0 {
					return
				}
				mu.Lock()
				for _, c := range claims {
					if other, taken := seen[c.AddressID]; taken {
						t.Errorf("address %s claimed by %s and %s", c.AddressID, other, token)
					}
					seen[c.AddressID] = token
				}
				mu.Unlock()
			}
		}(token)
	}
	wg.Wait()
	if len(seen) != 120 {
		t.Fatalf("claimed %d addresses, want all 120 exactly once", len(seen))
	}
	again, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now, 500, 500))
	if err != nil || len(again) != 0 {
		t.Fatalf("a claim inside the lease = %d rows, %v, want nothing until the lease ends", len(again), err)
	}
	after, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now.Add(geocoding.Lease+time.Second), 500, 500))
	if err != nil || len(after) != 120 {
		t.Fatalf("a claim after the lease = %d rows, %v, want every address back", len(after), err)
	}
	if after[0].Attempts != 2 {
		t.Fatalf("attempts = %d, want 2 after two claims", after[0].Attempts)
	}
}

func TestClaimTakesTurnsBetweenWorkspacesAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	busy, quiet := uuid.NewString(), uuid.NewString()
	for i := 0; i < 30; i++ {
		leadWithAddress(t, repo, busy, "Grande "+uuid.NewString()[:6], paulista)
	}
	for i := 0; i < 3; i++ {
		leadWithAddress(t, repo, quiet, "Pequeno "+uuid.NewString()[:6], bahia)
	}
	claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), time.Now().UTC().Add(time.Minute), 12, 5))
	if err != nil {
		t.Fatal(err)
	}
	per := map[string]int{}
	for _, c := range claims {
		per[c.WorkspaceID]++
	}
	if per[busy] != 5 || per[quiet] != 3 {
		t.Fatalf("claimed per workspace = %+v, want the busy workspace held to its turn of 5 and the quiet one served", per)
	}
}

func TestSettleWritesOnlyUnderTheLiveClaimAndBumpsTheLeadAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	kept := leadWithAddress(t, repo, ws, "Ana", paulista)
	edited := leadWithAddress(t, repo, ws, "Bia", paulista)
	now := time.Now().UTC().Add(time.Minute)
	token := uuid.NewString()
	claims, err := q.Claim(context.Background(), claimRequest(token, now, 10, 10))
	if err != nil || len(claims) != 2 {
		t.Fatalf("Claim() = %d, %v", len(claims), err)
	}
	moved := reload(t, repo, ws, edited.ID)
	moved.Addresses[0].Postal = bahia
	moved.Addresses[0].GeoStatus = lead.GeoPending
	if err := repo.Save(context.Background(), moved, moved.Version, nil); err != nil {
		t.Fatalf("an edit during the lease: %v", err)
	}
	fix := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionStreet, Source: geo.SourceReference, FixedAt: now}
	var settlements []geocoding.Settlement
	for _, c := range claims {
		a := c.Address
		a.Fix, a.GeoStatus = &fix, lead.GeoLocated
		settlements = append(settlements, geocoding.Settlement{Claim: c, Resolution: geocoding.Resolution{Address: a, Changed: true}})
	}
	if _, err := q.Settle(context.Background(), uuid.NewString(), now, settlements); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, repo, ws, kept.ID); got.Addresses[0].Fix != nil {
		t.Fatalf("a write under another token landed: %+v", got.Addresses[0])
	}
	result, err := q.Settle(context.Background(), token, now, settlements)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 1 || result.Stale != 1 || result.Leads != 1 {
		t.Fatalf("Settle() = %+v, want the edited address refused as stale", result)
	}
	located := reload(t, repo, ws, kept.ID)
	if located.Addresses[0].Fix == nil || located.Addresses[0].Fix.Precision != geo.PrecisionStreet || located.Addresses[0].GeoStatus != lead.GeoLocated {
		t.Fatalf("located address = %+v", located.Addresses[0])
	}
	if located.Version != kept.Version+1 {
		t.Fatalf("version = %d, want %d: a written position bumps the lead", located.Version, kept.Version+1)
	}
	still := reload(t, repo, ws, edited.ID)
	if still.Addresses[0].Fix != nil || still.Addresses[0].Postal.City != "Belo Horizonte" || still.Addresses[0].GeoStatus != lead.GeoPending {
		t.Fatalf("edited address = %+v, want the new text kept pending", still.Addresses[0])
	}
	requeued, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now, 10, 10))
	if err != nil || len(requeued) != 1 || requeued[0].LeadID != edited.ID {
		t.Fatalf("after the write-back the queue holds %d, %v, want only the edited address", len(requeued), err)
	}
	backlog, err := q.Backlog(context.Background())
	if err != nil || backlog[lead.GeoPending] != 1 {
		t.Fatalf("Backlog() = %+v, %v", backlog, err)
	}
}

func TestArmSchedulesQueuedAddressesWithoutATimeAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	leadWithAddress(t, repo, ws, "Ana", paulista)
	if err := repo.db.Exec("UPDATE lead_addresses SET geo_next_at = NULL").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	armed, err := q.Arm(context.Background(), now)
	if err != nil || armed != 1 {
		t.Fatalf("Arm() = %d, %v, want one address scheduled", armed, err)
	}
	claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now.Add(time.Second), 10, 10))
	if err != nil || len(claims) != 1 {
		t.Fatalf("Claim() after arming = %d, %v", len(claims), err)
	}
}

func TestClaimRotatesThroughEveryBusyWorkspaceAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	var workspaces []string
	for i := 0; i < 7; i++ {
		ws := uuid.NewString()
		workspaces = append(workspaces, ws)
		for j := 0; j < 4; j++ {
			leadWithAddress(t, repo, ws, "Pessoa "+uuid.NewString()[:6], paulista)
		}
	}
	now := time.Now().UTC().Add(time.Minute)
	served := map[string]int{}
	after := ""
	for batch := 0; batch < 2; batch++ {
		req := claimRequest(uuid.NewString(), now, 10, 2)
		req.After = after
		claims, err := q.Claim(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if len(claims) != 10 {
			t.Fatalf("batch %d claimed %d addresses, want the limit of 10", batch, len(claims))
		}
		for _, c := range claims {
			served[c.WorkspaceID]++
		}
		after = geocoding.NextTurn(after, claims)
	}
	for _, ws := range workspaces {
		if served[ws] == 0 {
			t.Fatalf("served per workspace = %+v, want all 7 busy workspaces served within two batches", served)
		}
	}
}

func TestWakeUnavailableBringsParkedAddressesBackAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	parked := leadWithAddress(t, repo, ws, "Ana", paulista)
	located := leadWithAddress(t, repo, ws, "Bia", paulista)
	later := time.Now().UTC().Add(6 * time.Hour)
	if err := repo.db.Exec("UPDATE lead_addresses SET geo_status = 'unavailable', geo_next_at = ? WHERE lead_id = ?", later, parked.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.db.Exec("UPDATE lead_addresses SET geo_status = 'located', geo_next_at = NULL WHERE lead_id = ?", located.ID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now, 10, 10)); err != nil || len(claims) != 0 {
		t.Fatalf("before waking, Claim() = %d, %v, want the parked address still waiting", len(claims), err)
	}
	woken, err := q.WakeUnavailable(context.Background(), now)
	if err != nil || woken != 1 {
		t.Fatalf("WakeUnavailable() = %d, %v, want the one parked address", woken, err)
	}
	claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now.Add(time.Second), 10, 10))
	if err != nil || len(claims) != 1 || claims[0].LeadID != parked.ID {
		t.Fatalf("after waking, Claim() = %+v, %v, want the parked address", claims, err)
	}
}

func TestClaimSkipsAddressesOfSoftDeletedLeadsAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	live := leadWithAddress(t, repo, ws, "Ana", paulista)
	gone := leadWithAddress(t, repo, ws, "Bia", paulista)
	if err := repo.db.Exec("UPDATE leads SET deleted_at = now() WHERE id = ?", gone.ID).Error; err != nil {
		t.Fatal(err)
	}
	claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), time.Now().UTC().Add(time.Minute), 10, 10))
	if err != nil || len(claims) != 1 || claims[0].LeadID != live.ID {
		t.Fatalf("Claim() = %+v, %v, want only the live lead's address", claims, err)
	}
	var attempts int
	if err := repo.db.Raw("SELECT geo_attempts FROM lead_addresses WHERE lead_id = ?", gone.ID).Scan(&attempts).Error; err != nil || attempts != 0 {
		t.Fatalf("the deleted lead's address was touched: attempts = %d, %v", attempts, err)
	}
}

func TestClaimStaysOnTheQueueIndexWithTheLiveLeadCheckAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	db := repo.db
	seed := []string{
		"INSERT INTO leads (id, workspace_id, created_at, updated_at, deleted_at)" +
			" SELECT gen_random_uuid(), ('00000000-0000-0000-0000-' || lpad((g % 40)::text, 12, '0'))::uuid, now(), now()," +
			" CASE WHEN g % 50 = 0 THEN now() END FROM generate_series(1, 60000) g",
		"INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, geo_status, geo_next_at, fingerprint, created_at, updated_at)" +
			" SELECT gen_random_uuid(), l.workspace_id, l.id, 'home', true," +
			" CASE WHEN random() < 0.05 THEN 'pending' ELSE 'located' END, now() - (random() * interval '1 day'), md5(l.id::text)::varchar(32), now(), now()" +
			" FROM leads l",
		"ANALYZE leads",
		"ANALYZE lead_addresses",
	}
	for _, statement := range seed {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	now := time.Now().UTC()
	var plan []string
	if err := db.Raw("EXPLAIN "+geocodeClaimSQL, zeroUUID, now, geocoding.WorkspaceTurn, geocoding.BatchSize, now.Add(geocoding.Lease), uuid.NewString()).Scan(&plan).Error; err != nil {
		t.Fatalf("explain: %v", err)
	}
	text := strings.Join(plan, "\n")
	if strings.Count(text, database.GeocodeQueueIndex) < 2 || strings.Contains(text, "Seq Scan on leads") || !strings.Contains(text, "leads_pkey") {
		t.Fatalf("the workspace list and each turn must stay on the queue index, with the live lead check as a primary key probe:\n%s", text)
	}
	claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), now.Add(time.Minute), geocoding.BatchSize, geocoding.WorkspaceTurn))
	if err != nil || len(claims) == 0 {
		t.Fatalf("Claim() = %d, %v", len(claims), err)
	}
	ids := make([]string, 0, len(claims))
	for _, c := range claims {
		ids = append(ids, c.LeadID)
	}
	var deleted int64
	if err := db.Raw("SELECT count(*) FROM leads WHERE id::text = ANY(?) AND deleted_at IS NOT NULL", pq.StringArray(ids)).Scan(&deleted).Error; err != nil || deleted != 0 {
		t.Fatalf("claimed %d addresses of deleted leads, %v", deleted, err)
	}
}
