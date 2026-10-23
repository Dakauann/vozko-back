package carriercampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
)

func draftOf(c cc.Campaign) Draft {
	return Draft{
		Name: c.Name, SenderID: c.SenderID, Channel: c.Channel, Kind: c.Kind, Variants: c.Variants,
		Fallback: c.Fallback, Audience: c.Audience, ScheduledAt: c.ScheduledAt, Recurrence: c.Recurrence,
	}
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrDependencyMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreate(t *testing.T) {
	h := newHarness(t)
	got, err := h.svc.Create(context.Background(), wsID, Actor{UserID: owner}, draftOf(rcsCampaign("")))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.WorkspaceID != wsID || got.Status != cc.StatusDraft || got.CreatedBy != owner || !got.CreatedAt.Equal(testNow) || !got.UpdatedAt.Equal(testNow) {
		t.Fatalf("created %+v", got)
	}
	if stored := h.campaigns.get(got.ID); stored.Name != "Oferta" {
		t.Fatalf("stored %+v", stored)
	}

	bad := draftOf(rcsCampaign(""))
	bad.Channel = "email"
	if _, err := h.svc.Create(context.Background(), wsID, Actor{UserID: owner}, bad); !errors.Is(err, cc.ErrChannelInvalid) {
		t.Fatalf("invalid channel err = %v", err)
	}
	if _, err := h.svc.Create(context.Background(), wsID, Actor{}, draftOf(rcsCampaign(""))); !errors.Is(err, ErrActorRequired) {
		t.Fatalf("no actor err = %v", err)
	}
}

func TestUpdate(t *testing.T) {
	future := testNow.Add(2 * time.Hour)
	cases := []struct {
		name       string
		status     cc.Status
		wantStatus cc.Status
		err        error
	}{
		{"draft stays draft", cc.StatusDraft, cc.StatusDraft, nil},
		{"scheduled goes back to draft", cc.StatusScheduled, cc.StatusDraft, nil},
		{"ready goes back to draft", cc.StatusReady, cc.StatusDraft, nil},
		{"running is refused", cc.StatusRunning, cc.StatusRunning, cc.ErrNotEditable},
		{"completed is refused", cc.StatusCompleted, cc.StatusCompleted, cc.ErrNotEditable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			c := rcsCampaign("c1")
			c.Status = tc.status
			c.ScheduledAt = &future
			c.CreatedAt = testNow.Add(-time.Hour)
			h.campaigns.put(c)
			d := draftOf(c)
			d.Name = "Novo nome"
			d.ScheduledAt = nil
			got, err := h.svc.Update(context.Background(), wsID, "c1", d)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			stored := h.campaigns.get("c1")
			if stored.Status != tc.wantStatus {
				t.Fatalf("status %s, want %s", stored.Status, tc.wantStatus)
			}
			if err != nil {
				if stored.Name != "Oferta" {
					t.Fatal("refused update must not change the campaign")
				}
				return
			}
			if got.Name != "Novo nome" || stored.Name != "Novo nome" || got.CreatedBy != owner || !got.CreatedAt.Equal(c.CreatedAt) || !got.UpdatedAt.Equal(testNow) {
				t.Fatalf("updated %+v", got)
			}
		})
	}
}

func TestUpdateOtherWorkspace(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	if _, err := h.svc.Update(context.Background(), "ws2", "c1", draftOf(rcsCampaign("c1"))); !errors.Is(err, cc.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDelete(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	scheduled := rcsCampaign("c2")
	scheduled.Status = cc.StatusScheduled
	h.campaigns.put(scheduled)
	if err := h.svc.Delete(context.Background(), wsID, "c2"); !errors.Is(err, ErrOnlyDraftDeletes) {
		t.Fatalf("scheduled delete err = %v", err)
	}
	if err := h.svc.Delete(context.Background(), wsID, "c1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.campaigns.find("c1"); ok {
		t.Fatal("draft should be gone")
	}
	ready := rcsCampaign("c3")
	ready.Status = cc.StatusReady
	h.campaigns.put(ready)
	if err := h.svc.Delete(context.Background(), wsID, "c3"); err != nil {
		t.Fatalf("ready delete err = %v", err)
	}
	if _, ok := h.campaigns.find("c3"); ok {
		t.Fatal("ready campaign should be gone")
	}
}

func TestUpdateComparesTheStatusItRead(t *testing.T) {
	h := newHarness(t)
	c := rcsCampaign("c1")
	c.Status = cc.StatusReady
	h.campaigns.put(c)
	h.campaigns.beforeUpdate = func() {
		started := h.campaigns.get("c1")
		started.Status = cc.StatusRunning
		h.campaigns.put(started)
	}
	d := draftOf(c)
	d.Name = "Novo nome"
	if _, err := h.svc.Update(context.Background(), wsID, "c1", d); !errors.Is(err, cc.ErrNotEditable) {
		t.Fatalf("err = %v", err)
	}
	if stored := h.campaigns.get("c1"); stored.Status != cc.StatusRunning || stored.Name != "Oferta" {
		t.Fatalf("a campaign started during the edit must keep running untouched: %+v", stored)
	}
}

func TestDuplicate(t *testing.T) {
	h := newHarness(t)
	src := rcsCampaign("c1")
	src.Status = cc.StatusCompleted
	at := testNow.Add(-time.Hour)
	src.ScheduledAt = &at
	src.Recurrence = &cc.Recurrence{Frequency: cc.FrequencyDaily, Time: "09:00", Timezone: "America/Sao_Paulo"}
	h.campaigns.put(src)

	got, err := h.svc.Duplicate(context.Background(), wsID, "c1", Actor{UserID: "user-2"}, "Oferta de novo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "c1" || got.Name != "Oferta de novo" || got.Status != cc.StatusDraft || got.ScheduledAt != nil || got.CreatedBy != "user-2" || got.Recurrence == nil {
		t.Fatalf("copy %+v", got)
	}
	if _, err := h.svc.Duplicate(context.Background(), wsID, "c1", Actor{UserID: "user-2"}, " "); !errors.Is(err, cc.ErrNameRequired) {
		t.Fatalf("blank name err = %v", err)
	}
}

func TestGetAndList(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	h.campaigns.put(smsCampaign("c2"))
	other := rcsCampaign("c3")
	other.WorkspaceID = "ws2"
	h.campaigns.put(other)

	if _, err := h.svc.Get(context.Background(), wsID, "c3"); !errors.Is(err, cc.ErrCampaignNotFound) {
		t.Fatalf("foreign get err = %v", err)
	}
	page, err := h.svc.List(context.Background(), wsID, cc.ListCampaignsInput{WorkspaceID: "ws2", Channel: carriermessaging.ChannelSMS, Pagination: shared.Pagination{Page: 1, PageSize: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "c2" {
		t.Fatalf("list %+v", page.Items)
	}
}

func TestNewDefaultsTheClockAndIDs(t *testing.T) {
	deps := newHarness(t).svc.deps
	deps.Now, deps.NewID = nil, nil
	svc, err := New(deps)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if svc.deps.NewID() == "" || svc.now().IsZero() {
		t.Fatal("a service without a clock or id source uses the real ones")
	}
}
