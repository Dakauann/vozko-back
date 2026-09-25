package opportunity_repository

import (
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

	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
)

func integrationDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
}

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(integrationDSN()), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "opp_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(integrationDSN()+" search_path="+schemaName), silent)
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
	if err := db.AutoMigrate(&schema.Opportunity{}, &schema.OpportunityConversation{}, &schema.OpportunityEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newDeal(ws, pipeline, owner string) *opportunity.Opportunity {
	return &opportunity.Opportunity{
		ID:          uuid.New().String(),
		WorkspaceID: ws,
		PipelineID:  pipeline,
		StageID:     uuid.New().String(),
		OwnerID:     owner,
		CreatedBy:   owner,
		Title:       "Plano Pro",
		ValueCents:  7900,
		Currency:    "BRL",
		Status:      opportunity.StatusOpen,
	}
}

func TestOwnersAndAuthorsOfEveryKindRoundTrip(t *testing.T) {
	repo := NewRepository(integrationDB(t))
	ws, pipeline, id := uuid.New().String(), uuid.New().String(), uuid.New().String()

	for _, owner := range []string{id, "ai:" + id, "workflow:" + id} {
		deal := newDeal(ws, pipeline, owner)
		deal.Status = opportunity.StatusWon
		deal.ClosedBy = "system"
		closed := time.Now().UTC()
		deal.CloseDate = &closed
		if err := repo.Create(deal, nil, nil); err != nil {
			t.Fatalf("Create(%s) error = %v", owner, err)
		}
		got, err := repo.GetByID(ws, deal.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if got.OwnerID != owner || got.CreatedBy != owner || got.ClosedBy != "system" {
			t.Fatalf("read back owner %q created by %q closed by %q, want %q, %q, system", got.OwnerID, got.CreatedBy, got.ClosedBy, owner, owner)
		}
	}
}

func TestALegacyDealWithoutAuthorsReadsBackAsItWas(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ws, owner := uuid.New().String(), uuid.New().String()
	deal := newDeal(ws, uuid.New().String(), owner)
	deal.CreatedBy = ""
	if err := repo.Create(deal, nil, nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := db.Exec("UPDATE opportunities SET owner_kind = DEFAULT, created_by_kind = '' WHERE id = ?", deal.ID).Error; err != nil {
		t.Fatalf("simulate legacy row: %v", err)
	}
	got, err := repo.GetByID(ws, deal.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.OwnerID != owner || got.CreatedBy != "" || got.ClosedBy != "" {
		t.Fatalf("legacy deal read back as owner %q created by %q closed by %q", got.OwnerID, got.CreatedBy, got.ClosedBy)
	}
}

func TestCreateCommitsTheDealItsLinkAndItsEventTogether(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	links := NewLinkRepository(db)
	ws, entry := uuid.New().String(), uuid.New().String()
	deal := newDeal(ws, uuid.New().String(), "ai:"+uuid.New().String())
	link := opportunity.ConversationLink{OpportunityID: deal.ID, EntryID: entry, EntryType: "whatsapp"}
	events := opportunity.Changes(nil, deal, deal.CreatedBy, time.Now().UTC())

	if err := repo.Create(deal, []opportunity.ConversationLink{link}, events); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	gotLinks, err := links.ListByEntry(ws, entry, "whatsapp")
	if err != nil || len(gotLinks) != 1 || gotLinks[0].OpportunityID != deal.ID {
		t.Fatalf("ListByEntry() = %v, %v, want the new deal", gotLinks, err)
	}
	gotEvents, err := repo.ListEvents(ws, deal.ID)
	if err != nil || len(gotEvents) != 1 {
		t.Fatalf("ListEvents() = %v, %v, want one event", gotEvents, err)
	}
	if gotEvents[0].Type != opportunity.EventCreated || gotEvents[0].ActorID != deal.CreatedBy || gotEvents[0].ValueCents != 7900 {
		t.Fatalf("created event read back as %+v", gotEvents[0])
	}
}

func TestAFailedCreateLeavesNothingBehind(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ws := uuid.New().String()
	deal := newDeal(ws, uuid.New().String(), uuid.New().String())
	broken := opportunity.ConversationLink{OpportunityID: deal.ID, EntryID: "not-a-uuid", EntryType: "whatsapp"}

	if err := repo.Create(deal, []opportunity.ConversationLink{broken}, nil); err == nil {
		t.Fatalf("Create() with a broken link succeeded")
	}
	if _, err := repo.GetByID(ws, deal.ID); !errors.Is(err, opportunity.ErrNotFound) {
		t.Fatalf("GetByID() after a failed create = %v, want ErrNotFound: the deal outlived its failed link", err)
	}
}

func TestUpdateCommitsItsEvents(t *testing.T) {
	repo := NewRepository(integrationDB(t))
	ws := uuid.New().String()
	deal := newDeal(ws, uuid.New().String(), uuid.New().String())
	if err := repo.Create(deal, nil, nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	before := *deal
	deal.ValueCents = 15000
	if err := repo.Update(deal, opportunity.Changes(&before, deal, deal.OwnerID, time.Now().UTC())); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	events, err := repo.ListEvents(ws, deal.ID)
	if err != nil || len(events) != 1 || events[0].Type != opportunity.EventValueChanged {
		t.Fatalf("ListEvents() = %v, %v, want one value change", events, err)
	}
	if events[0].Details["from_value_cents"] != float64(7900) {
		t.Fatalf("value change details = %v", events[0].Details)
	}
}

func TestOpenForEntryFindsOnlyTheOpenDealOfThatFunnel(t *testing.T) {
	repo := NewRepository(integrationDB(t))
	ws, pipeline, entry := uuid.New().String(), uuid.New().String(), uuid.New().String()
	link := func(d *opportunity.Opportunity) []opportunity.ConversationLink {
		return []opportunity.ConversationLink{{OpportunityID: d.ID, EntryID: entry, EntryType: "whatsapp"}}
	}

	won := newDeal(ws, pipeline, uuid.New().String())
	won.Status = opportunity.StatusWon
	otherFunnel := newDeal(ws, uuid.New().String(), uuid.New().String())
	for _, d := range []*opportunity.Opportunity{won, otherFunnel} {
		if err := repo.Create(d, link(d), nil); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if _, err := repo.OpenForEntry(ws, pipeline, entry, "whatsapp"); !errors.Is(err, opportunity.ErrNotFound) {
		t.Fatalf("OpenForEntry() with only a won deal and another funnel's deal = %v, want ErrNotFound", err)
	}

	open := newDeal(ws, pipeline, uuid.New().String())
	if err := repo.Create(open, link(open), nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := repo.OpenForEntry(ws, pipeline, entry, "whatsapp")
	if err != nil || got.ID != open.ID {
		t.Fatalf("OpenForEntry() = %v, %v, want the open deal", got, err)
	}
}

func TestConcurrentAutomationsCreateExactlyOneDeal(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ws, pipeline, entry := uuid.New().String(), uuid.New().String(), uuid.New().String()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.WithEntryLock(ws, entry, "whatsapp", func(store opportunity.Store) error {
				if _, err := store.OpenForEntry(ws, pipeline, entry, "whatsapp"); err == nil {
					return nil
				} else if !errors.Is(err, opportunity.ErrNotFound) {
					return err
				}
				deal := newDeal(ws, pipeline, "ai:"+uuid.New().String())
				return store.Create(deal, []opportunity.ConversationLink{{OpportunityID: deal.ID, EntryID: entry, EntryType: "whatsapp"}}, nil)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("WithEntryLock() error = %v", err)
		}
	}

	var count int64
	db.Model(&schema.OpportunityConversation{}).Where("entry_id = ?", entry).Count(&count)
	if count != 1 {
		t.Fatalf("%d deals were created for one conversation, want exactly 1", count)
	}
}

func TestCurrentForEntryPrefersTheOpenDealThenTheLatestClosedOne(t *testing.T) {
	repo := NewRepository(integrationDB(t))
	ws, pipeline, entry := uuid.New().String(), uuid.New().String(), uuid.New().String()
	create := func(d *opportunity.Opportunity) {
		t.Helper()
		if err := repo.Create(d, []opportunity.ConversationLink{{OpportunityID: d.ID, EntryID: entry, EntryType: "whatsapp"}}, nil); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	if _, err := repo.CurrentForEntry(ws, pipeline, entry, "whatsapp"); !errors.Is(err, opportunity.ErrNotFound) {
		t.Fatalf("CurrentForEntry() with no deal = %v, want ErrNotFound", err)
	}

	lost := newDeal(ws, pipeline, uuid.New().String())
	lost.Status, lost.LostReasonID = opportunity.StatusLost, "preço"
	create(lost)
	time.Sleep(5 * time.Millisecond)
	won := newDeal(ws, pipeline, uuid.New().String())
	won.Status = opportunity.StatusWon
	create(won)
	create(newDeal(ws, uuid.New().String(), uuid.New().String()))

	got, err := repo.CurrentForEntry(ws, pipeline, entry, "whatsapp")
	if err != nil || got.ID != won.ID {
		t.Fatalf("CurrentForEntry() = %v, %v, want the latest closed deal", got, err)
	}

	open := newDeal(ws, pipeline, uuid.New().String())
	create(open)
	got, err = repo.CurrentForEntry(ws, pipeline, entry, "whatsapp")
	if err != nil || got.ID != open.ID {
		t.Fatalf("CurrentForEntry() = %v, %v, want the open deal", got, err)
	}
}
