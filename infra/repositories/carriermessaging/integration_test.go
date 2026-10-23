package carriermessaging_repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	"vozko/domain/working_hours"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

const racers = 12

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	return repotest.IsolatedDB(t, "rcs_test", schema.CarrierMessagingModels()...)
}

func newReference() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

type messageSeed struct {
	workspaceID string
	channel     carriermessaging.Channel
	parentID    string
	phone       string
	marketing   bool
	runKind     carriermessaging.RunKind
	step        carriermessaging.Step
	status      carriermessaging.Status
	createdAt   time.Time
	deliveredAt *time.Time
	expiresAt   *time.Time
}

func seedMessage(t *testing.T, repo carriermessaging.MessageRepository, s messageSeed) *carriermessaging.Message {
	t.Helper()
	if s.runKind == "" {
		s.runKind = carriermessaging.RunCampaign
	}
	if s.channel == "" {
		s.channel = carriermessaging.ChannelRCS
	}
	if s.step == "" {
		s.step = carriermessaging.StepRCS
		if s.channel == carriermessaging.ChannelSMS {
			s.step = carriermessaging.StepSMS
		}
	}
	class := rcs.ClassBasic
	if s.step != carriermessaging.StepRCS {
		class = ""
	}
	if s.status == "" {
		s.status = carriermessaging.StatusAccepted
	}
	m := &carriermessaging.Message{
		WorkspaceID: s.workspaceID, Channel: s.channel, ParentMessageID: s.parentID, Reference: newReference(), SenderID: uuid.NewString(), Phone: s.phone,
		RunKind: s.runKind, Marketing: s.marketing, Content: rcs.Content{Text: "oi"}, Text: "Loja: oi", Segments: 1, Class: class,
		Step: s.step, Status: s.status, CreatedAt: s.createdAt, DeliveredAt: s.deliveredAt, ExpiresAt: s.expiresAt,
		Fallback: carriermessaging.FallbackSpec{SMSText: "oi"},
	}
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	return m
}

func runConcurrently(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

func TestIntegrationAdvanceFollowsTheDomainOrdering(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	base := time.Now().UTC().Truncate(time.Microsecond)

	steps := []struct {
		name       string
		next       carriermessaging.Status
		at         time.Time
		wantMoved  bool
		wantStatus carriermessaging.Status
	}{
		{"read arrives first", carriermessaging.StatusRead, base.Add(2 * time.Minute), true, carriermessaging.StatusRead},
		{"late delivered does not move read", carriermessaging.StatusDelivered, base.Add(3 * time.Minute), false, carriermessaging.StatusRead},
		{"failed after read does not move", carriermessaging.StatusFailed, base.Add(4 * time.Minute), false, carriermessaging.StatusRead},
	}
	m := seedMessage(t, repo, messageSeed{workspaceID: ws, phone: "5511999990001", createdAt: base})
	for _, step := range steps {
		got, moved, err := repo.Advance(ctx, m.Reference, step.next, "E9", step.at)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if moved != step.wantMoved || got.Status != step.wantStatus {
			t.Fatalf("%s: moved %v status %s", step.name, moved, got.Status)
		}
	}
	stored, err := repo.FindByReference(ctx, m.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != carriermessaging.StatusRead || stored.DeliveredAt == nil || !stored.DeliveredAt.Equal(base.Add(2*time.Minute)) || stored.FailedAt != nil || stored.ErrorCode != "" {
		t.Fatalf("stored %+v", stored)
	}

	delivered := seedMessage(t, repo, messageSeed{workspaceID: ws, phone: "5511999990001", createdAt: base})
	if _, moved, err := repo.Advance(ctx, delivered.Reference, carriermessaging.StatusDelivered, "", base.Add(time.Minute)); err != nil || !moved {
		t.Fatalf("delivered: %v %v", moved, err)
	}
	got, moved, err := repo.Advance(ctx, delivered.Reference, carriermessaging.StatusFailed, "E1", base.Add(2*time.Minute))
	if err != nil || moved || got.Status != carriermessaging.StatusDelivered {
		t.Fatalf("failed after delivered: %+v %v %v", got, moved, err)
	}

	if _, _, err := repo.Advance(ctx, newReference(), carriermessaging.StatusDelivered, "", base); !errors.Is(err, carriermessaging.ErrMessageNotFound) {
		t.Fatalf("unknown reference: %v", err)
	}
}

func TestIntegrationConcurrentAdvanceSettlesOnTheFurthestStatus(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	m := seedMessage(t, repo, messageSeed{workspaceID: uuid.NewString(), phone: "5511999990002"})
	statuses := []carriermessaging.Status{carriermessaging.StatusDelivered, carriermessaging.StatusRead, carriermessaging.StatusAccepted, carriermessaging.StatusFailed}
	var mu sync.Mutex
	var errs []error
	runConcurrently(racers, func(i int) {
		if _, _, err := repo.Advance(ctx, m.Reference, statuses[i%len(statuses)], "", time.Now().UTC()); err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	})
	if len(errs) > 0 {
		t.Fatalf("advance errors: %v", errs)
	}
	stored, err := repo.FindByReference(ctx, m.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Status.Landed() {
		t.Fatalf("a landed message must never end failed or behind: %s", stored.Status)
	}
	if stored.Status == carriermessaging.StatusDelivered {
		if _, moved, _ := repo.Advance(ctx, m.Reference, carriermessaging.StatusRead, "", time.Now().UTC()); !moved {
			t.Fatal("read must still move a delivered message")
		}
	}
}

func TestIntegrationLinkFallbackHasOneWinner(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	m := seedMessage(t, repo, messageSeed{workspaceID: uuid.NewString(), phone: "5511999990003"})
	candidates := make([]string, racers)
	for i := range candidates {
		candidates[i] = uuid.NewString()
	}
	var mu sync.Mutex
	var winners []string
	var errs []error
	runConcurrently(racers, func(i int) {
		linked, err := repo.LinkFallback(ctx, m.ID, candidates[i])
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, err)
		}
		if linked {
			winners = append(winners, candidates[i])
		}
	})
	if len(errs) > 0 || len(winners) != 1 {
		t.Fatalf("winners %v errors %v", winners, errs)
	}
	stored, err := repo.FindByID(ctx, m.ID)
	if err != nil || stored.FallbackMessageID != winners[0] {
		t.Fatalf("stored %+v %v", stored, err)
	}
	if _, err := repo.LinkFallback(ctx, uuid.NewString(), uuid.NewString()); !errors.Is(err, carriermessaging.ErrMessageNotFound) {
		t.Fatalf("missing message: %v", err)
	}
}

func TestIntegrationMessageCountsAndScans(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	phone := "5511999990004"
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-24 * time.Hour)
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)
	expired := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	deliveredEarly := now.Add(-3 * time.Hour)
	deliveredLate := now.Add(-2 * time.Hour)
	deliveredLatest := now.Add(-90 * time.Minute)

	seed := func(s messageSeed) *carriermessaging.Message { return seedMessage(t, repo, s) }
	seed(messageSeed{workspaceID: ws, phone: phone, marketing: true, createdAt: recent, status: carriermessaging.StatusPending, expiresAt: &expired})
	delivered := seed(messageSeed{workspaceID: ws, phone: phone, marketing: true, createdAt: recent, status: carriermessaging.StatusDelivered, deliveredAt: &deliveredEarly})
	seed(messageSeed{workspaceID: ws, phone: phone, marketing: true, createdAt: recent, status: carriermessaging.StatusRejected})
	seed(messageSeed{workspaceID: ws, phone: phone, marketing: true, parentID: delivered.ID, createdAt: recent, step: carriermessaging.StepWhatsApp, status: carriermessaging.StatusDelivered, deliveredAt: &future})
	sms := seed(messageSeed{workspaceID: ws, channel: carriermessaging.ChannelSMS, phone: phone, marketing: true, createdAt: recent, status: carriermessaging.StatusDelivered, deliveredAt: &deliveredLatest})
	seed(messageSeed{workspaceID: ws, phone: phone, runKind: carriermessaging.RunOperator, createdAt: recent, status: carriermessaging.StatusRead, deliveredAt: &deliveredLate})
	seed(messageSeed{workspaceID: ws, phone: phone, marketing: true, createdAt: old, status: carriermessaging.StatusAccepted, expiresAt: &future})
	seed(messageSeed{workspaceID: ws, phone: "5511999990099", marketing: true, createdAt: recent})
	seed(messageSeed{workspaceID: uuid.NewString(), phone: phone, marketing: true, createdAt: recent})
	seed(messageSeed{workspaceID: ws, phone: phone, runKind: carriermessaging.RunTest, createdAt: recent})
	seed(messageSeed{workspaceID: ws, phone: phone, runKind: carriermessaging.RunTest, createdAt: old})

	if got, err := repo.CountMarketingSince(ctx, ws, phone, since); err != nil || got != 3 {
		t.Fatalf("marketing count %d %v", got, err)
	}
	if got, err := repo.CountRunKindSince(ctx, ws, carriermessaging.RunTest, since); err != nil || got != 1 {
		t.Fatalf("test sends %d %v", got, err)
	}
	latest, err := repo.LatestDeliveredTo(ctx, ws, phone, since)
	if err != nil || latest == nil || latest.ID != sms.ID || latest.Channel != carriermessaging.ChannelSMS || latest.Segments != 1 {
		t.Fatalf("latest delivered %+v %v", latest, err)
	}
	if none, err := repo.LatestDeliveredTo(ctx, ws, "5511999990098", since); err != nil || none != nil {
		t.Fatalf("no delivery %+v %v", none, err)
	}
	overdue, err := repo.ListOverdue(ctx, now, 10)
	if err != nil || len(overdue) != 1 || overdue[0].Status != carriermessaging.StatusPending {
		t.Fatalf("overdue %+v %v", overdue, err)
	}
}

func TestIntegrationLatestSMSToCrossesWorkspacesAndIncludesFallbackChildren(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	phone := "5511999990010"
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-30 * 24 * time.Hour)
	seed := func(s messageSeed) *carriermessaging.Message { return seedMessage(t, repo, s) }
	seed(messageSeed{workspaceID: uuid.NewString(), channel: carriermessaging.ChannelSMS, phone: phone, createdAt: now.Add(-40 * 24 * time.Hour)})
	seed(messageSeed{workspaceID: uuid.NewString(), channel: carriermessaging.ChannelSMS, phone: phone, createdAt: now.Add(-2 * time.Hour)})
	parent := seed(messageSeed{workspaceID: uuid.NewString(), phone: phone, createdAt: now.Add(-3 * time.Hour)})
	child := seed(messageSeed{workspaceID: parent.WorkspaceID, channel: carriermessaging.ChannelSMS, parentID: parent.ID, phone: phone, createdAt: now.Add(-time.Hour)})
	seed(messageSeed{workspaceID: uuid.NewString(), phone: phone, createdAt: now.Add(-time.Minute)})

	got, err := repo.LatestSMSTo(ctx, phone, since)
	if err != nil || got == nil || got.ID != child.ID || got.WorkspaceID != parent.WorkspaceID || got.ParentMessageID != parent.ID {
		t.Fatalf("latest sms %+v %v", got, err)
	}
	if none, err := repo.LatestSMSTo(ctx, "5511999990011", since); err != nil || none != nil {
		t.Fatalf("no sms %+v %v", none, err)
	}

	if err := repo.RecordOperator(ctx, child.Reference, "Claro"); err != nil {
		t.Fatal(err)
	}
	var operator string
	if err := db.Raw("SELECT operator FROM carrier_messages WHERE id = ?", child.ID).Scan(&operator).Error; err != nil || operator != "Claro" {
		t.Fatalf("operator %q %v", operator, err)
	}
	if err := repo.RecordOperator(ctx, newReference(), "Claro"); !errors.Is(err, carriermessaging.ErrMessageNotFound) {
		t.Fatalf("unknown reference: %v", err)
	}
}

func TestIntegrationContactFindOrCreateRace(t *testing.T) {
	db := integrationDB(t)
	repo := NewContactRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	ids := make([]string, racers)
	var mu sync.Mutex
	var errs []error
	runConcurrently(racers, func(i int) {
		c, err := repo.FindOrCreate(ctx, ws, "5511999990005", "Ana")
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, err)
			return
		}
		ids[i] = c.ID
	})
	if len(errs) > 0 {
		t.Fatalf("errors %v", errs)
	}
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("racers got different contacts: %v", ids)
		}
	}
	var rows int64
	if err := db.Model(&schema.CarrierContact{}).Where("workspace_id = ?", ws).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("rows %d %v", rows, err)
	}

	at := time.Now().UTC()
	if changed, err := repo.MarkFlooding(ctx, ids[0], at); err != nil || !changed {
		t.Fatalf("first flood mark %v %v", changed, err)
	}
	if changed, err := repo.MarkFlooding(ctx, ids[0], at.Add(time.Minute)); err != nil || changed {
		t.Fatalf("second flood mark %v %v", changed, err)
	}
	if _, err := repo.MarkFlooding(ctx, uuid.NewString(), at); !errors.Is(err, carriermessaging.ErrContactNotFound) {
		t.Fatalf("missing contact: %v", err)
	}
	lead := uuid.NewString()
	if err := repo.SetLead(ctx, ids[0], lead); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, ids[0])
	if err != nil || got.LeadID == nil || *got.LeadID != lead || got.FloodingAt == nil {
		t.Fatalf("contact %+v %v", got, err)
	}

	if err := db.Delete(&schema.CarrierContact{}, "id = ?", ids[0]).Error; err != nil {
		t.Fatal(err)
	}
	fresh, err := repo.FindOrCreate(ctx, ws, "5511999990005", "Ana")
	if err != nil || fresh.ID == ids[0] {
		t.Fatalf("a deleted contact must not block a new one: %+v %v", fresh, err)
	}
}

func TestIntegrationConversationFindOrCreateRaceAndBilledWindow(t *testing.T) {
	db := integrationDB(t)
	repo := NewRCSConversationRepository(db)
	ctx := context.Background()
	ws, sender, contact := uuid.NewString(), uuid.NewString(), uuid.NewString()
	var mu sync.Mutex
	var created int
	ids := map[string]bool{}
	var errs []error
	runConcurrently(racers, func(int) {
		conv, isNew, err := repo.FindOrCreate(ctx, ws, sender, contact)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, err)
			return
		}
		ids[conv.ID] = true
		if isNew {
			created++
		}
	})
	if len(errs) > 0 || created != 1 || len(ids) != 1 {
		t.Fatalf("created %d ids %v errors %v", created, ids, errs)
	}
	var id string
	for k := range ids {
		id = k
	}

	start := time.Now().UTC().Truncate(time.Microsecond)
	window := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"first message opens", start, true},
		{"inside the window", start.Add(23 * time.Hour), false},
		{"window over", start.Add(carriermessaging.ConversationWindow), true},
		{"new window holds", start.Add(carriermessaging.ConversationWindow + time.Hour), false},
	}
	for _, w := range window {
		got, err := repo.OpenBilledConversation(ctx, id, w.at)
		if err != nil || got != w.want {
			t.Fatalf("%s: got %v %v", w.name, got, err)
		}
	}
	conv, err := repo.FindByID(ctx, id)
	if err != nil || conv.ConversationOpenedAt == nil || !conv.ConversationOpenedAt.Equal(start.Add(carriermessaging.ConversationWindow)) {
		t.Fatalf("conversation %+v %v", conv, err)
	}
	if _, err := repo.OpenBilledConversation(ctx, uuid.NewString(), start); !errors.Is(err, carriermessaging.ErrConversationNotFound) {
		t.Fatalf("missing conversation: %v", err)
	}

	if err := repo.RecordInbound(ctx, id, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordOutbound(ctx, id, start); err != nil {
		t.Fatal(err)
	}
	conv, err = repo.FindByID(ctx, id)
	if err != nil || !conv.LastMessageAt.Equal(start.Add(time.Hour)) || !conv.LastCustomerMessageAt.Equal(start.Add(time.Hour)) || !conv.LastAgentMessageAt.Equal(start) {
		t.Fatalf("clocks %+v %v", conv, err)
	}
}

func TestIntegrationEventDedupAndCountSince(t *testing.T) {
	db := integrationDB(t)
	repo := NewEventRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	phone := "5511999990006"
	now := time.Now().UTC()
	events := []struct {
		kind  carriermessaging.EventKind
		key   string
		at    time.Time
		phone string
		want  bool
	}{
		{carriermessaging.EventInbound, "a", now.Add(-10 * time.Minute), phone, true},
		{carriermessaging.EventInbound, "a", now.Add(-10 * time.Minute), phone, false},
		{carriermessaging.EventInbound, "b", now.Add(-20 * time.Minute), phone, true},
		{carriermessaging.EventInbound, "c", now.Add(-2 * time.Hour), phone, true},
		{carriermessaging.EventClick, "d", now.Add(-time.Minute), phone, true},
		{carriermessaging.EventInbound, "e", now.Add(-time.Minute), "5511999990097", true},
	}
	for _, e := range events {
		got, err := repo.Record(ctx, &carriermessaging.MessageEvent{WorkspaceID: ws, Phone: e.phone, Kind: e.kind, DedupKey: e.key, OccurredAt: e.at,
			Button: &rcs.ButtonKey{Card: 0, Index: 1}})
		if err != nil || got != e.want {
			t.Fatalf("record %s: %v %v", e.key, got, err)
		}
	}
	var same atomic.Int64
	runConcurrently(racers, func(int) {
		if ok, err := repo.Record(ctx, &carriermessaging.MessageEvent{WorkspaceID: ws, Phone: phone, Kind: carriermessaging.EventTap, DedupKey: "race", OccurredAt: now}); err == nil && ok {
			same.Add(1)
		}
	})
	if same.Load() != 1 {
		t.Fatalf("concurrent redeliveries inserted %d rows", same.Load())
	}
	if got, err := repo.CountSince(ctx, ws, phone, carriermessaging.EventInbound, now.Add(-time.Hour)); err != nil || got != 2 {
		t.Fatalf("count %d %v", got, err)
	}
}

func TestIntegrationSuppressions(t *testing.T) {
	db := integrationDB(t)
	repo := NewSuppressionRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	for i, want := range []bool{true, false} {
		got, err := repo.Add(ctx, carriermessaging.Suppression{WorkspaceID: ws, Phone: "5511999990007", Source: carriermessaging.SuppressionKeyword})
		if err != nil || got != want {
			t.Fatalf("add %d: %v %v", i, got, err)
		}
	}
	if _, err := repo.Add(ctx, carriermessaging.Suppression{WorkspaceID: ws, Phone: "5511999990008", Source: carriermessaging.SuppressionButton, MessageID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	among, err := repo.SuppressedAmong(ctx, ws, []string{"5511999990007", "5511999990008", "5511999990009"})
	if err != nil || len(among) != 2 || !among["5511999990007"] || among["5511999990009"] {
		t.Fatalf("among %v %v", among, err)
	}
	list, total, err := repo.List(ctx, ws, 1, 0)
	if err != nil || total != 2 || len(list) != 1 {
		t.Fatalf("list %+v %d %v", list, total, err)
	}
	if err := repo.Remove(ctx, ws, "5511999990007"); err != nil {
		t.Fatal(err)
	}
	if suppressed, err := repo.IsSuppressed(ctx, ws, "5511999990007"); err != nil || suppressed {
		t.Fatalf("after remove %v %v", suppressed, err)
	}
	if suppressed, err := repo.IsSuppressed(ctx, ws, "5511999990008"); err != nil || !suppressed {
		t.Fatalf("still suppressed %v %v", suppressed, err)
	}
}

func TestIntegrationSettingsUpsert(t *testing.T) {
	db := integrationDB(t)
	repo := NewSettingsRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	if got, err := repo.Get(ctx, ws); err != nil || got != nil {
		t.Fatalf("missing settings %+v %v", got, err)
	}
	first := carriermessaging.DefaultSettings(ws, "America/Sao_Paulo")
	if err := repo.Put(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.FrequencyCap = carriermessaging.FrequencyCap{PerDay: 1, PerWeek: 3}
	second.AttributionDays = 30
	second.QuietHours = working_hours.Spec{Timezone: "America/Manaus", Days: map[string][]working_hours.Window{"mon": {{Start: "10:00", End: "16:00"}}}}
	if err := repo.Put(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, ws)
	if err != nil || got.FrequencyCap != second.FrequencyCap || got.AttributionDays != 30 || got.QuietHours.Timezone != "America/Manaus" || got.QuietHours.Days["mon"][0].Start != "10:00" {
		t.Fatalf("settings %+v %v", got, err)
	}
}

func TestIntegrationSenderLifecycle(t *testing.T) {
	db := integrationDB(t)
	repo := NewSenderRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	now := time.Now().UTC()
	s := &rcs.Sender{WorkspaceID: ws, BrandName: "Loja", Status: rcs.SenderDraft, Provider: "cm"}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.Status, s.PausedBy, s.PauseReason, s.PausedAt, s.ProviderProfileID = rcs.SenderPaused, rcs.PausedByPlatform, rcs.PauseManual, &now, "prof-1"
	if err := repo.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.Status, s.PausedBy, s.PauseReason, s.PausedAt = rcs.SenderSubmitted, "", "", nil
	if err := repo.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByProfileID(ctx, "cm", "prof-1")
	if err != nil || got.ID != s.ID || got.PausedBy != "" || got.PausedAt != nil || got.Status != rcs.SenderSubmitted {
		t.Fatalf("sender %+v %v", got, err)
	}
	if _, err := repo.FindByID(ctx, uuid.NewString(), s.ID); !errors.Is(err, rcs.ErrSenderNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	awaiting, err := repo.ListAwaitingProvider(ctx, 10)
	if err != nil || len(awaiting) != 1 {
		t.Fatalf("awaiting %v %v", awaiting, err)
	}
	if err := repo.Delete(ctx, ws, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByIDUnscoped(ctx, s.ID); !errors.Is(err, rcs.ErrSenderNotFound) {
		t.Fatalf("deleted sender: %v", err)
	}
	if err := repo.Update(ctx, s); !errors.Is(err, rcs.ErrSenderNotFound) {
		t.Fatalf("update deleted sender: %v", err)
	}
}

func TestIntegrationUniqueKeysExistInPostgres(t *testing.T) {
	db := integrationDB(t)
	want := map[string]string{
		"ux_carrier_contacts_workspace_phone":     "(workspace_id, phone) WHERE (deleted_at IS NULL)",
		"idx_rcs_conversations_thread":            "(workspace_id, sender_id, contact_id) WHERE (deleted_at IS NULL)",
		"idx_sms_conversations_thread":            "(workspace_id, sender_id, contact_id) WHERE (deleted_at IS NULL)",
		"ux_sms_routes_dedicated_number":          "(number) WHERE (((kind)::text = 'dedicated'::text) AND (deleted_at IS NULL))",
		"ux_carrier_messages_reference":           "(reference)",
		"ux_carrier_message_events_dedup":         "(dedup_key)",
		"ux_carrier_suppressions_workspace_phone": "(workspace_id, phone)",
	}
	for name, suffix := range want {
		var def string
		if err := db.Raw("SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?", name).Scan(&def).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(def, "CREATE UNIQUE INDEX") || !strings.HasSuffix(def, suffix) {
			t.Fatalf("%s = %q, want a unique index ending in %q", name, def, suffix)
		}
	}
}

func TestIntegrationSMSConversationsAreSeparateFromRCS(t *testing.T) {
	db := integrationDB(t)
	rcsRepo := NewRCSConversationRepository(db)
	smsRepo := NewSMSConversationRepository(db)
	ctx := context.Background()
	ws, sender, contact := uuid.NewString(), uuid.NewString(), uuid.NewString()
	var created atomic.Int64
	runConcurrently(racers, func(int) {
		if _, isNew, err := smsRepo.FindOrCreate(ctx, ws, sender, contact); err == nil && isNew {
			created.Add(1)
		}
	})
	if created.Load() != 1 {
		t.Fatalf("sms racers created %d conversations", created.Load())
	}
	smsConv, _, err := smsRepo.FindOrCreate(ctx, ws, sender, contact)
	if err != nil || smsConv.Channel != carriermessaging.ChannelSMS {
		t.Fatalf("sms conversation %+v %v", smsConv, err)
	}
	rcsConv, isNew, err := rcsRepo.FindOrCreate(ctx, ws, sender, contact)
	if err != nil || !isNew || rcsConv.ID == smsConv.ID || rcsConv.Channel != carriermessaging.ChannelRCS {
		t.Fatalf("rcs conversation %+v %v %v", rcsConv, isNew, err)
	}
	if _, err := rcsRepo.FindByID(ctx, smsConv.ID); !errors.Is(err, carriermessaging.ErrConversationNotFound) {
		t.Fatalf("an sms conversation must not be found as rcs: %v", err)
	}
	at := time.Now().UTC()
	if opened, err := smsRepo.OpenBilledConversation(ctx, smsConv.ID, at); err != nil || !opened {
		t.Fatalf("sms billed window %v %v", opened, err)
	}
	if err := smsRepo.RecordOutbound(ctx, smsConv.ID, at); err != nil {
		t.Fatal(err)
	}
	got, err := smsRepo.FindByIDs(ctx, []string{smsConv.ID, rcsConv.ID})
	if err != nil || len(got) != 1 || got[0].LastAgentMessageAt == nil || got[0].ConversationOpenedAt == nil {
		t.Fatalf("sms by ids %+v %v", got, err)
	}
}

func TestIntegrationRoutes(t *testing.T) {
	db := integrationDB(t)
	repo := NewRouteRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	shared := &sms.Route{WorkspaceID: ws, BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive}
	if err := repo.Create(ctx, shared); err != nil {
		t.Fatal(err)
	}
	otherShared := &sms.Route{WorkspaceID: uuid.NewString(), BrandPrefix: "Outra", Kind: sms.RouteShared, Status: sms.RouteActive}
	if err := repo.Create(ctx, otherShared); err != nil {
		t.Fatalf("two shared routes must coexist: %v", err)
	}
	dedicated := &sms.Route{WorkspaceID: ws, BrandPrefix: "Loja", Kind: sms.RouteDedicated, Number: "29123", Status: sms.RouteActive}
	if err := repo.Create(ctx, dedicated); err != nil {
		t.Fatal(err)
	}
	clash := &sms.Route{WorkspaceID: uuid.NewString(), BrandPrefix: "Rival", Kind: sms.RouteDedicated, Number: "29123", Status: sms.RouteActive}
	if err := repo.Create(ctx, clash); err == nil {
		t.Fatal("a dedicated number must belong to one live route")
	}
	got, err := repo.FindByNumber(ctx, "29123")
	if err != nil || got.ID != dedicated.ID {
		t.Fatalf("by number %+v %v", got, err)
	}
	if _, err := repo.FindByNumber(ctx, "40000"); !errors.Is(err, sms.ErrRouteNotFound) {
		t.Fatalf("unknown number: %v", err)
	}
	dedicated.Status, dedicated.PauseReason = sms.RoutePaused, "manual"
	if err := repo.Update(ctx, dedicated); err != nil {
		t.Fatal(err)
	}
	dedicated.Status, dedicated.PauseReason = sms.RouteActive, ""
	if err := repo.Update(ctx, dedicated); err != nil {
		t.Fatal(err)
	}
	got, err = repo.FindByID(ctx, ws, dedicated.ID)
	if err != nil || got.Status != sms.RouteActive || got.PauseReason != "" {
		t.Fatalf("after resume %+v %v", got, err)
	}
	if _, err := repo.FindByID(ctx, uuid.NewString(), dedicated.ID); !errors.Is(err, sms.ErrRouteNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	list, err := repo.ListByWorkspace(ctx, ws)
	if err != nil || len(list) != 2 {
		t.Fatalf("workspace routes %v %v", list, err)
	}
	if err := repo.Delete(ctx, ws, dedicated.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, clash); err != nil {
		t.Fatalf("a deleted route frees its number: %v", err)
	}
	all, err := repo.ListAll(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("all routes %v %v", all, err)
	}
}

func TestIntegrationAutomationColumnsRoundTripAndClear(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	agent := uuid.NewString()
	on := conversation.ChannelAutomation{AgentID: &agent, EnableAgentResponses: true, EnableAutoMemory: true, Disclosure: "Assistente"}

	routes := NewRouteRepository(db)
	route := &sms.Route{WorkspaceID: uuid.NewString(), BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive, Automation: on}
	if err := routes.Create(ctx, route); err != nil {
		t.Fatal(err)
	}
	gotRoute, err := routes.FindByIDUnscoped(ctx, route.ID)
	if err != nil || gotRoute.Automation.AgentID == nil || *gotRoute.Automation.AgentID != agent || !gotRoute.Automation.EnableAutoMemory || gotRoute.Automation.Disclosure != "Assistente" {
		t.Fatalf("route automation %+v %v", gotRoute, err)
	}
	route.Automation = conversation.ChannelAutomation{}
	if err := routes.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	if gotRoute, err = routes.FindByIDUnscoped(ctx, route.ID); err != nil || gotRoute.Automation != (conversation.ChannelAutomation{}) {
		t.Fatalf("cleared route automation %+v %v", gotRoute.Automation, err)
	}

	senders := NewSenderRepository(db)
	sender := &rcs.Sender{WorkspaceID: uuid.NewString(), Status: rcs.SenderDraft, Automation: on}
	if err := senders.Create(ctx, sender); err != nil {
		t.Fatal(err)
	}
	sender.Automation = conversation.ChannelAutomation{}
	if err := senders.Update(ctx, sender); err != nil {
		t.Fatal(err)
	}
	gotSender, err := senders.FindByIDUnscoped(ctx, sender.ID)
	if err != nil || gotSender.Automation != (conversation.ChannelAutomation{}) {
		t.Fatalf("cleared sender automation %+v %v", gotSender, err)
	}
}

func createLedgerTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE balance_transactions (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, type varchar(20) NOT NULL, amount bigint NOT NULL,
		service_type varchar(50) NOT NULL, reference_id varchar(255), is_refund boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
}

func ledger(t *testing.T, db *gorm.DB, ws, kind, service string, amount int64, refund bool, reference string, at time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO balance_transactions (id, workspace_id, type, amount, service_type, reference_id, is_refund, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), ws, kind, amount, service, reference, refund, at).Error; err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationSpendSinceNetsCarrierRefunds(t *testing.T) {
	db := integrationDB(t)
	createLedgerTable(t, db)
	ws := uuid.NewString()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ledger(t, db, ws, "debit", "rcs", 500, false, "carrier:rcs_basic:a", at)
	ledger(t, db, ws, "credit", "rcs", 200, true, "refund:carrier:rcs_basic:a", at)
	ledger(t, db, ws, "debit", "sms", 300, false, "carrier:sms_segment:b", at)
	ledger(t, db, ws, "debit", "whatsapp_campaign", 999, false, "x", at)
	ledger(t, db, ws, "credit", "rcs", 5000, false, "topup", at)
	ledger(t, db, ws, "debit", "rcs", 70, false, "carrier:rcs_basic:old", at.Add(-60*24*time.Hour))
	ledger(t, db, uuid.NewString(), "debit", "rcs", 40, false, "carrier:rcs_basic:c", at)
	spent, err := NewSpendReader(db).SpendSince(context.Background(), ws, at.Add(-30*24*time.Hour))
	if err != nil || spent != 600 {
		t.Fatalf("spend since %d %v", spent, err)
	}
}

func TestIntegrationUpdatesKeepTheInboxContainerColumns(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	department, pipeline := uuid.NewString(), uuid.NewString()
	route := &sms.Route{WorkspaceID: uuid.NewString(), BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive}
	routes := NewRouteRepository(db)
	if err := routes.Create(ctx, route); err != nil {
		t.Fatal(err)
	}
	sender := &rcs.Sender{WorkspaceID: uuid.NewString(), Status: rcs.SenderDraft}
	senders := NewSenderRepository(db)
	if err := senders.Create(ctx, sender); err != nil {
		t.Fatal(err)
	}
	for _, table := range []struct{ name, id string }{{"sms_routes", route.ID}, {"rcs_senders", sender.ID}} {
		if err := db.Exec("UPDATE "+table.name+" SET department_id = ?, pipeline_id = ? WHERE id = ?", department, pipeline, table.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	route.Status = sms.RoutePaused
	if err := routes.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	sender.BrandName = "Loja"
	if err := senders.Update(ctx, sender); err != nil {
		t.Fatal(err)
	}
	for _, table := range []struct{ name, id string }{{"sms_routes", route.ID}, {"rcs_senders", sender.ID}} {
		var row struct {
			DepartmentID *string
			PipelineID   *string
		}
		if err := db.Raw("SELECT department_id, pipeline_id FROM "+table.name+" WHERE id = ?", table.id).Scan(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.DepartmentID == nil || *row.DepartmentID != department || row.PipelineID == nil || *row.PipelineID != pipeline {
			t.Fatalf("%s lost its department or funnel on update: %+v", table.name, row)
		}
	}
}
