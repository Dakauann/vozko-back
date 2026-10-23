package lead

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/lead"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
)

func TestASelectionSampleGroupsTheSelectedPositionsAndCountsTheRestAgainstPostgres(t *testing.T) {
	db := mapDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	sameLat, sameLng := at(-23.5614, -46.6559)
	first := seedLead(t, db, ws, &seededPosition{lat: sameLat, lng: sameLng, precision: "exact"})
	second := seedLead(t, db, ws, &seededPosition{lat: sameLat, lng: sameLng, precision: "exact"})
	farLat, farLng := at(-23.60, -46.70)
	third := seedLead(t, db, ws, &seededPosition{lat: farLat, lng: farLng, precision: "district"})
	excluded := seedLead(t, db, ws, &seededPosition{lat: farLat, lng: farLng, precision: "exact"})
	seedLead(t, db, ws, nil)
	otherLat, otherLng := at(-23.70, -46.80)
	seedLead(t, db, other, &seededPosition{lat: otherLat, lng: otherLng, precision: "exact"})

	sample, err := repo.SelectionSample(ctx, lead.SelectionQuery{WorkspaceID: ws, ExcludeIDs: []string{excluded}}, leadmap.MaxSamplePoints)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Located != 3 || sample.Positions != 2 || len(sample.Points) != 2 || sample.Sampled() {
		t.Fatalf("sample = %+v, want 3 located leads on 2 positions", sample)
	}
	people := map[string]int{}
	for _, p := range sample.Points {
		people[p.ID()] += p.People
		if p.Placement == "" {
			t.Fatalf("a point lost its placement: %+v", p)
		}
	}
	if people["-23.5614,-46.6559"] != 2 {
		t.Fatalf("the shared position = %+v, want two people", people)
	}

	limited, err := repo.SelectionSample(ctx, lead.SelectionQuery{WorkspaceID: ws, ExcludeIDs: []string{excluded}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Points) != 1 || limited.Positions != 2 || limited.Located != 3 || !limited.Sampled() {
		t.Fatalf("limited sample = %+v, want one point of two positions", limited)
	}

	picked, err := repo.SelectionSample(ctx, lead.SelectionQuery{WorkspaceID: ws, IDs: []string{first, third}}, leadmap.MaxSamplePoints)
	if err != nil {
		t.Fatal(err)
	}
	if picked.Located != 2 || picked.Positions != 2 {
		t.Fatalf("picked sample = %+v, want the two picked leads", picked)
	}

	firstN, err := repo.SelectionSample(ctx, lead.SelectionQuery{WorkspaceID: ws, Order: []shared.Sort{lead.DefaultSort}, Limit: 1}, leadmap.MaxSamplePoints)
	if err != nil {
		t.Fatal(err)
	}
	if firstN.Located > 1 {
		t.Fatalf("first n sample = %+v, want at most the one chosen lead", firstN)
	}

	foreign, err := repo.SelectionSample(ctx, lead.SelectionQuery{WorkspaceID: other, IDs: []string{second}}, leadmap.MaxSamplePoints)
	if err != nil {
		t.Fatal(err)
	}
	if foreign.Located != 0 || len(foreign.Points) != 0 {
		t.Fatalf("another workspace saw this lead: %+v", foreign)
	}

	if _, err := repo.SelectionSample(ctx, lead.SelectionQuery{WorkspaceID: ws}, leadmap.MaxSamplePoints+1); !errors.Is(err, leadmap.ErrSampleLimit) {
		t.Fatalf("an unbounded sample was read: %v", err)
	}
}
