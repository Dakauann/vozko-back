package database

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/emailhygiene"
)

const emailDomainChecksTable = "email_domain_checks"

func recheckUnconfirmedNoMail(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(emailDomainChecksTable) {
		return nil
	}
	definite := Texts{}
	for _, detail := range emailhygiene.DefiniteNoMailDetails() {
		definite = append(definite, string(detail))
	}
	now := time.Now().UTC()
	return tx.Exec(`UPDATE `+emailDomainChecksTable+` SET state = ?, detail = '', attempts = 0, expires_at = ?, due_at = ?, updated_at = ?
WHERE state = ? AND detail <> ALL(?::text[])`,
		string(emailhygiene.DomainPending), now, now, now, string(emailhygiene.DomainNoMail), definite).Error
}
