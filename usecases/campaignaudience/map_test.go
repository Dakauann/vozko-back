package campaignaudience_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/campaignaudience"
	"vozko/domain/geo"
	"vozko/domain/leadmap"
	"vozko/domain/selection"
)

type fakePlaces struct {
	mu     sync.Mutex
	scopes []selection.Scope
	asked  []selection.Selection
	limits []int
	sample leadmap.Sample
	pause  chan struct{}
}

func (f *fakePlaces) Sample(_ context.Context, scope selection.Scope, s selection.Selection, limit int) (leadmap.Sample, error) {
	if f.pause != nil {
		<-f.pause
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes = append(f.scopes, scope)
	f.asked = append(f.asked, s)
	f.limits = append(f.limits, limit)
	return f.sample, nil
}

func TestTheMapSamplesTheCountedSelectionForTheCaller(t *testing.T) {
	w := newWorld(t)
	w.places.sample = leadmap.Sample{
		Points:    []leadmap.Point{leadmap.NewPoint(geo.Point{Lat: -23.56, Lng: -46.65}, geo.PrecisionStreet, "on_map", 2, []string{"lead-1", "lead-2"}, "")},
		Located:   2,
		Positions: 1,
		Bounds:    &geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6},
	}
	spec := leadsSpec()
	sel := *spec.Selection
	sel.ExcludeIDs = []string{"lead-9"}
	spec.Selection = &sel
	spec.Variables = []campaignaudience.Variable{{Key: "1", Source: "lead.first_name"}}

	got, err := w.service.Map(ctx, testActor, campaignaudience.ChannelWhatsApp, spec)
	if err != nil {
		t.Fatal(err)
	}
	if got.Located != 2 || got.Positions != 1 || len(got.Points) != 1 || got.Points[0].People != 2 || got.Bounds == nil || *got.Bounds != *w.places.sample.Bounds {
		t.Fatalf("map = %+v, want the sample of the selection", got)
	}
	if len(w.places.asked) != 1 || w.places.limits[0] != leadmap.MaxSamplePoints {
		t.Fatalf("sampled %d times with limits %v", len(w.places.asked), w.places.limits)
	}
	if want := (selection.Scope{WorkspaceID: testActor.WorkspaceID, ActorID: testActor.UserID}); w.places.scopes[0] != want {
		t.Fatalf("scope = %+v, want the caller %+v", w.places.scopes[0], want)
	}
	if asked := w.places.asked[0]; asked.Mode != sel.Mode || len(asked.ExcludeIDs) != 1 || asked.ExcludeIDs[0] != "lead-9" {
		t.Fatalf("sampled %+v, want the counted selection %+v", asked, sel)
	}
}

func TestIdenticalMapsAtOnceSampleOnceAndAnotherViewerSamplesOnItsOwn(t *testing.T) {
	w := newWorld(t)
	w.places.pause = make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.service.Map(ctx, testActor, campaignaudience.ChannelWhatsApp, leadsSpec())
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(w.places.pause)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("map %d: %v", i, err)
		}
	}
	if len(w.places.asked) != 1 || w.gate.acquired != 1 {
		t.Fatalf("sampled %d times through %d gate slots, want one read", len(w.places.asked), w.gate.acquired)
	}
	other := testActor
	other.UserID = "user-2"
	other.IsAdmin = true
	if _, err := w.service.Map(ctx, other, campaignaudience.ChannelWhatsApp, leadsSpec()); err != nil {
		t.Fatal(err)
	}
	if len(w.places.asked) != 2 || w.places.scopes[1].ActorID != "user-2" || !w.places.scopes[1].IsAdmin {
		t.Fatalf("another viewer reused a sample: %+v", w.places.scopes)
	}
}

func TestTheMapRefusesAListAnUnknownChannelAndAMissingReader(t *testing.T) {
	w := newWorld(t)
	list := campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: "list-1"}
	if _, err := w.service.Map(ctx, testActor, campaignaudience.ChannelWhatsApp, list); !errors.Is(err, campaignaudience.ErrSourceUnsupported) {
		t.Fatalf("a list was mapped: %v", err)
	}
	if _, err := w.service.Map(ctx, testActor, campaignaudience.Channel("fax"), leadsSpec()); !errors.Is(err, campaignaudience.ErrChannelInvalid) {
		t.Fatalf("an unknown channel was mapped: %v", err)
	}
	if _, err := w.service.Map(ctx, testActor, campaignaudience.ChannelWhatsApp, campaignaudience.Spec{Source: campaignaudience.SourceLeads}); !errors.Is(err, campaignaudience.ErrSelectionRequired) {
		t.Fatalf("a spec without a selection was mapped: %v", err)
	}
	if len(w.places.asked) != 0 {
		t.Fatal("a refused map must sample nothing")
	}
	w.service.deps.Places = nil
	if _, err := w.service.Map(ctx, testActor, campaignaudience.ChannelWhatsApp, leadsSpec()); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("a service without lead positions mapped: %v", err)
	}
}
