package opportunity_repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func integrationDB(t *testing.T) *gorm.DB {
	return repotest.IsolatedDB(t, "opp_test", &schema.Opportunity{}, &schema.OpportunityConversation{}, &schema.OpportunityEvent{})
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

func TestDealsForEntryListsThisFunnelsDealsOpenFirst(t *testing.T) {
	repo := NewRepository(integrationDB(t))
	ws, pipeline, entry := uuid.New().String(), uuid.New().String(), uuid.New().String()
	create := func(d *opportunity.Opportunity) {
		t.Helper()
		if err := repo.Create(d, []opportunity.ConversationLink{{OpportunityID: d.ID, EntryID: entry, EntryType: "whatsapp"}}, nil); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	deals, err := repo.DealsForEntry(ws, pipeline, entry, "whatsapp")
	if err != nil || len(deals) != 0 {
		t.Fatalf("DealsForEntry() with no deal = %v, %v", deals, err)
	}

	won := newDeal(ws, pipeline, uuid.New().String())
	won.Status = opportunity.StatusWon
	create(won)
	time.Sleep(5 * time.Millisecond)
	first := newDeal(ws, pipeline, uuid.New().String())
	create(first)
	time.Sleep(5 * time.Millisecond)
	second := newDeal(ws, pipeline, uuid.New().String())
	create(second)
	create(newDeal(ws, uuid.New().String(), uuid.New().String()))

	deals, err = repo.DealsForEntry(ws, pipeline, entry, "whatsapp")
	if err != nil || len(deals) != 3 {
		t.Fatalf("DealsForEntry() = %v, %v, want the three deals of this funnel", deals, err)
	}
	if deals[0].ID != second.ID || deals[1].ID != first.ID || deals[2].ID != won.ID {
		t.Fatalf("order = %s %s %s, want open deals newest first, then closed", deals[0].ID, deals[1].ID, deals[2].ID)
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
				deals, err := store.DealsForEntry(ws, pipeline, entry, "whatsapp")
				if err != nil {
					return err
				}
				if len(deals.Open()) > 0 {
					return nil
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

func TestTwoSavesFromTheSameReadCannotBothWin(t *testing.T) {
	repo := NewRepository(integrationDB(t))
	ws, pipeline := uuid.New().String(), uuid.New().String()
	deal := newDeal(ws, pipeline, uuid.New().String())
	if err := repo.Create(deal, nil, nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if deal.Version != 1 {
		t.Fatalf("a new deal starts at version 1, got %d", deal.Version)
	}

	first, _ := repo.GetByID(ws, deal.ID)
	second, _ := repo.GetByID(ws, deal.ID)
	first.ValueCents = 1500
	if err := repo.Update(first, nil); err != nil {
		t.Fatalf("first save error = %v", err)
	}
	second.ValueCents = 9900
	if err := repo.Update(second, nil); !errors.Is(err, opportunity.ErrStaleDeal) {
		t.Fatalf("second save from the same read error = %v, want ErrStaleDeal", err)
	}

	stored, _ := repo.GetByID(ws, deal.ID)
	if stored.ValueCents != 1500 || stored.Version != 2 {
		t.Fatalf("stored = value %d version %d, want 1500 and 2", stored.ValueCents, stored.Version)
	}

	missing := newDeal(ws, pipeline, uuid.New().String())
	if err := repo.Update(missing, nil); !errors.Is(err, opportunity.ErrNotFound) {
		t.Fatalf("a missing deal must stay not found, got %v", err)
	}
}
