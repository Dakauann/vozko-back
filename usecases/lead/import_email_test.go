package lead_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
)

type fakeImportHygiene struct {
	mu      sync.Mutex
	verdict map[emailaddress.Address]emailhygiene.Assessment
	calls   [][]emailaddress.Address
	err     error
}

func (f *fakeImportHygiene) AssessMany(_ context.Context, _ string, addresses []emailaddress.Address) (map[emailaddress.Address]emailhygiene.Assessment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]emailaddress.Address(nil), addresses...))
	if f.err != nil {
		return nil, f.err
	}
	out := map[emailaddress.Address]emailhygiene.Assessment{}
	for _, a := range addresses {
		v, ok := f.verdict[a]
		if !ok {
			v = emailhygiene.Assessment{Verdict: emailhygiene.VerdictValid}
		}
		out[a] = v
	}
	return out, nil
}

const emailFile = "telefone;nome;email\n" +
	"11911110001;Ana;ana@gmial.com\n" +
	"11911110002;Bia;bia@mailinator.com\n" +
	"11911110003;Caio;caio@loja.com.br\n" +
	"11911110004;Duda;\n"

func emailFixture(t *testing.T) *importFixture {
	t.Helper()
	f := newImportFixture(t, allImportPermissions())
	f.hygiene.verdict = map[emailaddress.Address]emailhygiene.Assessment{
		"ana@gmial.com":      {Verdict: emailhygiene.VerdictTypo, Suggested: "ana@gmail.com"},
		"bia@mailinator.com": {Verdict: emailhygiene.VerdictDisposable},
	}
	return f
}

func TestTheDryRunJudgesEveryEmailOnceAndCountsTheVerdicts(t *testing.T) {
	f := emailFixture(t)
	up := f.upload(t, emailFile)
	job := f.dryRun(t, up, suggested(up))
	got := *job.DryRun
	if got.Emails["valid"] != 1 || got.Emails["typo"] != 1 || got.Emails["disposable"] != 1 || len(got.Emails) != 3 {
		t.Fatalf("emails = %v, a row without an email is not judged", got.Emails)
	}
	if got.Issues[string(lead.ReasonEmailTypo)] != 1 || got.Issues[string(lead.ReasonEmailDisposable)] != 1 {
		t.Fatalf("issues = %v", got.Issues)
	}
	if len(f.hygiene.calls) != 1 || len(f.hygiene.calls[0]) != 3 {
		t.Fatalf("calls = %v, one set-wise call per batch", f.hygiene.calls)
	}
	if f.base.writes != 0 {
		t.Fatal("a dry run fixes nothing")
	}
}

func TestStartAppliesTheChosenEmailFixes(t *testing.T) {
	f := emailFixture(t)
	up := f.upload(t, emailFile)
	job := f.dryRun(t, up, suggested(up))
	if _, err := f.imp.Start(context.Background(), importer(), job.ID, StartChoices{FixEmailTypos: true, DropUndeliverableEmails: true}); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	emails := map[string]string{}
	for _, l := range f.base.leads {
		emails[l.Name] = l.Email
	}
	if emails["Ana"] != "ana@gmail.com" || emails["Bia"] != "" || emails["Caio"] != "caio@loja.com.br" {
		t.Fatalf("emails = %v", emails)
	}
	if done := f.store.job(job.ID); !done.Grants.FixEmailTypos || !done.Grants.DropUndeliverableEmails {
		t.Fatalf("grants = %+v, the choices stay with the run so a resumed worker applies them too", done.Grants)
	}
}

func TestStartWithoutChoicesKeepsTheEmailsAsTheyCame(t *testing.T) {
	f := emailFixture(t)
	up := f.upload(t, emailFile)
	f.start(t, f.dryRun(t, up, suggested(up)))
	emails := map[string]string{}
	for _, l := range f.base.leads {
		emails[l.Name] = l.Email
	}
	if emails["Ana"] != "ana@gmial.com" || emails["Bia"] != "bia@mailinator.com" {
		t.Fatalf("emails = %v", emails)
	}
}

func TestAnUnreadableEmailCheckHandsTheBatchBack(t *testing.T) {
	f := emailFixture(t)
	f.hygiene.err = errors.New("db down")
	up := f.upload(t, emailFile)
	job := f.dryRun(t, up, suggested(up))
	if job.Status == leadimport.StatusAnalyzed {
		t.Fatal("an import never counts emails it could not check")
	}
}
