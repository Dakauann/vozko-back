package lead

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/address"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database"
)

var (
	paulista = address.Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP"}
	bahia    = address.Postal{ZipCode: "30130010", Street: "Rua da Bahia", Number: "10", District: "Centro", City: "Belo Horizonte", State: "MG"}
)

func collectionsDB(t *testing.T) (*gorm.DB, *repository) {
	t.Helper()
	db := leadStoreDB(t)
	if err := db.Exec("CREATE UNIQUE INDEX ux_leads_workspace_id_id ON leads (workspace_id, id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureCompositeLeadKeys(db); err != nil {
		t.Fatalf("composite keys: %v", err)
	}
	if err := database.EnsureCompositeLeadKeys(db); err != nil {
		t.Fatalf("composite keys must be idempotent: %v", err)
	}
	return db, &repository{db: db}
}

func newLead(t *testing.T, repo *repository, ws string, d lead.Draft) *lead.Lead {
	t.Helper()
	l, err := lead.New(ws, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(context.Background(), l, nil); err != nil {
		t.Fatalf("insert %s: %v", d.Name, err)
	}
	return l
}

func reload(t *testing.T, repo *repository, ws, id string) *lead.Lead {
	t.Helper()
	l, err := repo.Load(context.Background(), ws, id)
	if err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	return l
}

func TestPhonesAndAddressesRoundTripWithTheLeadAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Name: "Maria", Number: "5511987654321",
		Phones:    []lead.ContactPhone{{Number: "1133334444", Label: lead.PhoneLandline}},
		Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: paulista}, {Label: lead.AddressWork, Primary: true, Postal: bahia}}})

	stored := reload(t, repo, ws, maria.ID)
	if len(stored.Phones) != 1 || stored.Phones[0].Number != "551133334444" || len(stored.Addresses) != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	home, work := stored.Addresses[0], stored.Addresses[1]
	if home.GeoStatus != lead.GeoPending || !work.Primary {
		t.Fatalf("addresses = %+v", stored.Addresses)
	}
	if err := db.Exec("UPDATE lead_addresses SET latitude = -23.56, longitude = -46.65, geo_precision = 'postal_code', geo_source = 'reference', geo_status = 'approximate', geo_attempts = 2 WHERE id = ?", home.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE lead_addresses SET geo_attempts = 3 WHERE id = ?", work.ID).Error; err != nil {
		t.Fatal(err)
	}

	next := reload(t, repo, ws, maria.ID)
	phones := []lead.ContactPhone{{ID: stored.Phones[0].ID, Number: "551133334444", Label: lead.PhoneWork}}
	addresses := []lead.AddressInput{
		{ID: home.ID, Label: lead.AddressHome, Primary: true, Postal: paulista},
		{ID: work.ID, Label: lead.AddressWork, Postal: address.Postal{ZipCode: "30140071", City: "Belo Horizonte", State: "MG"}},
	}
	before := *next
	if err := next.ApplyEdit(lead.Edit{Phones: &phones, Addresses: &addresses}, time.Now()); err != nil {
		t.Fatal(err)
	}
	event := lead.Changes(lead.EventUpdated, uuid.NewString(), &before, next, nil)
	if err := repo.Save(ctx, next, before.Version, []recordevent.Event{event}); err != nil {
		t.Fatalf("save: %v", err)
	}

	after := reload(t, repo, ws, maria.ID)
	if after.Phones[0].Label != lead.PhoneWork || after.Phones[0].ID != stored.Phones[0].ID {
		t.Fatalf("phones = %+v", after.Phones)
	}
	if !after.Addresses[0].Primary || after.Addresses[0].Fix == nil || after.Addresses[0].GeoStatus != lead.GeoApproximate {
		t.Fatalf("an address whose text did not change keeps its position and takes the primary: %+v", after.Addresses[0])
	}
	if after.Addresses[1].Primary || after.Addresses[1].GeoStatus != lead.GeoPending {
		t.Fatalf("the edited address goes back to the queue: %+v", after.Addresses[1])
	}
	var attempts []int
	db.Raw("SELECT geo_attempts FROM lead_addresses WHERE lead_id = ? ORDER BY position", maria.ID).Scan(&attempts)
	if len(attempts) != 2 || attempts[0] != 2 || attempts[1] != 0 {
		t.Fatalf("attempts = %v, want the untouched address kept at 2 and the edited one requeued at 0", attempts)
	}
	var keys struct{ CityKey, DistrictKey string }
	db.Raw("SELECT city_key, district_key FROM lead_addresses WHERE id = ?", home.ID).Scan(&keys)
	if keys.CityKey != "sp:sao paulo" || keys.DistrictKey != "bela vista" {
		t.Fatalf("keys = %+v", keys)
	}

	cleared := reload(t, repo, ws, maria.ID)
	none, noAddresses := []lead.ContactPhone{}, []lead.AddressInput{}
	if err := cleared.ApplyEdit(lead.Edit{Phones: &none, Addresses: &noAddresses}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, cleared, after.Version, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	var left int64
	db.Raw("SELECT (SELECT COUNT(*) FROM lead_phones WHERE lead_id = ?) + (SELECT COUNT(*) FROM lead_addresses WHERE lead_id = ?)", maria.ID, maria.ID).Scan(&left)
	if left != 0 {
		t.Fatalf("rows left = %d", left)
	}
}

func link(t *testing.T, repo *repository, ws string, subject, other *lead.Lead, kind lead.RelationKind, actorID string) lead.RelationWrite {
	t.Helper()
	r, err := lead.RelationBetween(subject, other, kind, actorID)
	if err != nil {
		t.Fatal(err)
	}
	written, err := repo.AddRelation(context.Background(), ws, r)
	if err != nil {
		t.Fatalf("link %s to %s: %v", subject.Name, other.Name, err)
	}
	return written
}

func TestRelationsCountOnBothSidesInTheSameWriteAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	actorID := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Name: "Maria"})
	lider := newLead(t, repo, ws, lead.Draft{Name: "Líder"})

	relative, err := lead.New(ws, lead.Draft{Name: "Pedro"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	relative.ID = uuid.NewString()
	anchor := reload(t, repo, ws, maria.ID)
	r, err := relative.Relate(anchor, lead.KindParent, actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, relative, []recordevent.Event{lead.RelationEvent(lead.EventRelationAdded, actorID, relative.ID, r)}); err != nil {
		t.Fatalf("insert relative: %v", err)
	}
	if got := reload(t, repo, ws, maria.ID); got.RelativesCount != 1 || got.Version != 2 {
		t.Fatalf("maria = %+v", got)
	}
	if got := reload(t, repo, ws, relative.ID); got.RelativesCount != 1 {
		t.Fatalf("pedro = %+v", got)
	}
	var createdBy string
	db.Raw("SELECT created_by::text FROM lead_relations WHERE id = ?", relative.Relations[0].ID).Scan(&createdBy)
	if createdBy != actorID {
		t.Fatalf("the relation is stamped with who linked it, got %q", createdBy)
	}

	written := link(t, repo, ws, maria, lider, lead.KindReferredBy, actorID)
	if tally := written.Leads[lider.ID]; tally.Counts.Referred != 1 || tally.Version != 2 {
		t.Fatalf("the referrer counts the referral and moves to a new version: %+v", written.Leads)
	}
	if tally := written.Leads[maria.ID]; tally.Version != 3 || tally.Counts.Relatives != 1 {
		t.Fatalf("maria moves to a new version too: %+v", written.Leads)
	}
	for id, want := range map[string]int64{lider.ID: 1, maria.ID: 2} {
		var events int64
		db.Raw("SELECT COUNT(*) FROM lead_events WHERE lead_id = ? AND kind = 'relation_added' AND actor_id = ?", id, actorID).Scan(&events)
		if events != want {
			t.Fatalf("lead %s records %d relation events, want %d (both sides of every link)", id, events, want)
		}
	}

	removed, err := repo.RemoveRelation(ctx, ws, written.Relation.ID, actorID)
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if got := reload(t, repo, ws, lider.ID); got.ReferredCount != 0 || removed.Leads[lider.ID].Version != 3 {
		t.Fatalf("leader = %+v, tallies %+v", got, removed.Leads)
	}
	if got := reload(t, repo, ws, maria.ID); got.Version != 4 || got.RelativesCount != 1 {
		t.Fatalf("maria moves to a new version when her referral goes away: %+v", got)
	}
	if _, err := repo.RemoveRelation(ctx, ws, written.Relation.ID, actorID); !errors.Is(err, lead.ErrRelationNotFound) {
		t.Fatalf("removing twice = %v", err)
	}
}

func TestARelationIsRemovableWhenItsHolderWasDeletedAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ws := uuid.NewString()
	mae := newLead(t, repo, ws, lead.Draft{Name: "Mãe"})
	filho := newLead(t, repo, ws, lead.Draft{Name: "Filho"})
	written := link(t, repo, ws, filho, mae, lead.KindParent, uuid.NewString())
	if written.Relation.LeadID != mae.ID {
		t.Fatalf("the mother holds the relation, got %+v", written.Relation)
	}
	if err := db.Exec("UPDATE leads SET deleted_at = now(), version = version + 1 WHERE id = ?", mae.ID).Error; err != nil {
		t.Fatal(err)
	}
	removed, err := repo.RemoveRelation(context.Background(), ws, written.Relation.ID, uuid.NewString())
	if err != nil {
		t.Fatalf("a relation whose holder is gone must still be removable: %v", err)
	}
	if _, announced := removed.Leads[mae.ID]; announced || len(removed.Leads) != 1 {
		t.Fatalf("only the live side moves, got %+v", removed.Leads)
	}
	if got := reload(t, repo, ws, filho.ID); got.RelativesCount != 0 {
		t.Fatalf("filho = %+v", got)
	}
}

func TestAStaleSaveNeverHidesARelationAddedFromTheOtherSideAgainstPostgres(t *testing.T) {
	_, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	ana := newLead(t, repo, ws, lead.Draft{Name: "Ana"})
	bia := newLead(t, repo, ws, lead.Draft{Name: "Bia"})

	staleBia := reload(t, repo, ws, bia.ID)
	link(t, repo, ws, ana, bia, lead.KindSibling, uuid.NewString())

	staleBia.Nickname = "Bibi"
	if err := repo.Save(ctx, staleBia, 1, nil); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("a save from before the link = %v, want a conflict", err)
	}
	if got := reload(t, repo, ws, bia.ID); got.RelativesCount != 1 {
		t.Fatalf("bia = %+v", got)
	}
	fresh := reload(t, repo, ws, bia.ID)
	fresh.Nickname = "Bibi"
	if err := repo.Save(ctx, fresh, fresh.Version, nil); err != nil {
		t.Fatalf("a fresh save: %v", err)
	}
	if got := reload(t, repo, ws, bia.ID); got.RelativesCount != 1 {
		t.Fatalf("a record save never touches the relation counts, got %+v", got)
	}
}

func TestConcurrentLinksBetweenTwoLeadsNeverDeadlockAgainstPostgres(t *testing.T) {
	_, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	ana := newLead(t, repo, ws, lead.Draft{Name: "Ana"})
	bia := newLead(t, repo, ws, lead.Draft{Name: "Bia"})

	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		results := make([]lead.RelationWrite, 2)
		errs := make([]error, 2)
		for i, pair := range [][2]*lead.Lead{{ana, bia}, {bia, ana}} {
			wg.Add(1)
			go func(i int, self, other *lead.Lead) {
				defer wg.Done()
				r, err := lead.RelationBetween(self, other, lead.KindCousin, uuid.NewString())
				if err != nil {
					errs[i] = err
					return
				}
				results[i], errs[i] = repo.AddRelation(ctx, ws, r)
			}(i, pair[0], pair[1])
		}
		wg.Wait()
		var kept string
		for i, err := range errs {
			switch {
			case err == nil:
				if kept != "" {
					t.Fatalf("round %d: both links were stored", round)
				}
				kept = results[i].Relation.ID
			case errors.Is(err, lead.ErrRelationExists):
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if kept == "" {
			t.Fatalf("round %d: no link was stored", round)
		}
		if _, err := repo.RemoveRelation(ctx, ws, kept, uuid.NewString()); err != nil {
			t.Fatalf("round %d reset: %v", round, err)
		}
	}
}

func TestTheRelativesPageWalksTheFamilyByKeysetAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	lider := newLead(t, repo, ws, lead.Draft{Name: "Líder"})
	var referrals []string
	for i := 0; i < 5; i++ {
		person := newLead(t, repo, ws, lead.Draft{Name: "Indicado " + string(rune('A'+i)), Number: "55119000000" + string(rune('0'+i)) + "0"})
		link(t, repo, ws, lider, person, lead.KindReferred, uuid.NewString())
		referrals = append(referrals, person.ID)
	}
	irma := newLead(t, repo, ws, lead.Draft{Name: "Irmã"})
	link(t, repo, ws, irma, lider, lead.KindSibling, uuid.NewString())
	if err := db.Exec("UPDATE leads SET deleted_at = now() WHERE id = ?", referrals[4]).Error; err != nil {
		t.Fatal(err)
	}

	var seen []string
	after := ""
	for pages := 0; ; pages++ {
		page, err := repo.ListRelatives(ctx, ws, lead.RelativesQuery{LeadID: lider.ID, Dimension: lead.DimensionReferral, After: after, Limit: 2})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range page.Relatives {
			if r.KindFrom(lider.ID) != lead.KindReferred || r.Lead == nil || r.Lead.Name == "" {
				t.Fatalf("relative = %+v", r)
			}
			seen = append(seen, r.Lead.ID)
		}
		if page.Next == "" {
			break
		}
		if pages > 3 {
			t.Fatal("the pages never end")
		}
		after = page.Next
	}
	if len(seen) != 4 || seen[0] != referrals[0] || seen[3] != referrals[3] {
		t.Fatalf("referrals = %v, want the four live ones in the order they were linked", seen)
	}

	family, err := repo.ListRelatives(ctx, ws, lead.RelativesQuery{LeadID: lider.ID, Dimension: lead.DimensionFamily})
	if err != nil || len(family.Relatives) != 1 || family.Relatives[0].Lead.ID != irma.ID || family.Relatives[0].KindFrom(lider.ID) != lead.KindSibling || family.Next != "" {
		t.Fatalf("family = %+v, %v", family, err)
	}
	all, err := repo.ListRelatives(ctx, ws, lead.RelativesQuery{LeadID: lider.ID})
	if err != nil || len(all.Relatives) != 5 {
		t.Fatalf("every dimension = %+v, %v", all, err)
	}
}

func TestNumberHoldersAreASemiJoinWithTheIdentityFirstAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	for i := 0; i < duplicateLookupLimit+5; i++ {
		newLead(t, repo, ws, lead.Draft{Name: "Escola " + uuid.NewString()[:8], Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneWork}}})
	}
	owner := newLead(t, repo, ws, lead.Draft{Name: "Recepção", Number: "551133334444"})

	found, err := repo.FindByNumbersOrAddresses(ctx, ws, []string{"551133334444"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != duplicateLookupLimit || found[0].ID != owner.ID {
		t.Fatalf("found %d leads, first %s; the identity holder must survive the cap", len(found), found[0].ID)
	}
	candidate, err := lead.New(ws, lead.Draft{Number: "551133334444"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if holder := lead.IdentityHolder(candidate, found); holder == nil || holder.ID != owner.ID {
		t.Fatalf("identity holder = %+v", holder)
	}

	formats := numberFormats([]string{"551133334444"})
	var plan []string
	if err := db.Raw("EXPLAIN (FORMAT JSON) "+holdingNumbersSQL, ws, ws, formats, ws, formats, formats, duplicateLookupLimit).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || strings.Contains(plan[0], "SubPlan") {
		t.Fatalf("the lookup must be a semi join, not a filter with a subplan per lead: %v", plan)
	}
}

func TestTheDatabaseRefusesRelationsTheDomainRefusesAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ws, otherWS := uuid.NewString(), uuid.NewString()
	ana := newLead(t, repo, ws, lead.Draft{Name: "Ana"})
	bia := newLead(t, repo, ws, lead.Draft{Name: "Bia"})
	stranger := newLead(t, repo, otherWS, lead.Draft{Name: "Stranger"})
	insert := func(workspaceID, from, to, dimension string) error {
		return db.Exec("INSERT INTO lead_relations (id, workspace_id, lead_id, other_lead_id, dimension, kind, created_at) VALUES (?, ?, ?, ?, ?, 'sibling', now())",
			uuid.NewString(), workspaceID, from, to, dimension).Error
	}
	if err := insert(ws, ana.ID, bia.ID, "family"); err != nil {
		t.Fatal(err)
	}
	if err := insert(ws, bia.ID, ana.ID, "family"); !database.IsUniqueViolation(err) {
		t.Fatalf("the same pair twice in one dimension = %v", err)
	}
	if err := insert(ws, bia.ID, ana.ID, "referral"); err != nil {
		t.Fatalf("the same pair in another dimension: %v", err)
	}
	if err := insert(ws, ana.ID, ana.ID, "family"); err == nil || !strings.Contains(err.Error(), "chk_lead_relations_not_self") {
		t.Fatalf("a self relation = %v", err)
	}
	if err := insert(ws, ana.ID, stranger.ID, "family"); err == nil || !strings.Contains(err.Error(), "fk_lead_relations_workspace_other_lead") {
		t.Fatalf("a relative of another workspace = %v", err)
	}
	if err := db.Exec("INSERT INTO lead_phones (id, workspace_id, lead_id, number, label, position, created_at) VALUES (?, ?, ?, '551133334444', 'landline', 0, now())",
		uuid.NewString(), otherWS, ana.ID).Error; err == nil || !strings.Contains(err.Error(), "fk_lead_phones_workspace_lead") {
		t.Fatalf("a phone filed under another workspace = %v", err)
	}
	two := func(id string) error {
		return db.Exec("INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, geo_status, fingerprint, created_at, updated_at) VALUES (?, ?, ?, 'home', true, 'pending', 'x', now(), now())",
			id, ws, bia.ID).Error
	}
	if err := two(uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := two(uuid.NewString()); !database.IsUniqueViolation(err) {
		t.Fatalf("a second primary address = %v", err)
	}
}

func TestAContactPhoneBecomesTheIdentityOfTheOnlyLeadWithoutOneAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	ws := uuid.NewString()
	rosa := newLead(t, repo, ws, lead.Draft{Name: "Dona Rosa", Phones: []lead.ContactPhone{{Number: "551187654321", Label: lead.PhoneMobile}, {Number: "551133334444", Label: lead.PhoneLandline}}})

	got, created, err := repo.FindOrCreate(ws, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, Name: "Rosa"})
	if err != nil || created || got.ID != rosa.ID {
		t.Fatalf("FindOrCreate = %+v, created %v, %v", got, created, err)
	}
	stored := reload(t, repo, ws, rosa.ID)
	if stored.Number != "5511987654321" || len(stored.Phones) != 1 || stored.Phones[0].Number != "551133334444" || stored.Name != "Dona Rosa" || stored.Version != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	var promoted int64
	db.Raw("SELECT COUNT(*) FROM lead_events WHERE lead_id = ? AND kind = 'identity_promoted' AND actor_kind = 'system'", rosa.ID).Scan(&promoted)
	if promoted != 1 {
		t.Fatalf("promotion events = %d", promoted)
	}

	newLead(t, repo, ws, lead.Draft{Name: "Ana", Phones: []lead.ContactPhone{{Number: "5521900001111", Label: lead.PhoneMobile}}})
	newLead(t, repo, ws, lead.Draft{Name: "Bia", Phones: []lead.ContactPhone{{Number: "5521900001111", Label: lead.PhoneMobile}}})
	common, created, err := repo.FindOrCreate(ws, "5521900001111", lead.LeadUpdate{Source: lead.SourceChannel})
	if err != nil || !created || common.Number != "5521900001111" {
		t.Fatalf("a number two leads share is a new lead, got %+v created %v, %v", common, created, err)
	}
}

func TestDuplicateCandidatesAreFoundByPhoneOrAddressAgainstPostgres(t *testing.T) {
	_, repo := collectionsDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	byPhone := newLead(t, repo, ws, lead.Draft{Name: "João", Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}})
	byAddress := newLead(t, repo, ws, lead.Draft{Name: "Maria Souza", Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: paulista}}})
	newLead(t, repo, uuid.NewString(), lead.Draft{Name: "Elsewhere", Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}})

	candidate, err := lead.New(ws, lead.Draft{Name: "maria souza", Phones: []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}},
		Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: paulista}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	reader := lead.Viewer{ReadsLeads: true, ReadsAddresses: true}
	numbers, fingerprints := candidate.DuplicateLookup(reader)
	found, err := repo.FindByNumbersOrAddresses(ctx, ws, numbers, fingerprints)
	if err != nil {
		t.Fatal(err)
	}
	got := lead.DuplicateCandidates(reader, candidate, found)
	if len(got) != 2 || got[0].LeadID != byPhone.ID || got[1].LeadID != byAddress.ID {
		t.Fatalf("candidates = %+v", got)
	}
}
