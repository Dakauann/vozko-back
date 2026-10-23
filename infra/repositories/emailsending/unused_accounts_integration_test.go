package emailsending_repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailcampaign"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
)

func unusedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := integrationDB(t)
	models := append(append([]any{}, schema.EmailCampaignModels()...), schema.EmailInboxModels()...)
	models = append(models, &schema.EmailAPISend{})
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	return db
}

type unusedSeed struct {
	ws, account, domain, sender string
}

func seedUnusedAccount(t *testing.T, db *gorm.DB, slug string, activated time.Time) unusedSeed {
	t.Helper()
	ctx := context.Background()
	ws := uuid.NewString()
	acct := &es.SenderAccount{WorkspaceID: ws, Provider: "ses", TenantName: "ws-" + ws, Mode: es.ModeProbation, Status: es.AccountActive, Slug: slug, ProbationStartedAt: activated}
	if err := NewAccountRepository(db).Create(ctx, acct); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE email_sender_accounts SET created_at = ? WHERE id = ?", activated, acct.ID).Error; err != nil {
		t.Fatal(err)
	}
	fqdn := slug + ".envios.test"
	d := &es.SendingDomain{WorkspaceID: ws, AccountID: acct.ID, Domain: fqdn, Kind: es.DomainSharedProbation, Status: es.DomainVerified,
		ConnectMethod: es.ConnectAutomatic, MailFromDomain: "bounce." + fqdn, ReplyDomain: fqdn}
	if err := NewDomainRepository(db).Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	sender := &es.Sender{WorkspaceID: ws, DomainID: d.ID, LocalPart: "contato", DisplayName: "Loja", IsDefault: true}
	if err := NewSenderRepository(db).Create(ctx, sender); err != nil {
		t.Fatal(err)
	}
	return unusedSeed{ws: ws, account: acct.ID, domain: d.ID, sender: sender.ID}
}

func campaignIn(db *gorm.DB, ws string, status emailcampaign.Status) error {
	return db.Create(&schema.EmailCampaign{ID: uuid.NewString(), WorkspaceID: ws, Name: string(status), Status: string(status)}).Error
}

func TestIntegrationUnusedCandidatesAreOldAccountsThatNeverSent(t *testing.T) {
	db := unusedDB(t)
	ctx := context.Background()
	repo := NewUnusedAccounts(db)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-23 * 24 * time.Hour)
	old := seedUnusedAccount(t, db, "antiga", now.Add(-40*24*time.Hour))
	fresh := seedUnusedAccount(t, db, "nova", now.Add(-2*24*time.Hour))
	sent := seedUnusedAccount(t, db, "enviou", now.Add(-40*24*time.Hour))
	seedMessage(t, NewMessageRepository(db), sent.ws, sent.domain, "ses-"+uuid.NewString(), now.Add(-30*24*time.Hour))
	paused := seedUnusedAccount(t, db, "pausada", now.Add(-40*24*time.Hour))
	if err := db.Exec("UPDATE email_sender_accounts SET status = ? WHERE id = ?", es.AccountPausedByPlatform, paused.account).Error; err != nil {
		t.Fatal(err)
	}
	warned := seedUnusedAccount(t, db, "avisada", now.Add(-2*24*time.Hour))
	if err := db.Exec("UPDATE email_sender_accounts SET unused_warned_at = ? WHERE id = ?", now, warned.account).Error; err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	after := ""
	for {
		page, err := repo.ListUnusedCandidates(ctx, cutoff, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 1 {
			t.Fatalf("page of %d, want at most the limit", len(page))
		}
		got[page[0].WorkspaceID] = true
		after = page[0].ID
	}
	if len(got) != 2 || !got[old.ws] || !got[warned.ws] || got[fresh.ws] || got[sent.ws] || got[paused.ws] {
		t.Fatalf("candidates = %v, want the old unused account and the warned one", got)
	}
}

func TestIntegrationInUseCountsSendsPendingCampaignsAndHistory(t *testing.T) {
	db := unusedDB(t)
	ctx := context.Background()
	repo := NewUnusedAccounts(db)
	activated := time.Now().UTC().Add(-40 * 24 * time.Hour)
	cases := []struct {
		name string
		mark func(s unusedSeed) error
		want bool
	}{
		{"nothing", func(unusedSeed) error { return nil }, false},
		{"a draft campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusDraft) }, false},
		{"a stopped campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusStopped) }, false},
		{"a ready campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusReady) }, true},
		{"a scheduled campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusScheduled) }, true},
		{"a running campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusRunning) }, true},
		{"a paused campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusPaused) }, true},
		{"a held campaign", func(s unusedSeed) error { return campaignIn(db, s.ws, emailcampaign.StatusHeld) }, true},
		{"a sent message", func(s unusedSeed) error {
			seedMessage(t, NewMessageRepository(db), s.ws, s.domain, "ses-"+uuid.NewString(), time.Now().UTC())
			return nil
		}, true},
		{"an inbound conversation", func(s unusedSeed) error {
			return db.Create(&schema.EmailConversation{ID: uuid.NewString(), WorkspaceID: s.ws, ContactID: uuid.NewString(), SenderID: s.sender}).Error
		}, true},
		{"an api send", func(s unusedSeed) error {
			return db.Exec("INSERT INTO email_api_sends (id, workspace_id, idempotency_key, fingerprint, origin, sender_id, from_address, subject, tags, recipients, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, now(), now())",
				uuid.NewString(), s.ws, uuid.NewString(), strings.Repeat("0", 64), "api", s.sender, "contato@x.test", "Oi", "[]", 1).Error
		}, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := seedUnusedAccount(t, db, "uso"+string(rune('a'+i)), activated)
			if err := tc.mark(s); err != nil {
				t.Fatal(err)
			}
			got, err := repo.InUse(ctx, s.ws)
			if err != nil || got != tc.want {
				t.Fatalf("InUse = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestIntegrationForgetRemovesTheAccountAndFreesItsName(t *testing.T) {
	db := unusedDB(t)
	ctx := context.Background()
	repo := NewUnusedAccounts(db)
	s := seedUnusedAccount(t, db, "liberada", time.Now().UTC().Add(-40*24*time.Hour))
	other := seedUnusedAccount(t, db, "vizinha", time.Now().UTC().Add(-40*24*time.Hour))
	draft := uuid.NewString()
	if err := db.Create(&schema.EmailCampaign{ID: draft, WorkspaceID: s.ws, Name: "Rascunho", Status: string(emailcampaign.StatusDraft), SenderID: schema.OptionalText(s.sender)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schema.EmailInbox{ID: s.sender, WorkspaceID: s.ws, Name: "Contato", Address: "contato@liberada.envios.test"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schema.EmailDailyUsage{DomainID: s.domain, Day: "2026-10-01", UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.Forget(ctx, s.ws, s.account); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAccountRepository(db).GetByWorkspace(ctx, s.ws); !errors.Is(err, es.ErrAccountNotFound) {
		t.Fatalf("account after forget: %v", err)
	}
	if taken, err := NewAccountRepository(db).SlugTaken(ctx, "liberada"); err != nil || taken {
		t.Fatalf("slug taken = %v %v", taken, err)
	}
	if _, err := NewDomainRepository(db).GetByDomain(ctx, "liberada.envios.test"); !errors.Is(err, es.ErrDomainNotFound) {
		t.Fatalf("domain after forget: %v", err)
	}
	if senders, err := NewSenderRepository(db).ListByWorkspace(ctx, s.ws); err != nil || len(senders) != 0 {
		t.Fatalf("senders after forget = %v %v", senders, err)
	}
	var inboxes, usage int64
	db.Model(&schema.EmailInbox{}).Where("id = ?", s.sender).Count(&inboxes)
	db.Model(&schema.EmailDailyUsage{}).Where("domain_id = ?", s.domain).Count(&usage)
	if inboxes != 0 || usage != 0 {
		t.Fatalf("inboxes %d usage %d left pointing at the removed sender and domain", inboxes, usage)
	}
	var campaign schema.EmailCampaign
	if err := db.Take(&campaign, "id = ?", draft).Error; err != nil || campaign.SenderID != "" {
		t.Fatalf("the draft keeps its row and falls back to the default sender: %+v %v", campaign, err)
	}
	if _, err := NewAccountRepository(db).GetByWorkspace(ctx, other.ws); err != nil {
		t.Fatalf("another workspace was touched: %v", err)
	}
	if err := repo.Forget(ctx, s.ws, s.account); err != nil {
		t.Fatalf("forgetting twice is done: %v", err)
	}
}

func TestIntegrationForgetRefusesAnAccountThatWasUsedMeanwhile(t *testing.T) {
	db := unusedDB(t)
	ctx := context.Background()
	repo := NewUnusedAccounts(db)
	s := seedUnusedAccount(t, db, "usada", time.Now().UTC().Add(-40*24*time.Hour))
	seedMessage(t, NewMessageRepository(db), s.ws, s.domain, "ses-"+uuid.NewString(), time.Now().UTC())
	if err := repo.Forget(ctx, s.ws, s.account); !errors.Is(err, es.ErrAccountInUse) {
		t.Fatalf("err = %v, want the account kept", err)
	}
	if _, err := NewAccountRepository(db).GetByWorkspace(ctx, s.ws); err != nil {
		t.Fatalf("a used account was removed: %v", err)
	}
	if senders, _ := NewSenderRepository(db).ListByWorkspace(ctx, s.ws); len(senders) != 1 {
		t.Fatal("a refused forget removed senders")
	}
}

func TestIntegrationTheUnusedWarningIsStoredAndCleared(t *testing.T) {
	db := unusedDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	s := seedUnusedAccount(t, db, "carimbo", time.Now().UTC().Add(-25*24*time.Hour))
	acct, err := accounts.GetByWorkspace(ctx, s.ws)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	acct.UnusedWarnedAt = &stamp
	if err := accounts.Update(ctx, acct); err != nil {
		t.Fatal(err)
	}
	stored, _ := accounts.GetByWorkspace(ctx, s.ws)
	if stored.UnusedWarnedAt == nil || !stored.UnusedWarnedAt.Equal(stamp) {
		t.Fatalf("stamp = %v", stored.UnusedWarnedAt)
	}
	stored.UnusedWarnedAt = nil
	if err := accounts.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := accounts.GetByWorkspace(ctx, s.ws); cleared.UnusedWarnedAt != nil {
		t.Fatalf("stamp after clearing = %v", cleared.UnusedWarnedAt)
	}
}
