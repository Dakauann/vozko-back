package emailsending_repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "email_sending_test", schema.EmailSendingModels()...)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(40)
	return db
}

func TestIntegrationTakeDailySlotIsExactUnderConcurrency(t *testing.T) {
	db := integrationDB(t)
	repo := NewDailyUsageRepository(db)
	ctx := context.Background()
	domainID := uuid.NewString()
	const capValue, attempts = 50, 200
	var granted atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.TakeDailySlot(ctx, domainID, "2026-10-21", capValue)
			if err != nil {
				errs <- err
				return
			}
			if ok {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if granted.Load() != capValue {
		t.Fatalf("granted %d slots, want exactly %d", granted.Load(), capValue)
	}
	used, err := repo.Used(ctx, domainID, "2026-10-21")
	if err != nil || used != capValue {
		t.Fatalf("used = %d err = %v", used, err)
	}
	if err := repo.GiveBackDailySlot(ctx, domainID, "2026-10-21"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := repo.TakeDailySlot(ctx, domainID, "2026-10-21", capValue); !ok {
		t.Fatal("a returned slot must be takeable again")
	}
	if ok, _ := repo.TakeDailySlot(ctx, domainID, "2026-10-21", capValue); ok {
		t.Fatal("the day is full again")
	}
	if ok, _ := repo.TakeDailySlot(ctx, domainID, "2026-10-22", capValue); !ok {
		t.Fatal("a new day starts empty")
	}
	for i := 0; i < 3; i++ {
		if ok, err := repo.TakeDailySlot(ctx, domainID, "2026-10-23", es.Uncapped); !ok || err != nil {
			t.Fatalf("uncapped refused: %v", err)
		}
	}
	if used, _ := repo.Used(ctx, domainID, "2026-10-23"); used != 3 {
		t.Fatalf("uncapped sends must still be counted, used = %d", used)
	}
	if used, _ := repo.Used(ctx, uuid.NewString(), "2026-10-21"); used != 0 {
		t.Fatalf("unknown domain used = %d", used)
	}
	if err := repo.GiveBackDailySlot(ctx, uuid.NewString(), "2026-10-21"); err != nil {
		t.Fatalf("give back on an empty day must be harmless: %v", err)
	}
}

func seedMessage(t *testing.T, repo es.MessageRepository, workspaceID, domainID, providerID string, sentAt time.Time) *es.Message {
	t.Helper()
	m := &es.Message{
		WorkspaceID:       workspaceID,
		ProviderMessageID: providerID,
		RunKind:           es.RunCampaign,
		RunID:             uuid.NewString(),
		DomainID:          domainID,
		Address:           "cliente@example.com",
		Status:            es.MessageSent,
		SentAt:            sentAt,
	}
	if err := repo.Record(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestIntegrationAppendEventDedupsAndKeepsCounters(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	m := seedMessage(t, repo, uuid.NewString(), uuid.NewString(), "ses-"+uuid.NewString(), at)

	delivery := es.DeliveryEvent{Kind: es.EventDelivery, ProviderMessageID: m.ProviderMessageID, OccurredAt: at.Add(time.Minute)}
	first, err := repo.AppendEvent(ctx, m.ID, delivery)
	if err != nil || !first.New || first.Message.Status != es.MessageDelivered {
		t.Fatalf("first = %+v err = %v", first, err)
	}
	again, err := repo.AppendEvent(ctx, m.ID, delivery)
	if err != nil || again.New {
		t.Fatalf("duplicate = %+v err = %v", again, err)
	}

	const opens = 25
	var wg sync.WaitGroup
	errs := make(chan error, opens*2)
	for i := 0; i < opens; i++ {
		ev := es.DeliveryEvent{Kind: es.EventOpen, ProviderMessageID: m.ProviderMessageID, OccurredAt: at.Add(time.Duration(i+2) * time.Minute)}
		for copyN := 0; copyN < 2; copyN++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := repo.AppendEvent(ctx, m.ID, ev); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Opens != opens || got.FirstOpenAt == nil || got.Status != es.MessageDelivered {
		t.Fatalf("message = %+v", got)
	}

	click := es.DeliveryEvent{Kind: es.EventClick, ProviderMessageID: m.ProviderMessageID, OccurredAt: at.Add(time.Hour), Link: "https://loja.test/a"}
	other := click
	other.Link = "https://loja.test/b"
	for _, ev := range []es.DeliveryEvent{click, other, click} {
		if _, err := repo.AppendEvent(ctx, m.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = repo.Get(ctx, m.ID)
	if got.Clicks != 2 {
		t.Fatalf("clicks = %d, want 2 (same instant, two links, one duplicate)", got.Clicks)
	}
	var events int64
	db.Model(&schema.EmailMessageEvent{}).Where("message_id = ?", m.ID).Count(&events)
	if events != 1+opens+2 {
		t.Fatalf("events = %d", events)
	}
	if _, err := repo.AppendEvent(ctx, uuid.NewString(), delivery); !errors.Is(err, es.ErrMessageNotFound) {
		t.Fatalf("unknown message = %v", err)
	}
	byProvider, err := repo.GetByProviderID(ctx, m.ProviderMessageID)
	if err != nil || byProvider.ID != m.ID {
		t.Fatalf("by provider = %+v err = %v", byProvider, err)
	}
}

func TestIntegrationQueuedMessagesHaveNoProviderIDYet(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	ws, dom := uuid.NewString(), uuid.NewString()
	for i := 0; i < 2; i++ {
		m := &es.Message{WorkspaceID: ws, RunKind: es.RunTriggered, DomainID: dom, Address: "a@b.com", Status: es.MessageQueued}
		if err := repo.Record(ctx, m); err != nil {
			t.Fatalf("queued rows without provider id must coexist: %v", err)
		}
	}
	if _, err := repo.GetByProviderID(ctx, ""); !errors.Is(err, es.ErrMessageNotFound) {
		t.Fatalf("empty provider id = %v", err)
	}
}

func TestIntegrationHealthWindowCountsProbationOnly(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	domains := NewDomainRepository(db)
	messages := NewMessageRepository(db)
	ws := uuid.NewString()
	probation := &es.SendingDomain{WorkspaceID: ws, AccountID: uuid.NewString(), Domain: "joana.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic, MailFromDomain: "bounce.joana.envios.test", ReplyDomain: "joana.envios.test"}
	own := &es.SendingDomain{WorkspaceID: ws, AccountID: probation.AccountID, Domain: "loja.com.br", Kind: es.DomainOwn, Status: es.DomainVerified, ConnectMethod: es.ConnectManual, MailFromDomain: "bounce.loja.com.br", ReplyDomain: "reply.loja.com.br"}
	for _, d := range []*es.SendingDomain{probation, own} {
		if err := domains.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	since := now.Add(-14 * 24 * time.Hour)
	stamp := now.Add(-time.Hour)
	mk := func(domainID string, sentAt time.Time, mutate func(*es.Message)) {
		m := seedMessage(t, messages, ws, domainID, "ses-"+uuid.NewString(), sentAt)
		mutate(m)
		if err := messages.Update(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	mk(probation.ID, stamp, func(m *es.Message) { m.DeliveredAt = &stamp })
	mk(probation.ID, stamp, func(m *es.Message) { m.DeliveredAt = &stamp; m.ComplainedAt = &stamp })
	mk(probation.ID, stamp, func(m *es.Message) { m.BouncedAt = &stamp; m.BounceKind = es.BounceHard })
	mk(probation.ID, stamp, func(m *es.Message) { m.BouncedAt = &stamp; m.BounceKind = es.BounceSoft })
	mk(probation.ID, since.Add(-time.Hour), func(m *es.Message) { m.DeliveredAt = &stamp })
	mk(own.ID, stamp, func(m *es.Message) { m.DeliveredAt = &stamp })
	w, err := messages.HealthWindow(ctx, ws, since)
	if err != nil {
		t.Fatal(err)
	}
	if w.Delivered != 2 || w.HardBounces != 1 || w.Complaints != 1 {
		t.Fatalf("window = %+v", w)
	}
}

func TestIntegrationDomainsSendersAndSuppressions(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	domains := NewDomainRepository(db)
	senders := NewSenderRepository(db)
	suppressions := NewSuppressionRepository(db)
	ws := uuid.NewString()

	acct := &es.SenderAccount{WorkspaceID: ws, Provider: "ses", TenantName: "ws-" + ws, Mode: es.ModeProbation, Status: es.AccountActive, Slug: "joana", ProbationStartedAt: time.Now().UTC()}
	if err := accounts.Create(ctx, acct); err != nil {
		t.Fatal(err)
	}
	if taken, _ := accounts.SlugTaken(ctx, "joana"); !taken {
		t.Fatal("slug must be taken")
	}
	if err := accounts.Create(ctx, &es.SenderAccount{WorkspaceID: ws, Provider: "ses", TenantName: "other", Mode: es.ModeProbation, Status: es.AccountActive, Slug: "other"}); err == nil {
		t.Fatal("one account per workspace")
	}
	acct.Mode = es.ModeTrusted
	if err := accounts.Update(ctx, acct); err != nil {
		t.Fatal(err)
	}
	if got, _ := accounts.GetByTenantName(ctx, acct.TenantName); got == nil || got.Mode != es.ModeTrusted {
		t.Fatalf("account = %+v", got)
	}
	if list, total, err := accounts.List(ctx, 0, 10); err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list = %v %d %v", list, total, err)
	}

	d := &es.SendingDomain{WorkspaceID: ws, AccountID: acct.ID, Domain: "joana.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerifying, ConnectMethod: es.ConnectAutomatic, MailFromDomain: "bounce.joana.envios.test", ReplyDomain: "joana.envios.test",
		Records: []es.DNSRecord{{Purpose: es.RecordReplyMX, Type: "MX", Name: "joana.envios.test", Value: "inbound", Priority: 10, Status: es.RecordVerified}}}
	if err := domains.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := domains.Create(ctx, &es.SendingDomain{WorkspaceID: uuid.NewString(), AccountID: uuid.NewString(), Domain: "joana.envios.test", Kind: es.DomainOwn, Status: es.DomainPending, ConnectMethod: es.ConnectManual, MailFromDomain: "x", ReplyDomain: "y"}); !errors.Is(err, es.ErrDomainTaken) {
		t.Fatalf("taken = %v", err)
	}
	pending, err := domains.ListPending(ctx, 10)
	if err != nil || len(pending) != 1 || len(pending[0].Records) != 1 || pending[0].Records[0].Status != es.RecordVerified {
		t.Fatalf("pending = %+v err = %v", pending, err)
	}
	d.Status = es.DomainVerified
	d.Retired = true
	if err := domains.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := domains.GetByReplyDomain(ctx, "joana.envios.test")
	if err != nil || !got.Retired || got.Status != es.DomainVerified {
		t.Fatalf("domain = %+v err = %v", got, err)
	}
	if _, err := domains.Get(ctx, uuid.NewString(), d.ID); !errors.Is(err, es.ErrDomainNotFound) {
		t.Fatalf("other workspace must not see the domain: %v", err)
	}

	first := &es.Sender{WorkspaceID: ws, DomainID: d.ID, LocalPart: "contato", DisplayName: "Joana", IsDefault: true}
	second := &es.Sender{WorkspaceID: ws, DomainID: d.ID, LocalPart: "vendas", DisplayName: "Joana", IsDefault: true}
	for _, s := range []*es.Sender{first, second} {
		if err := senders.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	def, err := senders.GetDefault(ctx, ws)
	if err != nil || def.ID != second.ID {
		t.Fatalf("default = %+v err = %v", def, err)
	}
	if err := senders.Create(ctx, &es.Sender{WorkspaceID: ws, DomainID: d.ID, LocalPart: "contato", DisplayName: "x"}); !errors.Is(err, es.ErrSenderExists) {
		t.Fatalf("duplicate local part = %v", err)
	}
	target := uuid.NewString()
	if err := senders.MoveDomain(ctx, ws, d.ID, target); err != nil {
		t.Fatal(err)
	}
	moved, _ := senders.Get(ctx, ws, first.ID)
	if moved.DomainID != target {
		t.Fatalf("moved = %+v", moved)
	}
	if err := senders.Delete(ctx, ws, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := senders.Delete(ctx, ws, first.ID); !errors.Is(err, es.ErrSenderNotFound) {
		t.Fatalf("second delete = %v", err)
	}

	addr := emailaddress.Address("cliente@example.com")
	for _, reason := range []es.SuppressReason{es.SuppressHardBounce, es.SuppressComplaint} {
		if err := suppressions.Suppress(ctx, es.Suppression{WorkspaceID: ws, Address: addr, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	found, err := suppressions.Suppressed(ctx, ws, []emailaddress.Address{addr, "other@example.com"})
	if err != nil || len(found) != 1 || found[addr] != es.SuppressHardBounce {
		t.Fatalf("suppressed = %v err = %v", found, err)
	}
	if list, total, err := suppressions.List(ctx, ws, es.SuppressionQuery{Limit: 10}); err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list = %v %d %v", list, total, err)
	}
	if err := suppressions.Remove(ctx, ws, addr); err != nil {
		t.Fatal(err)
	}
	if found, _ := suppressions.Suppressed(ctx, ws, []emailaddress.Address{addr}); len(found) != 0 {
		t.Fatalf("after remove = %v", found)
	}
}

func TestIntegrationSuppressionSearchByAddressPrefix(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	suppressions := NewSuppressionRepository(db)
	ws, other := uuid.NewString(), uuid.NewString()
	for _, s := range []es.Suppression{
		{WorkspaceID: ws, Address: "ana.souza@example.com", Reason: es.SuppressManual},
		{WorkspaceID: ws, Address: "ana_b@example.com", Reason: es.SuppressHardBounce},
		{WorkspaceID: ws, Address: "bruno@example.com", Reason: es.SuppressManual},
		{WorkspaceID: other, Address: "ana.souza@example.com", Reason: es.SuppressManual},
	} {
		if err := suppressions.Suppress(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		prefix string
		want   int64
	}{
		{"", 3},
		{"ana", 2},
		{"ana.", 1},
		{"ana_", 1},
		{"%", 0},
		{"zz", 0},
	}
	for _, tc := range cases {
		list, total, err := suppressions.List(ctx, ws, es.SuppressionQuery{Prefix: tc.prefix, Limit: 10})
		if err != nil || total != tc.want || int64(len(list)) != tc.want {
			t.Errorf("prefix %q: list = %v total = %d err = %v, want %d", tc.prefix, list, total, err, tc.want)
		}
	}
}

func TestIntegrationWorkspaceNamesByID(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	if err := db.Exec("CREATE TABLE workspaces (id uuid PRIMARY KEY, name varchar(255) NOT NULL, deleted_at timestamptz)").Error; err != nil {
		t.Fatal(err)
	}
	live, gone, missing := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := db.Exec("INSERT INTO workspaces (id, name, deleted_at) VALUES (?, 'Loja da Joana', NULL), (?, 'Antiga', now())", live, gone).Error; err != nil {
		t.Fatal(err)
	}
	names := NewWorkspaceNames(db)
	got, err := names.WorkspaceNamesByID(ctx, []string{live, gone, missing, "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[live] != "Loja da Joana" {
		t.Fatalf("names = %v", got)
	}
	if name, err := names.WorkspaceName(ctx, live); err != nil || name != "Loja da Joana" {
		t.Fatalf("name = %q err = %v", name, err)
	}
	if _, err := names.WorkspaceName(ctx, gone); err == nil {
		t.Fatal("a deleted workspace has no name")
	}
	if empty, err := names.WorkspaceNamesByID(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids = %v %v", empty, err)
	}
}

func TestIntegrationSuppressedAnswersEveryRefusalFromTheSharedRead(t *testing.T) {
	db := integrationDB(t)
	suppressions := NewSuppressionRepository(db)
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	local := emailaddress.Address("ana@loja.com.br")
	bounced := emailaddress.Address("gone@loja.com.br")
	deadDomain := emailaddress.Address("bia@nomail.com.br")
	clean := emailaddress.Address("caio@loja.com.br")
	if err := suppressions.Suppress(ctx, es.Suppression{WorkspaceID: ws, Address: local, Reason: es.SuppressSoftBounce}); err != nil {
		t.Fatal(err)
	}
	if err := suppressions.Suppress(ctx, es.Suppression{WorkspaceID: other, Address: clean, Reason: es.SuppressComplaint}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Exec("INSERT INTO email_bad_addresses (address_hash, reason, first_seen_at, last_seen_at) VALUES (sha256(convert_to(?, 'UTF8')), 'hard_bounce', ?, ?)", string(bounced), now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO email_domain_checks (domain, state, detail, expires_at, attempts, created_at, updated_at) VALUES ('nomail.com.br', 'no_mail', 'null_mx', ?, 0, ?, ?)", now.Add(time.Hour), now, now).Error; err != nil {
		t.Fatal(err)
	}
	found, err := suppressions.Suppressed(ctx, ws, []emailaddress.Address{local, bounced, deadDomain, clean})
	if err != nil {
		t.Fatal(err)
	}
	want := map[emailaddress.Address]es.SuppressReason{local: es.SuppressSoftBounce, bounced: es.SuppressBadAddress, deadDomain: es.SuppressNoMailDomain}
	if len(found) != len(want) {
		t.Fatalf("found = %v, want %v (another workspace's complaint never refuses here)", found, want)
	}
	for a, r := range want {
		if found[a] != r {
			t.Fatalf("%s = %q, want %q", a, found[a], r)
		}
	}
}

func TestIntegrationRunOutcomesCountEachMessageOnceEvenWhenNotificationsRepeat(t *testing.T) {
	db := integrationDB(t)
	repo := NewMessageRepository(db)
	outcomes := NewRunOutcomes(db)
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	ws, domainID, runID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seed := func(i int) *es.Message {
		m := &es.Message{WorkspaceID: ws, ProviderMessageID: fmt.Sprintf("ses-%s-%d", runID, i), RunKind: es.RunCampaign, RunID: runID,
			DomainID: domainID, Address: emailaddress.Address(fmt.Sprintf("c%d@example.com", i)), Status: es.MessageSent, SentAt: at}
		if err := repo.Record(ctx, m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	delivered, bounced, complained := seed(1), seed(2), seed(3)
	events := []struct {
		m  *es.Message
		ev es.DeliveryEvent
	}{
		{delivered, es.DeliveryEvent{Kind: es.EventDelivery, OccurredAt: at.Add(time.Minute)}},
		{delivered, es.DeliveryEvent{Kind: es.EventDelivery, OccurredAt: at.Add(time.Minute)}},
		{bounced, es.DeliveryEvent{Kind: es.EventBounce, OccurredAt: at.Add(time.Minute), Bounce: &es.Bounce{Kind: es.BounceHard, SubType: "NoEmail"}}},
		{bounced, es.DeliveryEvent{Kind: es.EventBounce, OccurredAt: at.Add(2 * time.Minute), Bounce: &es.Bounce{Kind: es.BounceHard, SubType: "NoEmail"}}},
		{complained, es.DeliveryEvent{Kind: es.EventDelivery, OccurredAt: at.Add(time.Minute)}},
		{complained, es.DeliveryEvent{Kind: es.EventComplaint, OccurredAt: at.Add(time.Hour)}},
		{complained, es.DeliveryEvent{Kind: es.EventComplaint, OccurredAt: at.Add(time.Hour)}},
	}
	for _, e := range events {
		e.ev.ProviderMessageID = e.m.ProviderMessageID
		if _, err := repo.AppendEvent(ctx, e.m.ID, e.ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := outcomes.RunOutcome(ctx, es.RunCampaign, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got != (es.RunOutcome{Delivered: 2, HardBounces: 1, Complaints: 1}) {
		t.Fatalf("outcome = %+v, want 2 delivered, 1 hard bounce, 1 complaint", got)
	}
	if none, err := outcomes.RunOutcome(ctx, es.RunCampaign, uuid.NewString()); err != nil || none != (es.RunOutcome{}) {
		t.Fatalf("a run without events reads zero, got %+v %v", none, err)
	}
}
