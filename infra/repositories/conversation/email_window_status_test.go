package conversation_repository

import (
	"strings"
	"testing"

	es "vozko/domain/emailsending"
	"vozko/infra/database/sqlexpr"
)

func TestEmailWindowOpensForEverySendableAccountStatus(t *testing.T) {
	for _, status := range es.SendableStatuses() {
		if !strings.Contains(emailWindowOpen, sqlexpr.Literal(string(status))) {
			t.Errorf("an account %s keeps the composer open", status)
		}
	}
	for _, status := range []es.AccountStatus{es.AccountPausedByProvider, es.AccountPausedByPlatform} {
		if strings.Contains(emailWindowOpen, sqlexpr.Literal(string(status))) {
			t.Errorf("an account %s must close the composer", status)
		}
	}
}
