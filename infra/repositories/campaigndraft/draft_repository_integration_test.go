package campaigndraft_repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaigndraft"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func seededDraft(t *testing.T, repo campaigndraft.Repository, workspaceID string, ch campaigndraft.Channel, name string, at time.Time) campaigndraft.Draft {
	t.Helper()
	d, err := campaigndraft.New(uuid.NewString(), workspaceID, uuid.NewString(), ch, name, json.RawMessage(`{"request":{"name":"`+name+`"},"state":{"step":"config"}}`), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), &d); err != nil {
		t.Fatalf("create: %v", err)
	}
	return d
}

func TestIntegrationDraftRoundTripAndScope(t *testing.T) {
	db := repotest.IsolatedDB(t, "campaign_drafts", schema.CampaignDraftModels()...)
	repo := NewRepository(db)
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	older := seededDraft(t, repo, ws, campaigndraft.ChannelWhatsApp, "older", base)
	newer := seededDraft(t, repo, ws, campaigndraft.ChannelWhatsApp, "newer", base.Add(time.Minute))
	seededDraft(t, repo, ws, campaigndraft.ChannelUnofficialWhatsApp, "unofficial", base)
	seededDraft(t, repo, other, campaigndraft.ChannelWhatsApp, "elsewhere", base)

	listed, err := repo.List(ctx, ws, campaigndraft.ChannelWhatsApp, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != newer.ID || listed[1].ID != older.ID {
		t.Fatalf("listed %+v", listed)
	}

	found, err := repo.Find(ctx, ws, campaigndraft.ChannelWhatsApp, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := found.Request()
	if err != nil || string(raw) != `{"name": "older"}` && string(raw) != `{"name":"older"}` {
		t.Fatalf("request %s %v", raw, err)
	}

	if _, err := repo.Find(ctx, other, campaigndraft.ChannelWhatsApp, older.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("another workspace read the draft: %v", err)
	}
	if _, err := repo.Find(ctx, ws, campaigndraft.ChannelUnofficialWhatsApp, older.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("another channel read the draft: %v", err)
	}

	if err := found.Revise("renamed", json.RawMessage(`{"request":{"name":"renamed"}}`), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, found); err != nil {
		t.Fatal(err)
	}
	again, err := repo.Find(ctx, ws, campaigndraft.ChannelWhatsApp, older.ID)
	if err != nil || again.Name != "renamed" || !again.UpdatedAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("after update %+v %v", again, err)
	}

	stranger := *again
	stranger.WorkspaceID = other
	if err := repo.Update(ctx, &stranger); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("another workspace updated the draft: %v", err)
	}
	if err := repo.Delete(ctx, other, campaigndraft.ChannelWhatsApp, older.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("another workspace deleted the draft: %v", err)
	}
	if err := repo.Delete(ctx, ws, campaigndraft.ChannelWhatsApp, older.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Find(ctx, ws, campaigndraft.ChannelWhatsApp, older.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("deleted draft still found: %v", err)
	}
}
