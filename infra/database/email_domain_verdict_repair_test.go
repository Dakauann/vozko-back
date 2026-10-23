package database

import (
	"testing"
	"time"

	"vozko/domain/emailhygiene"
	"vozko/infra/database/schema"
)

func TestTheUnconfirmedNoMailRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "em_recheck_unconfirmed_no_mail" {
			return
		}
	}
	t.Fatal("no-mail verdicts stored without a definite answer must be checked again at boot")
}

func TestRecheckUnconfirmedNoMailAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	if err := tx.Exec(`DROP TABLE IF EXISTS email_domain_checks CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&schema.EmailDomainCheck{}); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	rows := []schema.EmailDomainCheck{
		{Domain: "gmail.com", State: string(emailhygiene.DomainNoMail), Detail: string(emailhygiene.DetailNoRecords), ExpiresAt: old.Add(72 * time.Hour)},
		{Domain: "hotmail.com", State: string(emailhygiene.DomainNoMail), Detail: string(emailhygiene.DetailNone), ExpiresAt: old.Add(72 * time.Hour)},
		{Domain: "nullmx.com.br", State: string(emailhygiene.DomainNoMail), Detail: string(emailhygiene.DetailNullMX), ExpiresAt: old.Add(72 * time.Hour)},
		{Domain: "morto.com.br", State: string(emailhygiene.DomainNoMail), Detail: string(emailhygiene.DetailNoRecordsConfirmed), ExpiresAt: old.Add(72 * time.Hour)},
		{Domain: "loja.com.br", State: string(emailhygiene.DomainAccepts), Detail: string(emailhygiene.DetailMX), ExpiresAt: old.Add(7 * 24 * time.Hour)},
	}
	for i := range rows {
		rows[i].CreatedAt, rows[i].UpdatedAt, rows[i].CheckedAt = old, old, &old
		if err := tx.Create(&rows[i]).Error; err != nil {
			t.Fatalf("fixture %s: %v", rows[i].Domain, err)
		}
	}
	if err := recheckUnconfirmedNoMail(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := recheckUnconfirmedNoMail(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}
	var got []schema.EmailDomainCheck
	if err := tx.Order("domain").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	byDomain := map[string]schema.EmailDomainCheck{}
	for _, row := range got {
		byDomain[row.Domain] = row
	}
	for _, domain := range []string{"gmail.com", "hotmail.com"} {
		row := byDomain[domain]
		if row.State != string(emailhygiene.DomainPending) || row.Detail != "" || row.DueAt == nil || row.Attempts != 0 || row.ExpiresAt.After(time.Now()) {
			t.Fatalf("%s = %+v, an unconfirmed refusal goes back to the queue as pending", domain, row)
		}
	}
	for _, domain := range []string{"nullmx.com.br", "morto.com.br"} {
		if row := byDomain[domain]; row.State != string(emailhygiene.DomainNoMail) || row.DueAt != nil {
			t.Fatalf("%s = %+v, a definite refusal is kept", domain, row)
		}
	}
	if row := byDomain["loja.com.br"]; row.State != string(emailhygiene.DomainAccepts) || row.DueAt != nil {
		t.Fatalf("loja.com.br = %+v, other verdicts are untouched", row)
	}
}
