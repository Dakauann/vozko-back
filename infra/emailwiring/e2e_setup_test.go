package emailwiring_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	es "vozko/domain/emailsending"
)

func TestEmailE2E_SetupProvisionsASharedSubdomainAndTheSweepVerifiesIt(t *testing.T) {
	w := newWorld(t)
	ws := w.newTenant("Loja da Ana", 0)

	answered, err := w.module.Setup.Provision(w.ctx, ws.ID, "")
	w.must(err)
	if !answered.Provisioned || answered.Account == nil || answered.Account.Mode != es.ModeProbation || answered.Account.Status != es.AccountActive {
		t.Fatalf("account = %+v, want an active probation account", answered.Account)
	}
	if answered.Setup != (es.SetupProgress{State: es.SetupInProgress}) || len(answered.Domains) != 1 || answered.Domains[0].Domain.Status != es.DomainPending {
		t.Fatalf("the request answers with the setup in progress: setup %+v domains %+v", answered.Setup, answered.Domains)
	}
	if w.ses.HasTenant(answered.Account.TenantName) || len(w.zones.published(probationDomain)) != 0 {
		t.Fatal("the request did provider work instead of leaving it to the setup run")
	}
	if kicks := w.queue.readyCount(es.SetupFireTopic); kicks != 1 {
		t.Fatalf("setup runs queued = %d, want one started right away", kicks)
	}

	if ran := w.runSetup(); ran != 1 {
		t.Fatalf("setup runs = %d", ran)
	}
	status, err := w.module.Setup.Status(w.ctx, ws.ID)
	w.must(err)
	if status.Setup.State != es.SetupDone {
		t.Fatalf("setup after the run = %+v", status.Setup)
	}
	if !w.ses.HasTenant(status.Account.TenantName) || status.Account.TenantRef == "" {
		t.Fatalf("tenant %s was not created at the provider: %+v", status.Account.TenantName, status.Account)
	}
	if len(status.Domains) != 1 {
		t.Fatalf("domains = %d, want the one shared subdomain", len(status.Domains))
	}
	domain := status.Domains[0].Domain
	wantFQDN := status.Account.Slug + "." + probationDomain
	if domain.Kind != es.DomainSharedProbation || domain.Domain != wantFQDN || domain.ReplyDomain != wantFQDN || domain.MailFromDomain != "bounce."+wantFQDN {
		t.Fatalf("domain = %+v, want the probation subdomain %s", domain, wantFQDN)
	}
	if domain.Status != es.DomainVerifying || status.Domains[0].CapToday != es.ProbationDailyCap {
		t.Fatalf("status %s cap %d, want verifying with the probation cap", domain.Status, status.Domains[0].CapToday)
	}
	published := w.zones.published(probationDomain)
	if len(published) == 0 || len(published) != len(domain.Records) {
		t.Fatalf("published %d records on the shared zone, the domain lists %d", len(published), len(domain.Records))
	}
	if got := w.ses.Receiving(domain.ReplyDomain); !slices.Equal(got, es.InboundRecipients(domain.ReplyDomain)) {
		t.Fatalf("receipt conditions = %v, want reply@ and unsubscribe@", got)
	}
	if len(status.Senders) != 0 {
		t.Fatalf("senders before verification = %d", len(status.Senders))
	}

	w.must(w.module.Verification.Sweep(w.ctx))
	still, err := w.domains.Get(w.ctx, ws.ID, domain.ID)
	w.must(err)
	if still.Status != es.DomainVerifying {
		t.Fatalf("before the DNS propagates the sweep left %s", still.Status)
	}

	w.verifyDomains(ws)
	if ws.Domain.Status != es.DomainVerified || ws.Domain.VerifiedAt == nil || !ws.Domain.Sendable() {
		t.Fatalf("after propagation the domain is %+v", ws.Domain)
	}
	if ws.Sender.DomainID != domain.ID || !ws.Sender.IsDefault {
		t.Fatalf("default sender = %+v", ws.Sender)
	}
	if string(ws.Sender.FromAddress(ws.Domain)) != "contato@"+wantFQDN {
		t.Fatalf("default sender address = %s", ws.Sender.FromAddress(ws.Domain))
	}

	again, err := w.module.Setup.Provision(w.ctx, ws.ID, "")
	w.must(err)
	if len(again.Domains) != 1 || again.Domains[0].Domain.Status != es.DomainVerified || len(again.Senders) != 1 || again.Setup.State != es.SetupDone {
		t.Fatalf("provisioning twice = %+v, want the same verified domain and sender", again)
	}
	if kicks := w.queue.readyCount(es.SetupFireTopic); kicks != 0 {
		t.Fatalf("a finished setup queued %d runs", kicks)
	}
}

func TestEmailE2E_AProviderFailureInTheSetupRunIsRecordedAndARetryFinishesIt(t *testing.T) {
	w := newWorld(t)
	ws := w.newTenant("Loja do Bruno", 0)

	_, err := w.module.Setup.Provision(w.ctx, ws.ID, "")
	w.must(err)
	w.zones.refuseNextChange("AccessDenied", "not authorized to perform route53:ChangeResourceRecordSets")
	w.runSetup()

	failed, err := w.module.Setup.Status(w.ctx, ws.ID)
	w.must(err)
	if failed.Setup != (es.SetupProgress{State: es.SetupFailed, Failure: es.SetupFailureProvider}) {
		t.Fatalf("setup = %+v, want the provider failure", failed.Setup)
	}
	stored, err := w.domains.Get(w.ctx, ws.ID, failed.Domains[0].Domain.ID)
	w.must(err)
	if stored.Status != es.DomainPending || stored.SetupFailure != es.SetupFailureProvider || !strings.HasPrefix(stored.FailureReason, "dns: ") || !strings.Contains(stored.FailureReason, "AccessDenied") {
		t.Fatalf("stored domain = %+v, want the failure and its reason saved", stored)
	}

	retry, err := w.module.Setup.Provision(w.ctx, ws.ID, "")
	w.must(err)
	if retry.Setup.State != es.SetupInProgress || retry.Domains[0].Domain.FailureReason != "" {
		t.Fatalf("a retry answers with the setup in progress again: %+v", retry.Setup)
	}
	w.runSetup()
	done, err := w.module.Setup.Status(w.ctx, ws.ID)
	w.must(err)
	if done.Setup.State != es.SetupDone || done.Domains[0].Domain.Status != es.DomainVerifying || done.Domains[0].Domain.FailureReason != "" {
		t.Fatalf("after the retry = %+v %+v", done.Setup, done.Domains[0].Domain)
	}
}

func TestEmailE2E_TheCustomerChoosesTheSubdomainAndCanChangeItUntilTheFirstSend(t *testing.T) {
	w := newWorld(t)
	ws := w.newTenant("Cartão de Todos Osasco", 0)
	rival := w.newTenant("Outra Loja", 0)

	check, err := w.module.Setup.CheckSubdomain(w.ctx, ws.ID, "cartaodetodososasco")
	w.must(err)
	if check.Refusal != nil || check.Address != "contato@cartaodetodososasco."+probationDomain {
		t.Fatalf("check = %+v", check)
	}
	_, err = w.module.Setup.Provision(w.ctx, ws.ID, "cartaodetodososasco")
	w.must(err)
	if _, err := w.module.Setup.Provision(w.ctx, rival.ID, "cartaodetodososasco"); !errors.Is(err, es.ErrSlugTaken) {
		t.Fatalf("a second workspace on the same name = %v", err)
	}
	w.runSetup()
	first := "cartaodetodososasco." + probationDomain
	if !w.ses.HasIdentity(first) || len(w.zones.published(probationDomain)) == 0 {
		t.Fatal("the chosen subdomain was not set up at the provider")
	}

	changed, err := w.module.Setup.ChangeSubdomain(w.ctx, ws.ID, "cartaodetodos")
	w.must(err)
	if changed.Account.Slug != "cartaodetodos" || changed.Setup.State != es.SetupInProgress {
		t.Fatalf("after the change = %+v %+v", changed.Account, changed.Setup)
	}
	w.runSetup()
	second := "cartaodetodos." + probationDomain
	if w.ses.HasIdentity(first) || len(w.ses.Receiving(first)) != 0 || !w.ses.HasIdentity(second) {
		t.Fatalf("old identity %v old receiving %v new identity %v", w.ses.HasIdentity(first), w.ses.Receiving(first), w.ses.HasIdentity(second))
	}
	for _, r := range w.zones.published(probationDomain) {
		if strings.HasSuffix(r.Name, first) {
			t.Fatalf("an old record stayed published: %+v", r)
		}
	}
	old, err := w.domains.GetByDomain(w.ctx, first)
	w.must(err)
	if old.ReleaseState != es.ReleaseDone || !old.Retired {
		t.Fatalf("old domain = %+v", old)
	}
	if again, err := w.module.Setup.CheckSubdomain(w.ctx, rival.ID, "cartaodetodososasco"); err != nil || !errors.Is(again.Refusal, es.ErrSlugTaken) {
		t.Fatalf("a released name stays with its workspace: %+v %v", again, err)
	}

	w.verifyDomains(ws)
	if ws.Domain.Domain != second || !ws.Domain.Sendable() {
		t.Fatalf("sending domain = %+v", ws.Domain)
	}
	w.must(w.messages.Record(w.ctx, &es.Message{WorkspaceID: ws.ID, DomainID: ws.Domain.ID, RunKind: es.RunTest, RunID: uuid.NewString(), Address: "cliente@example.com", Status: es.MessageSent, SentAt: time.Now().UTC()}))
	if _, err := w.module.Setup.ChangeSubdomain(w.ctx, ws.ID, "outro-nome"); !errors.Is(err, es.ErrSubdomainLocked) {
		t.Fatalf("a change after the first send = %v", err)
	}
	status, err := w.module.Setup.Status(w.ctx, ws.ID)
	w.must(err)
	if !status.Subdomain.Locked || status.Subdomain.Address != "contato@"+second {
		t.Fatalf("subdomain = %+v", status.Subdomain)
	}
}
