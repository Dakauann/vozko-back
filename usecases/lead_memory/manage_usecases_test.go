package lead_memory_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/agent"
	ce "vozko/domain/conversation_event"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/user"
)

var ctx = context.Background()

func aiActor() leadmemory.WriteActor {
	return leadmemory.WriteActor{Kind: actor.KindAI, ID: actor.FormatAI("agent-1")}
}

func humanActor() leadmemory.WriteActor {
	return leadmemory.WriteActor{Kind: actor.KindHuman, ID: "user-1"}
}

func strPtr(s string) *string { return &s }

func createInput(content string) leadmemory.CreateInput {
	return leadmemory.CreateInput{
		WorkspaceID:     "ws-1",
		LeadID:          "lead-1",
		Content:         content,
		Category:        leadmemory.CategoryPreference,
		Actor:           aiActor(),
		SourceEntryID:   strPtr("entry-1"),
		SourceEntryType: strPtr("whatsapp"),
	}
}

func TestCreateRemembersAndEmits(t *testing.T) {
	repo, timeline := newFakeRepo(), &fakeTimeline{}
	uc, err := NewCreateUseCase(repo, timeline)
	if err != nil {
		t.Fatal(err)
	}

	got, err := uc.Execute(ctx, createInput("Prefere boleto a PIX."))
	if err != nil || got.Deduplicated {
		t.Fatalf("create = (%+v, %v)", got, err)
	}
	if got.Memory.ActorID != "ai:agent-1" || got.Memory.ActorKind != actor.KindAI {
		t.Fatalf("attribution = %s/%s", got.Memory.ActorKind, got.Memory.ActorID)
	}
	if len(timeline.events) != 1 || timeline.events[0].EventType != ce.EventLeadMemoryCreated {
		t.Fatalf("timeline = %+v", timeline.events)
	}
	if timeline.events[0].ActorID != "ai:agent-1" {
		t.Fatalf("event actor = %s", timeline.events[0].ActorID)
	}
}

func TestCreateDeduplicatesEquivalentContent(t *testing.T) {
	repo, timeline := newFakeRepo(), &fakeTimeline{}
	uc, _ := NewCreateUseCase(repo, timeline)

	first, _ := uc.Execute(ctx, createInput("Prefere boleto a PIX."))
	// Same fact, different casing and spacing: must come back deduplicated,
	// with no second row and no second event.
	second, err := uc.Execute(ctx, createInput("  prefere  BOLETO a pix. "))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Deduplicated || second.Memory.ID != first.Memory.ID {
		t.Fatalf("dedup = %+v", second)
	}
	if len(timeline.events) != 1 {
		t.Fatalf("dedup must not emit an event, got %d", len(timeline.events))
	}
}

func TestCreateStopsAtTheCap(t *testing.T) {
	repo := newFakeRepo()
	uc, _ := NewCreateUseCase(repo, nil)

	for i := 0; i < leadmemory.MaxActiveMemoriesPerLead; i++ {
		in := createInput("fato número " + strings.Repeat("x", i+1))
		if _, err := uc.Execute(ctx, in); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	_, err := uc.Execute(ctx, createInput("um fato a mais"))
	if !errors.Is(err, leadmemory.ErrLimitReached) {
		t.Fatalf("over cap = %v, want ErrLimitReached", err)
	}
}

func TestCreateResolvesDuplicateRace(t *testing.T) {
	repo := newFakeRepo()
	uc, _ := NewCreateUseCase(repo, nil)

	// The probe misses but the insert collides (two writers racing): the use
	// case must re-read and answer with the winner instead of erroring.
	first, _ := uc.Execute(ctx, createInput("Prefere boleto."))
	raced, err := uc.Execute(ctx, leadmemory.CreateInput{
		WorkspaceID: "ws-1", LeadID: "lead-1",
		Content:  "PREFERE   BOLETO.", // same norm; fake's Create rejects like the unique index
		Category: leadmemory.CategoryOther,
		Actor:    humanActor(),
	})
	if err != nil || !raced.Deduplicated || raced.Memory.ID != first.Memory.ID {
		t.Fatalf("raced create = (%+v, %v)", raced, err)
	}
}

func TestUpdateByPrefixLastWriterWins(t *testing.T) {
	repo, timeline := newFakeRepo(), &fakeTimeline{}
	create, _ := NewCreateUseCase(repo, nil)
	update, _ := NewUpdateUseCase(repo, timeline)

	created, _ := create.Execute(ctx, createInput("Orçamento de R$ 1.000."))

	got, err := update.Execute(ctx, leadmemory.UpdateInput{
		WorkspaceID: "ws-1",
		LeadID:      "lead-1",
		MemoryRef:   created.Memory.ID[:leadmemory.MinIDPrefixLen],
		Content:     "Orçamento aprovado de R$ 2.000.",
		Category:    leadmemory.CategoryDeal,
		Actor:       humanActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "Orçamento aprovado de R$ 2.000." || got.Category != leadmemory.CategoryDeal {
		t.Fatalf("update result = %+v", got)
	}
	// The correction belongs to the operator now; authorship history lives on
	// the timeline, not the row.
	if got.ActorKind != actor.KindHuman || got.ActorID != "user-1" {
		t.Fatalf("last-writer attribution = %s/%s", got.ActorKind, got.ActorID)
	}
}

func TestUpdateWithNothingToChangeIsANoOp(t *testing.T) {
	repo, timeline := newFakeRepo(), &fakeTimeline{}
	create, _ := NewCreateUseCase(repo, nil)
	update, _ := NewUpdateUseCase(repo, timeline)

	created, _ := create.Execute(ctx, createInput("Prefere boleto."))
	got, err := update.Execute(ctx, leadmemory.UpdateInput{
		WorkspaceID: "ws-1", LeadID: "lead-1", MemoryRef: created.Memory.ID, Actor: humanActor(),
	})
	if err != nil || got.ID != created.Memory.ID {
		t.Fatalf("noop update = (%+v, %v)", got, err)
	}
	if len(timeline.events) != 0 {
		t.Fatal("noop update must not emit an event")
	}
}

func TestResolveRefGuards(t *testing.T) {
	repo := newFakeRepo()
	create, _ := NewCreateUseCase(repo, nil)
	update, _ := NewUpdateUseCase(repo, nil)
	created, _ := create.Execute(ctx, createInput("Prefere boleto."))

	cases := []struct {
		name    string
		in      leadmemory.UpdateInput
		wantErr error
	}{
		{
			// The authorization boundary: another workspace's id behaves like a
			// missing row, never like a permission error.
			name:    "foreign workspace is not found",
			in:      leadmemory.UpdateInput{WorkspaceID: "ws-2", MemoryRef: created.Memory.ID, Content: "x", Actor: humanActor()},
			wantErr: leadmemory.ErrNotFound,
		},
		{
			// The tool always pins a lead; a full id of another lead's memory
			// must not resolve through it.
			name:    "full id of another lead is not found",
			in:      leadmemory.UpdateInput{WorkspaceID: "ws-1", LeadID: "lead-2", MemoryRef: created.Memory.ID, Content: "x", Actor: humanActor()},
			wantErr: leadmemory.ErrNotFound,
		},
		{
			name:    "short prefix is not found",
			in:      leadmemory.UpdateInput{WorkspaceID: "ws-1", LeadID: "lead-1", MemoryRef: created.Memory.ID[:4], Content: "x", Actor: humanActor()},
			wantErr: leadmemory.ErrNotFound,
		},
		{
			// A prefix without a lead scope has no meaning: prefixes exist only
			// inside one lead's prompt block.
			name:    "prefix without lead is not found",
			in:      leadmemory.UpdateInput{WorkspaceID: "ws-1", MemoryRef: created.Memory.ID[:leadmemory.MinIDPrefixLen], Content: "x", Actor: humanActor()},
			wantErr: leadmemory.ErrNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := update.Execute(ctx, tc.in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDeleteForgetsAndEmits(t *testing.T) {
	repo, timeline := newFakeRepo(), &fakeTimeline{}
	create, _ := NewCreateUseCase(repo, nil)
	del, _ := NewDeleteUseCase(repo, timeline)
	list, _ := NewListUseCase(repo, nil, nil)

	created, _ := create.Execute(ctx, createInput("Prefere boleto."))
	err := del.Execute(ctx, leadmemory.DeleteInput{
		WorkspaceID: "ws-1", LeadID: "lead-1", MemoryRef: created.Memory.ID,
		Actor:           humanActor(),
		SourceEntryID:   strPtr("entry-1"),
		SourceEntryType: strPtr("whatsapp"),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := list.Execute(ctx, leadmemory.ListInput{WorkspaceID: "ws-1", LeadID: "lead-1"})
	if out.Total != 0 {
		t.Fatalf("deleted memory still listed: %+v", out.Items)
	}
	if len(timeline.events) != 1 || timeline.events[0].EventType != ce.EventLeadMemoryDeleted {
		t.Fatalf("timeline = %+v", timeline.events)
	}
}

func TestMutationsWithoutEntryStayOffTheTimeline(t *testing.T) {
	repo, timeline := newFakeRepo(), &fakeTimeline{}
	uc, _ := NewCreateUseCase(repo, timeline)

	in := createInput("Prefere boleto.")
	in.SourceEntryID, in.SourceEntryType = nil, nil // lead-page edit: no conversation
	if _, err := uc.Execute(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(timeline.events) != 0 {
		t.Fatalf("entry-less mutation must not emit, got %+v", timeline.events)
	}
}

func TestListResolvesActorLabelsBestEffort(t *testing.T) {
	repo := newFakeRepo()
	create, _ := NewCreateUseCase(repo, nil)

	aiIn := createInput("Prefere boleto.")
	if _, err := create.Execute(ctx, aiIn); err != nil {
		t.Fatal(err)
	}
	humanIn := createInput("Esposa se chama Ana.")
	humanIn.Actor = humanActor()
	if _, err := create.Execute(ctx, humanIn); err != nil {
		t.Fatal(err)
	}

	t.Run("labels resolve", func(t *testing.T) {
		list, _ := NewListUseCase(repo,
			&fakeAgentFinder{agents: []*agent.Agent{{ID: "agent-1", Name: "Agente Vendas"}}},
			&fakeUserFinder{users: []*user.User{{ID: "user-1", Username: "maria"}}},
		)
		out, err := list.Execute(ctx, leadmemory.ListInput{WorkspaceID: "ws-1", LeadID: "lead-1"})
		if err != nil || len(out.Items) != 2 {
			t.Fatalf("list = (%+v, %v)", out, err)
		}
		byActor := map[string]string{}
		for _, v := range out.Items {
			byActor[v.ActorID] = v.ActorLabel
		}
		if byActor["ai:agent-1"] != "Agente Vendas" || byActor["user-1"] != "maria" {
			t.Fatalf("labels = %+v", byActor)
		}
	})

	t.Run("finder failure degrades to empty labels", func(t *testing.T) {
		list, _ := NewListUseCase(repo,
			&fakeAgentFinder{err: errors.New("boom")},
			&fakeUserFinder{err: errors.New("boom")},
		)
		out, err := list.Execute(ctx, leadmemory.ListInput{WorkspaceID: "ws-1", LeadID: "lead-1"})
		if err != nil || len(out.Items) != 2 {
			t.Fatalf("list with failing finders = (%+v, %v)", out, err)
		}
		for _, v := range out.Items {
			if v.ActorLabel != "" {
				t.Fatalf("label should be empty on failure, got %q", v.ActorLabel)
			}
		}
	})
}
